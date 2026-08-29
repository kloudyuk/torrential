package main

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

type readinessCheck struct {
	name  string
	check func() error
}

func waitForPrerequisites(configuration config) error {
	requestTimeout := configuration.requestTimeout
	if requestTimeout > 2*time.Second {
		requestTimeout = 2 * time.Second
	}
	httpClient := &http.Client{Timeout: requestTimeout}
	checks := []readinessCheck{
		fileReadinessCheck("Sonarr configuration", configuration.sonarr.configFile),
		fileReadinessCheck("Radarr configuration", configuration.radarr.configFile),
		fileReadinessCheck("Prowlarr configuration", configuration.prowlarr.configFile),
		fileReadinessCheck("Plex configuration", configuration.plex.configFile),
		fileReadinessCheck("Seerr configuration", configuration.seerr.configFile),
		httpReadinessCheck(httpClient, "Sonarr API", http.MethodGet, configuration.sonarr.baseURL+"/ping", nil),
		httpReadinessCheck(httpClient, "Radarr API", http.MethodGet, configuration.radarr.baseURL+"/ping", nil),
		httpReadinessCheck(httpClient, "Prowlarr API", http.MethodGet, configuration.prowlarr.baseURL+"/ping", nil),
		httpReadinessCheck(httpClient, "Plex API", http.MethodGet, configuration.plex.baseURL+"/identity", nil),
		httpReadinessCheck(httpClient, "Seerr API", http.MethodGet, configuration.seerr.baseURL+"/api/v1/settings/public", nil),
		httpReadinessCheck(httpClient, "Transmission RPC", http.MethodPost, configuration.transmission.rpcURL, []byte("{}")),
	}

	logMessage("waiting for service readiness")
	deadline := time.Now().Add(configuration.startupTimeout)
	var pending []string
	for {
		pending = pending[:0]
		for _, readiness := range checks {
			if err := readiness.check(); err != nil {
				pending = append(pending, readiness.name)
			}
		}
		if len(pending) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("services did not become ready within %s: %s", configuration.startupTimeout, strings.Join(pending, ", "))
		}
		delay := time.Second
		if remaining := time.Until(deadline); remaining < delay {
			delay = remaining
		}
		if delay > 0 {
			time.Sleep(delay)
		}
	}
}

func fileReadinessCheck(name, filename string) readinessCheck {
	return readinessCheck{name: name, check: func() error {
		information, err := os.Stat(filename)
		if err != nil {
			return err
		}
		if information.IsDir() || information.Size() == 0 {
			return fmt.Errorf("%s is not a populated file", filename)
		}
		return nil
	}}
}

func httpReadinessCheck(client *http.Client, name, method, endpoint string, body []byte) readinessCheck {
	return readinessCheck{name: name, check: func() error {
		request, err := http.NewRequest(method, endpoint, bytes.NewReader(body))
		if err != nil {
			return err
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		response.Body.Close()
		if response.StatusCode >= http.StatusInternalServerError {
			return fmt.Errorf("%s returned HTTP %d", endpoint, response.StatusCode)
		}
		return nil
	}}
}
