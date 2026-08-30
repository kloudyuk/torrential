package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	controllerAddress       = ":8080"
	defaultDockerSocket     = "/var/run/docker.sock"
	dockerRestartTimeout    = 30
	gluetunReadinessTimeout = 3 * time.Minute
)

var dashboardServiceNames = []string{"seerr", "sonarr", "radarr", "prowlarr", "transmission", "plex"}

var fullRestartGroups = [][]string{
	{"gluetun"},
	{"transmission", "prowlarr", "byparr"},
	{"sonarr", "radarr", "seerr", "plex"},
}

type serviceRuntimeStatus struct {
	Service   string `json:"service"`
	State     string `json:"state"`
	Health    string `json:"health"`
	StartedAt string `json:"startedAt,omitempty"`
}

type operationStatus struct {
	Running    bool   `json:"running"`
	Action     string `json:"action,omitempty"`
	Service    string `json:"service,omitempty"`
	Message    string `json:"message,omitempty"`
	Error      string `json:"error,omitempty"`
	StartedAt  string `json:"startedAt,omitempty"`
	FinishedAt string `json:"finishedAt,omitempty"`
}

type controllerResponse struct {
	Services  []serviceRuntimeStatus `json:"services"`
	Operation operationStatus        `json:"operation"`
}

type containerManager interface {
	statuses(context.Context, []string) ([]serviceRuntimeStatus, error)
	restart(context.Context, string) error
	waitHealthy(context.Context, string, time.Duration) error
}

type controlServer struct {
	manager   containerManager
	operation operationStatus
	mutex     sync.RWMutex
}

func newControlServer(manager containerManager) *controlServer {
	return &controlServer{manager: manager}
}

func (server *controlServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/status", server.handleStatus)
	mux.HandleFunc("POST /api/restart-all", server.handleRestartAll)
	mux.HandleFunc("POST /api/restart/{service}", server.handleRestartService)
	return mux
}

func (server *controlServer) handleStatus(writer http.ResponseWriter, request *http.Request) {
	statuses, err := server.manager.statuses(request.Context(), dashboardServiceNames)
	if err != nil {
		writeControllerError(writer, http.StatusBadGateway, err)
		return
	}
	server.mutex.RLock()
	operation := server.operation
	server.mutex.RUnlock()
	writeControllerJSON(writer, http.StatusOK, controllerResponse{Services: statuses, Operation: operation})
}

func (server *controlServer) handleRestartService(writer http.ResponseWriter, request *http.Request) {
	if err := validateRestartRequest(request); err != nil {
		writeControllerError(writer, http.StatusForbidden, err)
		return
	}
	service := request.PathValue("service")
	if !containsString(dashboardServiceNames, service) {
		writeControllerError(writer, http.StatusNotFound, fmt.Errorf("service is not restartable"))
		return
	}
	if !server.startOperation("restart", service, "Restarting "+service) {
		writeControllerError(writer, http.StatusConflict, fmt.Errorf("another restart is already running"))
		return
	}
	go server.runOperation(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		return server.manager.restart(ctx, service)
	})
	writeControllerJSON(writer, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (server *controlServer) handleRestartAll(writer http.ResponseWriter, request *http.Request) {
	if err := validateRestartRequest(request); err != nil {
		writeControllerError(writer, http.StatusForbidden, err)
		return
	}
	if !server.startOperation("restart-all", "", "Restarting stack services") {
		writeControllerError(writer, http.StatusConflict, fmt.Errorf("another restart is already running"))
		return
	}
	go server.runOperation(server.restartAll)
	writeControllerJSON(writer, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (server *controlServer) restartAll() error {
	for groupIndex, group := range fullRestartGroups {
		for _, service := range group {
			server.setOperationStep(service, "Restarting "+service)
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			err := server.manager.restart(ctx, service)
			cancel()
			if err != nil {
				return err
			}
		}
		if groupIndex == 0 {
			server.setOperationStep("gluetun", "Waiting for VPN health")
			ctx, cancel := context.WithTimeout(context.Background(), gluetunReadinessTimeout)
			err := server.manager.waitHealthy(ctx, "gluetun", gluetunReadinessTimeout)
			cancel()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (server *controlServer) startOperation(action, service, message string) bool {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	if server.operation.Running {
		return false
	}
	server.operation = operationStatus{
		Running:   true,
		Action:    action,
		Service:   service,
		Message:   message,
		StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	logController(message + " requested")
	return true
}

func (server *controlServer) setOperationStep(service, message string) {
	server.mutex.Lock()
	server.operation.Service = service
	server.operation.Message = message
	server.mutex.Unlock()
}

func (server *controlServer) runOperation(operation func() error) {
	err := operation()
	server.mutex.Lock()
	server.operation.Running = false
	server.operation.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err != nil {
		server.operation.Error = err.Error()
		server.operation.Message = "Restart failed"
		logController("restart failed: " + err.Error())
	} else {
		server.operation.Message = "Restart completed"
		logController("restart completed")
	}
	server.mutex.Unlock()
}

func validateRestartRequest(request *http.Request) error {
	if request.Header.Get("X-Torrential-Action") != "restart" {
		return fmt.Errorf("missing restart confirmation header")
	}
	if !strings.HasPrefix(strings.ToLower(request.Header.Get("Content-Type")), "application/json") {
		return fmt.Errorf("restart requests must use application/json")
	}
	origin := request.Header.Get("Origin")
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || !strings.EqualFold(parsed.Host, request.Host) {
		return fmt.Errorf("restart request origin does not match dashboard")
	}
	return nil
}

func containsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func writeControllerJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeControllerError(writer http.ResponseWriter, status int, err error) {
	writeControllerJSON(writer, status, map[string]string{"error": err.Error()})
}

type dockerContainerManager struct {
	client  *http.Client
	project string
}

type dockerContainerSummary struct {
	ID     string            `json:"Id"`
	Labels map[string]string `json:"Labels"`
}

type dockerContainerInspect struct {
	State struct {
		Status    string `json:"Status"`
		Running   bool   `json:"Running"`
		StartedAt string `json:"StartedAt"`
		Health    *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
}

func newDockerContainerManager(socketPath, project string) *dockerContainerManager {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &dockerContainerManager{
		client:  &http.Client{Transport: transport, Timeout: 45 * time.Second},
		project: project,
	}
}

func (manager *dockerContainerManager) statuses(ctx context.Context, services []string) ([]serviceRuntimeStatus, error) {
	containers, err := manager.containers(ctx)
	if err != nil {
		return nil, err
	}
	statuses := make([]serviceRuntimeStatus, 0, len(services))
	for _, service := range services {
		containerID := containers[service]
		if containerID == "" {
			statuses = append(statuses, serviceRuntimeStatus{Service: service, State: "missing", Health: "unavailable"})
			continue
		}
		inspection, err := manager.inspect(ctx, containerID)
		if err != nil {
			return nil, err
		}
		status := serviceRuntimeStatus{Service: service, State: inspection.State.Status, Health: "unavailable"}
		if inspection.State.Health != nil {
			status.Health = inspection.State.Health.Status
		} else if inspection.State.Running {
			status.Health = "running"
		}
		if inspection.State.Running {
			status.StartedAt = inspection.State.StartedAt
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func (manager *dockerContainerManager) restart(ctx context.Context, service string) error {
	containers, err := manager.containers(ctx)
	if err != nil {
		return err
	}
	containerID := containers[service]
	if containerID == "" {
		return fmt.Errorf("Torrential service %s was not found", service)
	}
	endpoint := fmt.Sprintf("http://docker/containers/%s/restart?t=%d", url.PathEscape(containerID), dockerRestartTimeout)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	response, err := manager.client.Do(request)
	if err != nil {
		return fmt.Errorf("restart %s: %w", service, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("restart %s returned HTTP %d", service, response.StatusCode)
	}
	return nil
}

func (manager *dockerContainerManager) waitHealthy(ctx context.Context, service string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		statuses, err := manager.statuses(ctx, []string{service})
		if err == nil && len(statuses) == 1 && statuses[0].Health == "healthy" {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for %s health: %w", service, ctx.Err())
		case <-deadline.C:
			return fmt.Errorf("wait for %s health timed out", service)
		case <-ticker.C:
		}
	}
}

func (manager *dockerContainerManager) containers(ctx context.Context) (map[string]string, error) {
	filters, _ := json.Marshal(map[string][]string{
		"label": {"com.docker.compose.project=" + manager.project},
	})
	endpoint := "http://docker/containers/json?all=true&filters=" + url.QueryEscape(string(filters))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	response, err := manager.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("list Docker containers: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list Docker containers returned HTTP %d", response.StatusCode)
	}
	var summaries []dockerContainerSummary
	if err := json.NewDecoder(response.Body).Decode(&summaries); err != nil {
		return nil, fmt.Errorf("decode Docker containers: %w", err)
	}
	containers := make(map[string]string)
	for _, summary := range summaries {
		service := summary.Labels["com.docker.compose.service"]
		if service != "" {
			containers[service] = summary.ID
		}
	}
	return containers, nil
}

func (manager *dockerContainerManager) inspect(ctx context.Context, containerID string) (dockerContainerInspect, error) {
	endpoint := "http://docker/containers/" + url.PathEscape(containerID) + "/json"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return dockerContainerInspect{}, err
	}
	response, err := manager.client.Do(request)
	if err != nil {
		return dockerContainerInspect{}, fmt.Errorf("inspect Docker container: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return dockerContainerInspect{}, fmt.Errorf("inspect Docker container returned HTTP %d", response.StatusCode)
	}
	var inspection dockerContainerInspect
	if err := json.NewDecoder(response.Body).Decode(&inspection); err != nil {
		return dockerContainerInspect{}, fmt.Errorf("decode Docker container: %w", err)
	}
	return inspection, nil
}

func runController(env environment) error {
	socketPath := envValue(env, "DOCKER_SOCKET", defaultDockerSocket)
	project := envValue(env, "TORRENTIAL_COMPOSE_PROJECT", "torrential")
	manager := newDockerContainerManager(socketPath, project)
	server := &http.Server{
		Addr:              controllerAddress,
		Handler:           newControlServer(manager).handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errChannel := make(chan error, 1)
	go func() {
		logController("listening on " + controllerAddress)
		errChannel <- server.ListenAndServe()
	}()
	select {
	case err := <-errChannel:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}

func probeController() error {
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://127.0.0.1:8080/healthz")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("controller probe returned HTTP %d", response.StatusCode)
	}
	return nil
}

func logController(message string) {
	fmt.Printf("[controller] %s\n", message)
}

func controllerSocketExists(env environment) error {
	socketPath := envValue(env, "DOCKER_SOCKET", defaultDockerSocket)
	if _, err := os.Stat(socketPath); err != nil {
		return fmt.Errorf("Docker socket %s is unavailable: %w", socketPath, err)
	}
	return nil
}
