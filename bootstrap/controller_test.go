package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"
)

type fakeContainerManager struct {
	mutex           sync.Mutex
	serviceStatuses []serviceRuntimeStatus
	restarts        []string
	waitedFor       []string
	restartErr      error
	restartCalled   chan string
	restartBlock    chan struct{}
}

func (manager *fakeContainerManager) statuses(_ context.Context, _ []string) ([]serviceRuntimeStatus, error) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	return append([]serviceRuntimeStatus(nil), manager.serviceStatuses...), nil
}

func (manager *fakeContainerManager) restart(ctx context.Context, service string) error {
	manager.mutex.Lock()
	manager.restarts = append(manager.restarts, service)
	manager.mutex.Unlock()
	if manager.restartCalled != nil {
		manager.restartCalled <- service
	}
	if manager.restartBlock != nil {
		select {
		case <-manager.restartBlock:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return manager.restartErr
}

func (manager *fakeContainerManager) waitHealthy(_ context.Context, service string, _ time.Duration) error {
	manager.mutex.Lock()
	manager.waitedFor = append(manager.waitedFor, service)
	manager.mutex.Unlock()
	return nil
}

func (manager *fakeContainerManager) recordedRestarts() []string {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	return append([]string(nil), manager.restarts...)
}

func TestControllerStatusReportsDockerStateAndStartedAt(t *testing.T) {
	startedAt := "2026-08-30T08:00:00Z"
	manager := &fakeContainerManager{serviceStatuses: []serviceRuntimeStatus{
		{Service: "seerr", State: "running", Health: "healthy", StartedAt: startedAt},
		{Service: "sonarr", State: "exited", Health: "unavailable"},
	}}
	request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	response := httptest.NewRecorder()
	newControlServer(manager).handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", response.Code)
	}
	var body controllerResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(body.Services, manager.serviceStatuses) {
		t.Fatalf("unexpected service statuses: %#v", body.Services)
	}
}

func TestControllerRestartsAllowedService(t *testing.T) {
	manager := &fakeContainerManager{restartCalled: make(chan string, 1)}
	server := newControlServer(manager)
	request := restartRequest("/api/restart/sonarr")
	response := httptest.NewRecorder()
	server.handler().ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("expected HTTP 202, got %d: %s", response.Code, response.Body.String())
	}
	select {
	case service := <-manager.restartCalled:
		if service != "sonarr" {
			t.Fatalf("expected sonarr restart, got %s", service)
		}
	case <-time.After(time.Second):
		t.Fatal("restart was not called")
	}
}

func TestControllerRejectsUnlistedService(t *testing.T) {
	manager := &fakeContainerManager{}
	request := restartRequest("/api/restart/controller")
	response := httptest.NewRecorder()
	newControlServer(manager).handler().ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected HTTP 404, got %d", response.Code)
	}
}

func TestControllerRejectsCrossOriginRestart(t *testing.T) {
	manager := &fakeContainerManager{}
	request := restartRequest("/api/restart/sonarr")
	request.Header.Set("Origin", "http://attacker.example")
	response := httptest.NewRecorder()
	newControlServer(manager).handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected HTTP 403, got %d", response.Code)
	}
}

func TestControllerAllowsOnlyOneRestartAtATime(t *testing.T) {
	block := make(chan struct{})
	manager := &fakeContainerManager{restartCalled: make(chan string, 1), restartBlock: block}
	server := newControlServer(manager)
	firstResponse := httptest.NewRecorder()
	server.handler().ServeHTTP(firstResponse, restartRequest("/api/restart/sonarr"))
	<-manager.restartCalled
	secondResponse := httptest.NewRecorder()
	server.handler().ServeHTTP(secondResponse, restartRequest("/api/restart/radarr"))
	close(block)
	if secondResponse.Code != http.StatusConflict {
		t.Fatalf("expected HTTP 409, got %d", secondResponse.Code)
	}
}

func TestControllerRestartsStackInDependencyOrder(t *testing.T) {
	manager := &fakeContainerManager{}
	server := newControlServer(manager)
	response := httptest.NewRecorder()
	server.handler().ServeHTTP(response, restartRequest("/api/restart-all"))
	if response.Code != http.StatusAccepted {
		t.Fatalf("expected HTTP 202, got %d", response.Code)
	}
	deadline := time.Now().Add(time.Second)
	for {
		server.mutex.RLock()
		running := server.operation.Running
		server.mutex.RUnlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stack restart did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	expected := []string{"gluetun", "transmission", "prowlarr", "byparr", "sonarr", "radarr", "seerr", "plex"}
	if actual := manager.recordedRestarts(); !reflect.DeepEqual(actual, expected) {
		t.Fatalf("unexpected restart order: %#v", actual)
	}
	if !reflect.DeepEqual(manager.waitedFor, []string{"gluetun"}) {
		t.Fatalf("expected Gluetun health wait, got %#v", manager.waitedFor)
	}
}

func TestControllerReportsCurrentFullRestartService(t *testing.T) {
	block := make(chan struct{})
	manager := &fakeContainerManager{
		restartCalled: make(chan string, len(fullRestartGroups)*4),
		restartBlock:  block,
	}
	server := newControlServer(manager)
	response := httptest.NewRecorder()
	server.handler().ServeHTTP(response, restartRequest("/api/restart-all"))
	if response.Code != http.StatusAccepted {
		t.Fatalf("expected HTTP 202, got %d", response.Code)
	}
	select {
	case service := <-manager.restartCalled:
		if service != "gluetun" {
			t.Fatalf("expected gluetun restart, got %s", service)
		}
	case <-time.After(time.Second):
		t.Fatal("stack restart was not called")
	}
	server.mutex.RLock()
	operation := server.operation
	server.mutex.RUnlock()
	if operation.Service != "gluetun" || operation.Message != "Restarting gluetun" {
		t.Fatalf("unexpected current operation: %#v", operation)
	}
	close(block)
	deadline := time.Now().Add(time.Second)
	for {
		server.mutex.RLock()
		running := server.operation.Running
		server.mutex.RUnlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stack restart did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestControllerReportsRestartFailure(t *testing.T) {
	manager := &fakeContainerManager{restartErr: errors.New("restart unavailable")}
	server := newControlServer(manager)
	response := httptest.NewRecorder()
	server.handler().ServeHTTP(response, restartRequest("/api/restart/plex"))
	if response.Code != http.StatusAccepted {
		t.Fatalf("expected HTTP 202, got %d", response.Code)
	}
	deadline := time.Now().Add(time.Second)
	for {
		server.mutex.RLock()
		operation := server.operation
		server.mutex.RUnlock()
		if !operation.Running {
			if operation.Error != "restart unavailable" {
				t.Fatalf("unexpected operation error: %q", operation.Error)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("restart operation did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func restartRequest(target string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, target, nil)
	request.Host = "dashboard.local"
	request.Header.Set("Origin", "http://dashboard.local")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Torrential-Action", "restart")
	return request
}
