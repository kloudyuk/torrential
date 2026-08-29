package main

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var errPlexUnclaimed = errors.New("Plex is not claimed")

type plexConfig struct {
	baseURL               string
	plexTVURL             string
	authorizeURL          string
	configFile            string
	language              string
	libraries             []plexLibrary
	timeout               time.Duration
	authTimeout           time.Duration
	authPollInterval      time.Duration
	readinessTimeout      time.Duration
	readinessPollInterval time.Duration
}

type plexLibrary struct {
	name     string
	typeName string
	agent    string
	scanner  string
	location string
}

type plexClient struct {
	baseURL          string
	token            string
	http             *http.Client
	readinessTimeout time.Duration
	retryInterval    time.Duration
}

type plexHTTPError struct {
	path       string
	statusCode int
	response   string
}

func (err *plexHTTPError) Error() string {
	message := fmt.Sprintf("Plex %s returned HTTP %d", err.path, err.statusCode)
	if err.response != "" {
		message += ": " + err.response
	}
	return message
}

type plexSection struct {
	Title    string `json:"title"`
	Type     string `json:"type"`
	Location []struct {
		Path string `json:"path"`
	} `json:"Location"`
}

func loadPlexConfig(env environment, timeout time.Duration, tvRoot, movieRoot string) (plexConfig, error) {
	baseURL, err := envURL(env, "PLEX_URL", "http://plex:32400")
	if err != nil {
		return plexConfig{}, err
	}
	plexTVURL, err := envURL(env, "PLEX_TV_URL", "https://plex.tv")
	if err != nil {
		return plexConfig{}, err
	}
	authorizeURL, err := envURL(env, "PLEX_AUTHORIZE_URL", "https://app.plex.tv/auth")
	if err != nil {
		return plexConfig{}, err
	}
	return plexConfig{
		baseURL:               baseURL,
		plexTVURL:             plexTVURL,
		authorizeURL:          authorizeURL,
		configFile:            envValue(env, "PLEX_CONFIG_FILE", "/plex-config/Library/Application Support/Plex Media Server/Preferences.xml"),
		language:              envValue(env, "PLEX_LIBRARY_LANGUAGE", "en-US"),
		timeout:               timeout,
		authTimeout:           15 * time.Minute,
		authPollInterval:      2 * time.Second,
		readinessTimeout:      2 * time.Minute,
		readinessPollInterval: 2 * time.Second,
		libraries: []plexLibrary{
			{name: "TV Shows", typeName: "show", agent: "tv.plex.agents.series", scanner: "Plex TV Series", location: tvRoot},
			{name: "Movies", typeName: "movie", agent: "tv.plex.agents.movie", scanner: "Plex Movie", location: movieRoot},
		},
	}, nil
}

func configurePlexLibraries(configuration plexConfig, token string) error {
	client := newPlexClient(configuration.baseURL, token, configuration.timeout)
	client.readinessTimeout = configuration.readinessTimeout
	client.retryInterval = configuration.readinessPollInterval
	for _, library := range configuration.libraries {
		state, err := client.ensureLibrary(library, configuration.language)
		if err != nil {
			return err
		}
		logMessage("Plex " + library.name + " library " + state)
	}
	return nil
}

func newPlexClient(baseURL, token string, requestTimeout time.Duration) plexClient {
	return plexClient{
		baseURL:          baseURL,
		token:            token,
		http:             &http.Client{Timeout: requestTimeout},
		readinessTimeout: 2 * time.Minute,
		retryInterval:    2 * time.Second,
	}
}

func readPlexToken(filename string) (string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return "", errPlexUnclaimed
		}
		return "", fmt.Errorf("cannot read %s: %w", filename, err)
	}
	var preferences struct {
		Token string `xml:"PlexOnlineToken,attr"`
	}
	if err := xml.Unmarshal(data, &preferences); err != nil {
		return "", fmt.Errorf("cannot parse %s: %w", filename, err)
	}
	token := strings.TrimSpace(preferences.Token)
	if token == "" {
		return "", errPlexUnclaimed
	}
	return token, nil
}

func (client plexClient) ensureLibrary(library plexLibrary, language string) (string, error) {
	deadline := time.Now().Add(client.readinessTimeout)
	for {
		state, err := client.ensureLibraryOnce(library, language)
		if err == nil || !isPlexReadinessError(err) || client.readinessTimeout <= 0 || !time.Now().Before(deadline) {
			return state, err
		}
		delay := client.retryInterval
		if delay <= 0 {
			delay = time.Second
		}
		if remaining := time.Until(deadline); delay > remaining {
			delay = remaining
		}
		logMessage("Plex is not ready; retrying library configuration")
		time.Sleep(delay)
	}
}

func (client plexClient) ensureLibraryOnce(library plexLibrary, language string) (string, error) {
	sections, err := client.sections()
	if err != nil {
		return "", err
	}
	for _, section := range sections {
		locationMatch := false
		for _, location := range section.Location {
			if location.Path == library.location {
				locationMatch = true
				break
			}
		}
		if locationMatch {
			if section.Type != library.typeName {
				return "", fmt.Errorf("Plex library at %s has type %s, expected %s", library.location, section.Type, library.typeName)
			}
			return "already present", nil
		}
		if section.Title == library.name {
			return "", fmt.Errorf("Plex library %q already exists with a different location", library.name)
		}
	}

	values := url.Values{
		"name":     {library.name},
		"type":     {library.typeName},
		"agent":    {library.agent},
		"scanner":  {library.scanner},
		"language": {language},
		"location": {library.location},
	}
	if err := client.request(http.MethodPost, "/library/sections?"+values.Encode(), nil); err != nil {
		return "", err
	}
	return "created", nil
}

func isPlexReadinessError(err error) bool {
	var responseError *plexHTTPError
	if !errors.As(err, &responseError) {
		return false
	}
	if responseError.statusCode == http.StatusUnauthorized || responseError.statusCode == http.StatusForbidden {
		return true
	}
	return responseError.statusCode == http.StatusBadRequest &&
		strings.Contains(strings.ToLower(responseError.response), "server is still starting up")
}

func (client plexClient) sections() ([]plexSection, error) {
	var result struct {
		MediaContainer struct {
			Directory []plexSection `json:"Directory"`
		} `json:"MediaContainer"`
	}
	if err := client.request(http.MethodGet, "/library/sections", &result); err != nil {
		return nil, err
	}
	return result.MediaContainer.Directory, nil
}

func (client plexClient) request(method, path string, result any) error {
	request, err := http.NewRequest(method, client.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("create Plex request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Plex-Token", client.token)
	request.Header.Set("X-Plex-Client-Identifier", "torrential-bootstrap")
	request.Header.Set("X-Plex-Product", "Torrential Bootstrap")
	request.Header.Set("X-Plex-Version", "1")

	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("Plex request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return &plexHTTPError{
			path:       path,
			statusCode: response.StatusCode,
			response:   strings.TrimSpace(string(data)),
		}
	}
	if result == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(result); err != nil {
		return fmt.Errorf("Plex %s returned invalid JSON: %w", path, err)
	}
	return nil
}
