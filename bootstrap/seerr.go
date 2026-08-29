package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type seerrConfig struct {
	baseURL              string
	configFile           string
	sonarrQualityProfile string
	radarrQualityProfile string
	locale               localeConfig
}

type seerrClient struct {
	config seerrConfig
	api    *apiClient
	http   *http.Client
}

type seerrPublicSettings struct {
	Initialized          bool   `json:"initialized"`
	PlexClientIdentifier string `json:"plexClientIdentifier"`
}

type seerrPlexSettings struct {
	MachineID string `json:"machineId"`
}

type seerrLibrary struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Type    string `json:"type"`
}

type seerrDVRSettings struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	Port     int    `json:"port"`
}

func loadSeerrConfig(env environment, locale localeConfig) (seerrConfig, error) {
	baseURL, err := envURL(env, "SEERR_URL", "http://seerr:5055")
	if err != nil {
		return seerrConfig{}, err
	}
	return seerrConfig{
		baseURL:              baseURL,
		configFile:           envValue(env, "SEERR_CONFIG_FILE", "/config/seerr/settings.json"),
		sonarrQualityProfile: envValue(env, "SEERR_SONARR_QUALITY_PROFILE", "HD-1080p"),
		radarrQualityProfile: envValue(env, "SEERR_RADARR_QUALITY_PROFILE", "HD-1080p"),
		locale:               locale,
	}, nil
}

func (client *seerrClient) configureLocale() error {
	var settings map[string]any
	return client.api.post("/api/v1/settings/main", map[string]any{
		"locale":           client.config.locale.language,
		"discoverRegion":   client.config.locale.region,
		"streamingRegion":  client.config.locale.region,
		"originalLanguage": client.config.locale.language,
	}, &settings)
}

func newSeerrClient(configuration seerrConfig, timeout time.Duration) (*seerrClient, error) {
	apiKey, err := readSeerrAPIKey(configuration.configFile)
	if err != nil {
		return nil, err
	}
	return &seerrClient{
		config: configuration,
		api:    newAPIClient(configuration.baseURL, apiKey, timeout),
		http:   &http.Client{Timeout: timeout},
	}, nil
}

func readSeerrAPIKey(filename string) (string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", filename, err)
	}
	var settings struct {
		Main struct {
			APIKey string `json:"apiKey"`
		} `json:"main"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return "", fmt.Errorf("cannot parse %s: %w", filename, err)
	}
	key := strings.TrimSpace(settings.Main.APIKey)
	if key == "" {
		return "", fmt.Errorf("%s does not contain an API key", filename)
	}
	return key, nil
}

func (client *seerrClient) publicSettings() (seerrPublicSettings, error) {
	var settings seerrPublicSettings
	status, err := client.request(http.MethodGet, "/api/v1/settings/public", "", nil, &settings)
	if err != nil {
		return settings, err
	}
	if status != http.StatusOK {
		return settings, fmt.Errorf("Seerr public settings returned HTTP %d", status)
	}
	if strings.TrimSpace(settings.PlexClientIdentifier) == "" {
		return settings, fmt.Errorf("Seerr did not provide a Plex client identifier")
	}
	return settings, nil
}

func (client *seerrClient) hasAdministrator() (bool, error) {
	status, err := client.request(http.MethodGet, "/api/v1/auth/me", client.api.apiKey, nil, nil)
	if err != nil {
		return false, err
	}
	switch status {
	case http.StatusOK:
		return true, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return false, nil
	default:
		return false, fmt.Errorf("Seerr administrator check returned HTTP %d", status)
	}
}

func (client *seerrClient) isPlexConfigured() (bool, error) {
	var settings seerrPlexSettings
	if err := client.api.get("/api/v1/settings/plex", &settings); err != nil {
		return false, err
	}
	return strings.TrimSpace(settings.MachineID) != "", nil
}

func (client *seerrClient) authenticatePlex(accountToken string) error {
	status, err := client.request(
		http.MethodPost,
		"/api/v1/auth/plex",
		"",
		map[string]string{"authToken": accountToken},
		nil,
	)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("Seerr Plex authentication returned HTTP %d", status)
	}
	return nil
}

func (client *seerrClient) configurePlex() error {
	var settings map[string]any
	if err := client.api.post("/api/v1/settings/plex", map[string]any{
		"ip":     "plex",
		"port":   32400,
		"useSsl": false,
	}, &settings); err != nil {
		return err
	}

	var libraries []seerrLibrary
	if err := client.api.get("/api/v1/settings/plex/library?sync=true", &libraries); err != nil {
		return err
	}
	enabled := make([]string, 0, len(libraries))
	for _, library := range libraries {
		if library.Type == "show" || library.Type == "movie" {
			enabled = append(enabled, library.ID)
		}
	}
	if len(enabled) == 0 {
		return fmt.Errorf("Seerr did not discover the Plex TV and movie libraries")
	}
	path := "/api/v1/settings/plex/library?enable=" + url.QueryEscape(strings.Join(enabled, ","))
	if err := client.api.get(path, &libraries); err != nil {
		return err
	}
	return nil
}

func (client *seerrClient) ensureArr(
	kind string,
	arr *arrClient,
	apiKey string,
	profileName string,
) (string, error) {
	profile, err := arr.qualityProfile(profileName)
	if err != nil {
		return "", err
	}
	payload := map[string]any{
		"name":              arr.name,
		"hostname":          strings.ToLower(arr.name),
		"port":              7878,
		"apiKey":            apiKey,
		"useSsl":            false,
		"baseUrl":           "",
		"activeProfileId":   profile.ID,
		"activeProfileName": profile.Name,
		"activeDirectory":   arr.config.rootFolder,
		"tags":              []int{},
		"is4k":              false,
		"isDefault":         true,
		"syncEnabled":       true,
		"preventSearch":     false,
		"tagRequests":       false,
		"overrideRule":      []int{},
	}
	if kind == "sonarr" {
		payload["port"] = 8989
		payload["seriesType"] = "standard"
		payload["animeSeriesType"] = "anime"
		payload["enableSeasonFolders"] = true
		payload["monitorNewItems"] = "all"
		payload["animeTags"] = []int{}
	} else {
		payload["minimumAvailability"] = "released"
	}

	var existing []seerrDVRSettings
	if err := client.api.get("/api/v1/settings/"+kind, &existing); err != nil {
		return "", err
	}
	matches := make([]seerrDVRSettings, 0, 1)
	for _, setting := range existing {
		if strings.EqualFold(setting.Name, arr.name) ||
			(setting.Hostname == strings.ToLower(arr.name) && setting.Port == payload["port"]) {
			matches = append(matches, setting)
		}
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("cannot adopt one of %d existing Seerr %s services", len(matches), arr.name)
	}
	if len(matches) == 1 {
		if err := client.api.put(fmt.Sprintf("/api/v1/settings/%s/%d", kind, matches[0].ID), payload); err != nil {
			return "", err
		}
		return "updated", nil
	}
	var created seerrDVRSettings
	if err := client.api.post("/api/v1/settings/"+kind, payload, &created); err != nil {
		return "", err
	}
	return "created", nil
}

func (client *seerrClient) initialize(alreadyInitialized bool) error {
	if alreadyInitialized {
		return nil
	}
	var result map[string]any
	return client.api.post("/api/v1/settings/initialize", map[string]any{}, &result)
}

func (client *seerrClient) request(method, path, apiKey string, body, result any) (int, error) {
	var requestBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("encode Seerr request: %w", err)
		}
		requestBody = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, client.config.baseURL+path, requestBody)
	if err != nil {
		return 0, fmt.Errorf("create Seerr request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		request.Header.Set("X-Api-Key", apiKey)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return 0, fmt.Errorf("Seerr request failed: %w", err)
	}
	defer response.Body.Close()
	if result != nil && response.StatusCode >= 200 && response.StatusCode < 300 {
		if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(result); err != nil && err != io.EOF {
			return response.StatusCode, fmt.Errorf("Seerr %s returned invalid JSON: %w", path, err)
		}
	}
	return response.StatusCode, nil
}
