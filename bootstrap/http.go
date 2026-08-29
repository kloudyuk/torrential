package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

type providerResource map[string]any

type apiClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func newAPIClient(baseURL, apiKey string, timeout time.Duration) *apiClient {
	return &apiClient{baseURL: baseURL, apiKey: apiKey, http: &http.Client{Timeout: timeout}}
}

func (client *apiClient) get(path string, result any) error {
	return client.request(http.MethodGet, path, nil, result)
}

func (client *apiClient) post(path string, body, result any) error {
	return client.request(http.MethodPost, path, body, result)
}

func (client *apiClient) put(path string, body any) error {
	return client.request(http.MethodPut, path, body, nil)
}

func (client *apiClient) request(method, path string, body, result any) error {
	var requestBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request for %s%s: %w", client.baseURL, path, err)
		}
		requestBody = bytes.NewReader(encoded)
	}

	request, err := http.NewRequest(method, client.baseURL+path, requestBody)
	if err != nil {
		return fmt.Errorf("create request for %s%s: %w", client.baseURL, path, err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Api-Key", client.apiKey)

	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("request to %s%s failed: %w", client.baseURL, path, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, readErr := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		if readErr != nil {
			return fmt.Errorf("%s%s returned HTTP %d (read response: %v)", client.baseURL, path, response.StatusCode, readErr)
		}
		message := strings.TrimSpace(string(data))
		if message != "" {
			return fmt.Errorf("%s%s returned HTTP %d: %s", client.baseURL, path, response.StatusCode, message)
		}
		return fmt.Errorf("%s%s returned HTTP %d", client.baseURL, path, response.StatusCode)
	}
	if result == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}

	decoder := json.NewDecoder(io.LimitReader(response.Body, 8<<20))
	if err := decoder.Decode(result); err != nil {
		if err == io.EOF {
			return nil
		}
		return fmt.Errorf("%s%s returned invalid JSON: %w", client.baseURL, path, err)
	}
	return nil
}

func updateFields(value any, replacements map[string]any) ([]any, error) {
	fields, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("provider schema does not contain fields")
	}
	remaining := make(map[string]struct{}, len(replacements))
	for name := range replacements {
		remaining[name] = struct{}{}
	}

	updated := make([]any, 0, len(fields))
	for _, value := range fields {
		field, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("provider schema contains an invalid field")
		}
		copy := make(map[string]any, len(field))
		for key, value := range field {
			copy[key] = value
		}
		name, _ := field["name"].(string)
		if replacement, found := replacements[name]; found {
			copy["value"] = replacement
			delete(remaining, name)
		}
		updated = append(updated, copy)
	}

	if len(remaining) != 0 {
		missing := make([]string, 0, len(remaining))
		for name := range remaining {
			missing = append(missing, name)
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("provider schema is missing fields: %s", strings.Join(missing, ", "))
	}
	return updated, nil
}

func resourceString(resource providerResource, key string) string {
	value, _ := resource[key].(string)
	return value
}

func resourceID(resource providerResource) (int, bool) {
	switch value := resource["id"].(type) {
	case float64:
		return int(value), value == float64(int(value))
	case int:
		return value, true
	default:
		return 0, false
	}
}
