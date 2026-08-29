package main

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type arrClient struct {
	name   string
	config arrConfig
	api    *apiClient
}

type qualityProfile struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func newArrClient(name string, config arrConfig, apiKey string, timeout time.Duration) *arrClient {
	return &arrClient{name: name, config: config, api: newAPIClient(config.baseURL, apiKey, timeout)}
}

func (client *arrClient) ensureRootFolder() (bool, error) {
	var roots []struct {
		Path string `json:"path"`
	}
	if err := client.api.get("/api/v3/rootfolder", &roots); err != nil {
		return false, err
	}
	expected := normalizePath(client.config.rootFolder)
	for _, root := range roots {
		if normalizePath(root.Path) == expected {
			return false, nil
		}
	}
	var created map[string]any
	if err := client.api.post("/api/v3/rootfolder", map[string]any{"path": client.config.rootFolder}, &created); err != nil {
		return false, err
	}
	return true, nil
}

func (client *arrClient) ensureTransmission(transmission transmissionConfig) (string, error) {
	var clients, schemas []providerResource
	if err := client.api.get("/api/v3/downloadclient", &clients); err != nil {
		return "", err
	}
	if err := client.api.get("/api/v3/downloadclient/schema", &schemas); err != nil {
		return "", err
	}
	existing, err := selectExisting(clients, "Transmission")
	if err != nil {
		return "", err
	}
	schema := findImplementation(schemas, "Transmission")
	if schema == nil {
		return "", fmt.Errorf("%s does not expose a Transmission schema", client.name)
	}
	base := schema
	if existing != nil {
		base = existing
	}

	categoryField := "movieCategory"
	if client.name == "Sonarr" {
		categoryField = "tvCategory"
	}
	fields, err := updateFields(base["fields"], map[string]any{
		"host":        "transmission",
		"port":        9091,
		"useSsl":      false,
		"urlBase":     "/transmission/",
		"username":    "",
		"password":    "",
		categoryField: client.config.category,
	})
	if err != nil {
		return "", err
	}
	payload := cloneResource(base)
	payload["name"] = "Transmission"
	payload["enable"] = true
	payload["priority"] = 1
	payload["removeCompletedDownloads"] = true
	payload["removeFailedDownloads"] = true
	payload["fields"] = fields

	if id, ok := resourceID(existing); ok {
		if err := client.api.put(fmt.Sprintf("/api/v3/downloadclient/%d", id), payload); err != nil {
			return "", err
		}
		return "updated", nil
	}
	var created providerResource
	if err := client.api.post("/api/v3/downloadclient", payload, &created); err != nil {
		return "", err
	}
	return "created", nil
}

func (client *arrClient) ensurePlexNotification(plexURL, token string) (string, error) {
	parsed, err := url.Parse(plexURL)
	if err != nil {
		return "", fmt.Errorf("parse Plex notification URL: %w", err)
	}
	port := 32400
	if parsed.Port() != "" {
		port, err = strconv.Atoi(parsed.Port())
		if err != nil {
			return "", fmt.Errorf("Plex notification URL has an invalid port")
		}
	}

	var notifications, schemas []providerResource
	if err := client.api.get("/api/v3/notification", &notifications); err != nil {
		return "", err
	}
	if err := client.api.get("/api/v3/notification/schema", &schemas); err != nil {
		return "", err
	}
	existing, err := selectExisting(notifications, "PlexServer")
	if err != nil {
		return "", err
	}
	schema := findImplementation(schemas, "PlexServer")
	if schema == nil {
		return "", fmt.Errorf("%s does not expose a Plex Media Server schema", client.name)
	}
	base := schema
	if existing != nil {
		base = existing
	}
	fields, err := updateFields(base["fields"], map[string]any{
		"host":          parsed.Hostname(),
		"port":          port,
		"useSsl":        parsed.Scheme == "https",
		"urlBase":       strings.Trim(parsed.Path, "/"),
		"authToken":     token,
		"updateLibrary": true,
		"mapFrom":       "",
		"mapTo":         "",
	})
	if err != nil {
		return "", err
	}
	payload := cloneResource(base)
	payload["name"] = "Plex"
	payload["onDownload"] = true
	payload["onUpgrade"] = true
	payload["fields"] = fields

	if id, ok := resourceID(existing); ok {
		if err := client.api.put(fmt.Sprintf("/api/v3/notification/%d", id), payload); err != nil {
			return "", err
		}
		return "updated", nil
	}
	var created providerResource
	if err := client.api.post("/api/v3/notification", payload, &created); err != nil {
		return "", err
	}
	return "created", nil
}

func (client *arrClient) qualityProfile(name string) (qualityProfile, error) {
	var profiles []qualityProfile
	if err := client.api.get("/api/v3/qualityprofile", &profiles); err != nil {
		return qualityProfile{}, err
	}
	for _, profile := range profiles {
		if strings.EqualFold(strings.TrimSpace(profile.Name), strings.TrimSpace(name)) {
			return profile, nil
		}
	}
	available := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		available = append(available, profile.Name)
	}
	return qualityProfile{}, fmt.Errorf(
		"%s quality profile %q was not found (available: %s)",
		client.name,
		name,
		strings.Join(available, ", "),
	)
}

type prowlarrClient struct {
	config serviceConfig
	api    *apiClient
}

func newProwlarrClient(config serviceConfig, apiKey string, timeout time.Duration) *prowlarrClient {
	return &prowlarrClient{config: config, api: newAPIClient(config.baseURL, apiKey, timeout)}
}

func (client *prowlarrClient) ensureApplication(name, baseURL, apiKey string) (string, error) {
	var applications, schemas []providerResource
	if err := client.api.get("/api/v1/applications", &applications); err != nil {
		return "", err
	}
	if err := client.api.get("/api/v1/applications/schema", &schemas); err != nil {
		return "", err
	}
	matches := matchingImplementations(applications, name)
	if len(matches) > 1 {
		return "", fmt.Errorf("cannot adopt one of %d existing %s applications", len(matches), name)
	}
	var existing providerResource
	if len(matches) == 1 {
		existing = matches[0]
	}
	schema := findImplementation(schemas, name)
	if schema == nil {
		return "", fmt.Errorf("Prowlarr does not expose a %s schema", name)
	}
	base := schema
	if existing != nil {
		base = existing
	}
	fields, err := updateFields(base["fields"], map[string]any{
		"prowlarrUrl": client.config.baseURL,
		"baseUrl":     baseURL,
		"apiKey":      apiKey,
	})
	if err != nil {
		return "", err
	}
	payload := cloneResource(base)
	payload["name"] = name
	payload["enable"] = true
	payload["syncLevel"] = "fullSync"
	payload["fields"] = fields

	if id, ok := resourceID(existing); ok {
		if err := client.api.put(fmt.Sprintf("/api/v1/applications/%d", id), payload); err != nil {
			return "", err
		}
		return "updated", nil
	}
	var created providerResource
	if err := client.api.post("/api/v1/applications", payload, &created); err != nil {
		return "", err
	}
	return "created", nil
}

func selectExisting(resources []providerResource, implementation string) (providerResource, error) {
	matches := matchingImplementations(resources, implementation)
	for _, resource := range matches {
		if resourceString(resource, "name") == implementation {
			return resource, nil
		}
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("cannot adopt one of %d existing %s clients", len(matches), implementation)
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return nil, nil
}

func matchingImplementations(resources []providerResource, implementation string) []providerResource {
	var matches []providerResource
	for _, resource := range resources {
		if resourceString(resource, "implementation") == implementation {
			matches = append(matches, resource)
		}
	}
	return matches
}

func findImplementation(resources []providerResource, implementation string) providerResource {
	for _, resource := range resources {
		if resourceString(resource, "implementation") == implementation {
			return resource
		}
	}
	return nil
}

func cloneResource(resource providerResource) providerResource {
	clone := make(providerResource, len(resource))
	for key, value := range resource {
		clone[key] = value
	}
	return clone
}

func normalizePath(path string) string {
	if path == "/" {
		return path
	}
	return strings.TrimRight(path, "/")
}
