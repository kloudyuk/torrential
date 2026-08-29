package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type transmissionClient struct {
	config    transmissionConfig
	http      *http.Client
	sessionID string
}

func newTransmissionClient(config transmissionConfig, timeout time.Duration) *transmissionClient {
	return &transmissionClient{config: config, http: &http.Client{Timeout: timeout}}
}

func (client *transmissionClient) configureDirectories(complete, incomplete string) (bool, error) {
	session := make(map[string]any)
	if err := client.rpc("session-get", nil, &session); err != nil {
		return false, err
	}
	if session["download-dir"] == complete &&
		session["incomplete-dir"] == incomplete &&
		session["incomplete-dir-enabled"] == true {
		return false, nil
	}
	arguments := map[string]any{
		"download-dir":           complete,
		"incomplete-dir":         incomplete,
		"incomplete-dir-enabled": true,
	}
	var ignored map[string]any
	if err := client.rpc("session-set", arguments, &ignored); err != nil {
		return false, err
	}
	return true, nil
}

func (client *transmissionClient) rpc(method string, arguments map[string]any, result any) error {
	response, err := client.send(method, arguments)
	if err != nil {
		return err
	}
	if response.StatusCode == http.StatusConflict {
		client.sessionID = response.Header.Get("X-Transmission-Session-Id")
		response.Body.Close()
		if client.sessionID == "" {
			return fmt.Errorf("Transmission did not provide an RPC session ID")
		}
		response, err = client.send(method, arguments)
		if err != nil {
			return err
		}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Transmission returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Arguments json.RawMessage `json:"arguments"`
		Result    string          `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&payload); err != nil {
		return fmt.Errorf("Transmission returned invalid JSON: %w", err)
	}
	if payload.Result != "success" || len(payload.Arguments) == 0 {
		return fmt.Errorf("Transmission RPC %s failed", method)
	}
	if result != nil {
		if err := json.Unmarshal(payload.Arguments, result); err != nil {
			return fmt.Errorf("Transmission RPC %s returned invalid arguments: %w", method, err)
		}
	}
	return nil
}

func (client *transmissionClient) send(method string, arguments map[string]any) (*http.Response, error) {
	payload := map[string]any{"method": method}
	if arguments != nil {
		payload["arguments"] = arguments
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode Transmission request: %w", err)
	}
	request, err := http.NewRequest(http.MethodPost, client.config.rpcURL, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("create Transmission request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	if client.sessionID != "" {
		request.Header.Set("X-Transmission-Session-Id", client.sessionID)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("Transmission request failed: %w", err)
	}
	return response, nil
}
