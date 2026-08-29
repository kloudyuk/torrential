package main

import (
	"fmt"
	"strings"
)

const (
	flareSolverrName    = "FlareSolverr"
	flareSolverrTag     = "flaresolverr"
	flareSolverrURL     = "http://127.0.0.1:8191"
	flareSolverrTimeout = 60
)

type prowlarrTag struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

func (client *prowlarrClient) ensureFlareSolverrProxy() (string, error) {
	tagID, err := client.ensureTag(flareSolverrTag)
	if err != nil {
		return "", err
	}

	var proxies, schemas []providerResource
	if err := client.api.get("/api/v1/indexerProxy", &proxies); err != nil {
		return "", err
	}
	if err := client.api.get("/api/v1/indexerProxy/schema", &schemas); err != nil {
		return "", err
	}
	existing, err := selectExisting(proxies, flareSolverrName)
	if err != nil {
		return "", err
	}
	schema := findImplementation(schemas, flareSolverrName)
	if schema == nil {
		return "", fmt.Errorf("Prowlarr does not expose a %s indexer proxy schema", flareSolverrName)
	}
	base := schema
	if existing != nil {
		base = existing
	}
	fields, err := updateFields(base["fields"], map[string]any{
		"host":           flareSolverrURL,
		"requestTimeout": flareSolverrTimeout,
	})
	if err != nil {
		return "", err
	}
	payload := cloneResource(base)
	payload["name"] = flareSolverrName
	payload["fields"] = fields
	payload["tags"] = appendTag(resourceTagIDs(base["tags"]), tagID)

	if id, ok := resourceID(existing); ok {
		if err := client.api.put(fmt.Sprintf("/api/v1/indexerProxy/%d", id), payload); err != nil {
			return "", err
		}
		return "updated", nil
	}
	var created providerResource
	if err := client.api.post("/api/v1/indexerProxy", payload, &created); err != nil {
		return "", err
	}
	return "created", nil
}

func (client *prowlarrClient) ensureTag(label string) (int, error) {
	var tags []prowlarrTag
	if err := client.api.get("/api/v1/tag", &tags); err != nil {
		return 0, err
	}
	for _, tag := range tags {
		if strings.EqualFold(strings.TrimSpace(tag.Label), strings.TrimSpace(label)) {
			return tag.ID, nil
		}
	}
	var created prowlarrTag
	if err := client.api.post("/api/v1/tag", map[string]any{"label": label}, &created); err != nil {
		return 0, err
	}
	if created.ID <= 0 {
		return 0, fmt.Errorf("Prowlarr did not return an ID for tag %q", label)
	}
	return created.ID, nil
}

func resourceTagIDs(value any) []int {
	values, _ := value.([]any)
	result := make([]int, 0, len(values))
	for _, value := range values {
		switch id := value.(type) {
		case float64:
			if id > 0 && id == float64(int(id)) {
				result = append(result, int(id))
			}
		case int:
			if id > 0 {
				result = append(result, id)
			}
		}
	}
	return result
}

func appendTag(tags []int, tagID int) []int {
	for _, existing := range tags {
		if existing == tagID {
			return tags
		}
	}
	return append(tags, tagID)
}
