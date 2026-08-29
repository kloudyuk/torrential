package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigDefaults(t *testing.T) {
	configuration, err := loadConfig(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, configuration.sonarr.baseURL, "http://sonarr:8989")
	assertEqual(t, configuration.sonarr.rootFolder, "/data/media/tv")
	assertEqual(t, configuration.sonarr.category, "sonarr")
	assertEqual(t, configuration.radarr.baseURL, "http://radarr:7878")
	assertEqual(t, configuration.radarr.rootFolder, "/data/media/movies")
	assertEqual(t, configuration.prowlarr.baseURL, "http://prowlarr:9696")
	assertEqual(t, configuration.transmission.rpcURL, "http://transmission:9091/transmission/rpc")
	assertEqual(t, configuration.seerr.baseURL, "http://seerr:5055")
	assertEqual(t, configuration.seerr.configFile, "/service-config/seerr/settings.json")
	assertEqual(t, configuration.seerr.sonarrQualityProfile, "HD-1080p")
	assertEqual(t, configuration.seerr.radarrQualityProfile, "HD-1080p")
}

func TestLoadConfigRejectsInvalidValues(t *testing.T) {
	values := map[string]string{"PUID": "not-a-number"}
	_, err := loadConfig(mapEnvironment(values))
	if err == nil || !strings.Contains(err.Error(), "PUID must be an integer") {
		t.Fatalf("expected PUID validation error, got %v", err)
	}
}

func TestReadAPIKey(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "config.xml")
	if err := os.WriteFile(filename, []byte("<Config><ApiKey>generated-secret</ApiKey></Config>"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := readAPIKey(filename)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, key, "generated-secret")
}

func TestEnsureDirectoriesCreatesDownloadCategories(t *testing.T) {
	root := t.TempDir()
	configuration := config{
		sonarr: arrConfig{category: "sonarr"},
		radarr: arrConfig{category: "radarr"},
		directories: directoryConfig{
			complete:   filepath.Join(root, "torrents", "complete"),
			incomplete: filepath.Join(root, "torrents", "incomplete"),
			tv:         filepath.Join(root, "media", "tv"),
			movies:     filepath.Join(root, "media", "movies"),
		},
	}
	directories := dataDirectories(configuration)

	if err := ensureDirectories(directories, os.Getuid(), os.Getgid()); err != nil {
		t.Fatal(err)
	}
	for _, directory := range directories {
		info, err := os.Stat(directory)
		if err != nil {
			t.Fatalf("expected %s to exist: %v", directory, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected %s to be a directory", directory)
		}
	}
}

func TestLoadPlexConfigDefaults(t *testing.T) {
	configuration, err := loadPlexConfig(
		func(string) (string, bool) { return "", false },
		15*time.Second,
		"/data/media/tv",
		"/data/media/movies",
	)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, configuration.baseURL, "http://plex:32400")
	assertEqual(t, configuration.plexTVURL, "https://plex.tv")
	assertEqual(t, configuration.authorizeURL, "https://app.plex.tv/auth")
	assertEqual(t, configuration.language, "en-US")
	assertEqual(t, configuration.libraries[0], plexLibrary{
		name: "TV Shows", typeName: "show", agent: "tv.plex.agents.series",
		scanner: "Plex TV Series", location: "/data/media/tv",
	})
	assertEqual(t, configuration.libraries[1], plexLibrary{
		name: "Movies", typeName: "movie", agent: "tv.plex.agents.movie",
		scanner: "Plex Movie", location: "/data/media/movies",
	})
}

func TestReadSeerrAPIKey(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(filename, []byte(`{"main":{"apiKey":"seerr-secret"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := readSeerrAPIKey(filename)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, key, "seerr-secret")
}

func TestSeerrPlexConfigurationState(t *testing.T) {
	machineID := ""
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assertEqual(t, request.Method, http.MethodGet)
		assertEqual(t, request.URL.Path, "/api/v1/settings/plex")
		assertEqual(t, request.Header.Get("X-Api-Key"), "seerr-secret")
		writeJSON(t, writer, map[string]string{"machineId": machineID})
	}))
	defer server.Close()

	client := &seerrClient{api: newAPIClient(server.URL, "seerr-secret", time.Second)}
	configured, err := client.isPlexConfigured()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, configured, false)

	machineID = "plex-machine"
	configured, err = client.isPlexConfigured()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, configured, true)
}

func TestPlexPINAuthorizationAndClaimToken(t *testing.T) {
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assertEqual(t, request.Header.Get("X-Plex-Client-Identifier"), "seerr-client")
		assertEqual(t, request.Header.Get("X-Plex-Product"), plexProduct)
		assertEqual(t, request.Header.Get("X-Plex-Version"), plexVersion)
		assertEqual(t, request.Header.Get("X-Plex-Platform"), plexPlatform)
		assertEqual(t, request.Header.Get("X-Plex-Platform-Version"), plexPlatformVersion)
		assertEqual(t, request.Header.Get("X-Plex-Device"), plexDevice)
		assertEqual(t, request.Header.Get("X-Plex-Device-Name"), plexDeviceName)
		switch request.Method + " " + request.URL.Path {
		case "POST /api/v2/pins":
			assertEqual(t, request.URL.Query().Get("strong"), "true")
			writeJSON(t, writer, map[string]any{"id": 42, "code": "ABCD"})
		case "GET /api/v2/pins/42":
			polls++
			assertEqual(t, request.URL.Query().Get("code"), "ABCD")
			token := ""
			if polls == 2 {
				token = "account-token"
			}
			writeJSON(t, writer, map[string]any{"id": 42, "code": "ABCD", "authToken": token})
		case "GET /api/claim/token.json":
			assertEqual(t, request.Header.Get("X-Plex-Token"), "account-token")
			writeJSON(t, writer, map[string]any{"token": "claim-token"})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := &plexTVClient{
		baseURL: server.URL, authorizeURL: "https://app.plex.tv/auth", clientID: "seerr-client",
		http: &http.Client{Timeout: time.Second}, authTimeout: time.Second, pollInterval: time.Millisecond,
	}
	authorizationURL := client.authorizationURL(url.Values{
		"clientID":                         {client.clientID},
		"code":                             {"ABCD"},
		"context[device][product]":         {plexProduct},
		"context[device][version]":         {plexVersion},
		"context[device][platform]":        {plexPlatform},
		"context[device][platformVersion]": {plexPlatformVersion},
		"context[device][device]":          {plexDevice},
		"context[device][deviceName]":      {plexDeviceName},
	})
	if !strings.HasPrefix(authorizationURL, "https://app.plex.tv/auth/#!?") {
		t.Fatalf("unexpected Plex authorization URL: %s", authorizationURL)
	}
	fragment := strings.TrimPrefix(strings.SplitN(authorizationURL, "#", 2)[1], "!?")
	parameters, err := url.ParseQuery(fragment)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, parameters.Get("context[device][product]"), plexProduct)
	assertEqual(t, parameters.Get("context[device][platform]"), plexPlatform)
	assertEqual(t, parameters.Get("context[device][deviceName]"), plexDeviceName)
	accountToken, err := client.authorize()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, accountToken, "account-token")
	claimToken, err := client.claimToken(accountToken)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, claimToken, "claim-token")
	assertEqual(t, polls, 2)
}

func TestPlexServerClaimUsesAuthorizedClientIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assertEqual(t, request.Method, http.MethodPost)
		assertEqual(t, request.URL.Path, "/myplex/claim")
		assertEqual(t, request.URL.Query().Get("token"), "claim-token")
		assertEqual(t, request.Header.Get("X-Plex-Client-Identifier"), "seerr-client")
		assertEqual(t, request.Header.Get("X-Plex-Product"), plexProduct)
		assertEqual(t, request.Header.Get("X-Plex-Platform"), plexPlatform)
		time.Sleep(20 * time.Millisecond)
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := claimPlexServer(plexConfig{
		baseURL: server.URL, timeout: time.Millisecond, readinessTimeout: time.Second,
	}, "claim-token", "seerr-client")
	if err != nil {
		t.Fatal(err)
	}
}

func TestSeerrCreatesDefaultSonarrService(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method + " " + request.URL.Path {
		case "GET /api/v3/qualityprofile":
			writeJSON(t, writer, []any{map[string]any{"id": 7, "name": "HD-1080p"}})
		case "GET /api/v1/settings/sonarr":
			writeJSON(t, writer, []any{})
		case "POST /api/v1/settings/sonarr":
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			writeJSON(t, writer, map[string]any{"id": 0})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	arr := newArrClient("Sonarr", arrConfig{
		serviceConfig: serviceConfig{baseURL: server.URL},
		rootFolder:    "/data/media/tv",
	}, "sonarr-key", time.Second)
	seerr := &seerrClient{
		config: seerrConfig{baseURL: server.URL},
		api:    newAPIClient(server.URL, "seerr-key", time.Second),
		http:   &http.Client{Timeout: time.Second},
	}
	state, err := seerr.ensureArr("sonarr", arr, "sonarr-key", "HD-1080p")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, state, "created")
	assertEqual(t, payload["hostname"], "sonarr")
	assertEqual(t, payload["activeProfileId"], float64(7))
	assertEqual(t, payload["activeDirectory"], "/data/media/tv")
	assertEqual(t, payload["isDefault"], true)
	assertEqual(t, payload["enableSeasonFolders"], true)
	assertEqual(t, payload["monitorNewItems"], "all")
}

func TestReadPlexToken(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "Preferences.xml")
	if err := os.WriteFile(filename, []byte(`<Preferences PlexOnlineToken="server-token"/>`), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := readPlexToken(filename)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, token, "server-token")

	if err := os.WriteFile(filename, []byte(`<Preferences/>`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPlexToken(filename); !errors.Is(err, errPlexUnclaimed) {
		t.Fatalf("expected unclaimed error, got %v", err)
	}
}

func TestPlexCreatesMissingLibrary(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		assertEqual(t, request.Header.Get("X-Plex-Token"), "server-token")
		switch request.Method + " " + request.URL.Path {
		case "GET /library/sections":
			writeJSON(t, writer, map[string]any{"MediaContainer": map[string]any{"Directory": []any{}}})
		case "POST /library/sections":
			query := request.URL.Query()
			assertEqual(t, query.Get("name"), "TV Shows")
			assertEqual(t, query.Get("type"), "show")
			assertEqual(t, query.Get("agent"), "tv.plex.agents.series")
			assertEqual(t, query.Get("scanner"), "Plex TV Series")
			assertEqual(t, query.Get("language"), "en-US")
			assertEqual(t, query.Get("location"), "/data/media/tv")
			writer.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := newPlexClient(server.URL, "server-token", time.Second)
	state, err := client.ensureLibrary(plexLibrary{
		name: "TV Shows", typeName: "show", agent: "tv.plex.agents.series",
		scanner: "Plex TV Series", location: "/data/media/tv",
	}, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, state, "created")
	assertEqual(t, requests, 2)
}

func TestPlexReusesLibraryByLocation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(t, writer, map[string]any{"MediaContainer": map[string]any{
			"Directory": []any{map[string]any{
				"title": "Television", "type": "show",
				"Location": []any{map[string]any{"path": "/data/media/tv"}},
			}},
		}})
	}))
	defer server.Close()

	client := newPlexClient(server.URL, "server-token", time.Second)
	state, err := client.ensureLibrary(plexLibrary{name: "TV Shows", typeName: "show", location: "/data/media/tv"}, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, state, "already present")
}

func TestPlexRetriesAuthorizationDuringFirstStart(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method + " " + request.URL.Path {
		case "GET /library/sections":
			writeJSON(t, writer, map[string]any{"MediaContainer": map[string]any{"Directory": []any{}}})
		case "POST /library/sections":
			posts++
			if posts == 1 {
				http.Error(writer, "claim authorization pending", http.StatusForbidden)
				return
			}
			writer.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := newPlexClient(server.URL, "server-token", time.Second)
	client.readinessTimeout = 100 * time.Millisecond
	client.retryInterval = time.Millisecond
	state, err := client.ensureLibrary(plexLibrary{
		name: "Movies", typeName: "movie", agent: "tv.plex.agents.movie",
		scanner: "Plex Movie", location: "/data/media/movies",
	}, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, state, "created")
	assertEqual(t, posts, 2)
}

func TestPlexRetriesWhileLibrarySubsystemStarts(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method + " " + request.URL.Path {
		case "GET /library/sections":
			writeJSON(t, writer, map[string]any{"MediaContainer": map[string]any{"Directory": []any{}}})
		case "POST /library/sections":
			posts++
			if posts == 1 {
				http.Error(writer, "the server is still starting up. Please retry later", http.StatusBadRequest)
				return
			}
			writer.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := newPlexClient(server.URL, "server-token", time.Second)
	client.readinessTimeout = 100 * time.Millisecond
	client.retryInterval = time.Millisecond
	state, err := client.ensureLibrary(plexLibrary{
		name: "TV Shows", typeName: "show", agent: "tv.plex.agents.series",
		scanner: "Plex TV Series", location: "/data/media/tv",
	}, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, state, "created")
	assertEqual(t, posts, 2)
}

func TestUpdateFields(t *testing.T) {
	fields := []any{
		map[string]any{"name": "host", "value": "localhost"},
		map[string]any{"name": "port", "value": float64(9091)},
	}
	updated, err := updateFields(fields, map[string]any{"host": "transmission"})
	if err != nil {
		t.Fatal(err)
	}
	host := updated[0].(map[string]any)
	assertEqual(t, host["value"], "transmission")
	if _, err := updateFields(fields, map[string]any{"missing": true}); err == nil ||
		!strings.Contains(err.Error(), "provider schema is missing fields: missing") {
		t.Fatalf("expected missing-field error, got %v", err)
	}
}

func TestTransmissionConfiguresDirectories(t *testing.T) {
	requestNumber := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestNumber++
		if _, _, ok := request.BasicAuth(); ok {
			t.Errorf("Transmission request unexpectedly used basic authentication")
		}
		var payload struct {
			Method    string         `json:"method"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Error(err)
		}

		switch requestNumber {
		case 1:
			writer.Header().Set("X-Transmission-Session-Id", "rpc-session")
			writer.WriteHeader(http.StatusConflict)
		case 2:
			assertEqual(t, request.Header.Get("X-Transmission-Session-Id"), "rpc-session")
			assertEqual(t, payload.Method, "session-get")
			writeJSON(t, writer, map[string]any{
				"result": "success",
				"arguments": map[string]any{
					"download-dir":           "/downloads/complete",
					"incomplete-dir":         "/downloads/incomplete",
					"incomplete-dir-enabled": false,
				},
			})
		case 3:
			assertEqual(t, payload.Method, "session-set")
			assertEqual(t, payload.Arguments["download-dir"], "/data/torrents/complete")
			assertEqual(t, payload.Arguments["incomplete-dir"], "/data/torrents/incomplete")
			assertEqual(t, payload.Arguments["incomplete-dir-enabled"], true)
			writeJSON(t, writer, map[string]any{"result": "success", "arguments": map[string]any{}})
		default:
			t.Errorf("unexpected request %d", requestNumber)
		}
	}))
	defer server.Close()

	client := newTransmissionClient(transmissionConfig{rpcURL: server.URL}, time.Second)
	changed, err := client.configureDirectories("/data/torrents/complete", "/data/torrents/incomplete")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected directory configuration to change")
	}
}

func TestArrCreatesRootAndTransmissionClient(t *testing.T) {
	var downloadClientPayload providerResource
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assertEqual(t, request.Header.Get("X-Api-Key"), "api-key")
		switch request.Method + " " + request.URL.Path {
		case "GET /api/v3/rootfolder":
			writeJSON(t, writer, []any{})
		case "POST /api/v3/rootfolder":
			writeJSON(t, writer, map[string]any{"id": 1, "path": "/data/media/tv"})
		case "GET /api/v3/downloadclient":
			writeJSON(t, writer, []any{})
		case "GET /api/v3/downloadclient/schema":
			writeJSON(t, writer, []any{transmissionSchema("tvCategory")})
		case "POST /api/v3/downloadclient":
			if err := json.NewDecoder(request.Body).Decode(&downloadClientPayload); err != nil {
				t.Error(err)
			}
			writeJSON(t, writer, map[string]any{"id": 2})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := newArrClient("Sonarr", arrConfig{
		serviceConfig: serviceConfig{baseURL: server.URL, configFile: "/config.xml"},
		rootFolder:    "/data/media/tv",
		category:      "sonarr",
	}, "api-key", time.Second)
	created, err := client.ensureRootFolder()
	if err != nil || !created {
		t.Fatalf("ensure root folder: created=%v err=%v", created, err)
	}
	state, err := client.ensureTransmission(transmissionConfig{})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, state, "created")
	assertEqual(t, downloadClientPayload["name"], "Transmission")
	fields := fieldsByName(t, downloadClientPayload["fields"])
	assertEqual(t, fields["host"], "transmission")
	assertEqual(t, fields["urlBase"], "/transmission/")
	assertEqual(t, fields["username"], "")
	assertEqual(t, fields["password"], "")
	assertEqual(t, fields["tvCategory"], "sonarr")
}

func TestProwlarrCreatesFullSyncApplication(t *testing.T) {
	var applicationPayload providerResource
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method + " " + request.URL.Path {
		case "GET /api/v1/applications":
			writeJSON(t, writer, []any{})
		case "GET /api/v1/applications/schema":
			writeJSON(t, writer, []any{map[string]any{
				"implementation": "Sonarr",
				"fields": []any{
					map[string]any{"name": "prowlarrUrl", "value": "http://localhost:9696"},
					map[string]any{"name": "baseUrl", "value": "http://localhost:8989"},
					map[string]any{"name": "apiKey", "value": nil},
				},
			}})
		case "POST /api/v1/applications":
			if err := json.NewDecoder(request.Body).Decode(&applicationPayload); err != nil {
				t.Error(err)
			}
			writeJSON(t, writer, map[string]any{"id": 1})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := newProwlarrClient(serviceConfig{baseURL: server.URL}, "prowlarr-key", time.Second)
	state, err := client.ensureApplication("Sonarr", "http://sonarr:8989", "sonarr-key")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, state, "created")
	assertEqual(t, applicationPayload["syncLevel"], "fullSync")
	fields := fieldsByName(t, applicationPayload["fields"])
	assertEqual(t, fields["prowlarrUrl"], server.URL)
	assertEqual(t, fields["baseUrl"], "http://sonarr:8989")
	assertEqual(t, fields["apiKey"], "sonarr-key")
}

func TestProwlarrCreatesFlareSolverrProxyAndTag(t *testing.T) {
	var proxyPayload providerResource
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method + " " + request.URL.Path {
		case "GET /api/v1/tag":
			writeJSON(t, writer, []any{})
		case "POST /api/v1/tag":
			writeJSON(t, writer, map[string]any{"id": 7, "label": "flaresolverr"})
		case "GET /api/v1/indexerProxy":
			writeJSON(t, writer, []any{})
		case "GET /api/v1/indexerProxy/schema":
			writeJSON(t, writer, []any{flareSolverrSchema()})
		case "POST /api/v1/indexerProxy":
			if err := json.NewDecoder(request.Body).Decode(&proxyPayload); err != nil {
				t.Error(err)
			}
			writeJSON(t, writer, map[string]any{"id": 1})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := newProwlarrClient(serviceConfig{baseURL: server.URL}, "prowlarr-key", time.Second)
	state, err := client.ensureFlareSolverrProxy()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, state, "created")
	assertEqual(t, proxyPayload["name"], "FlareSolverr")
	assertEqual(t, proxyPayload["tags"], []any{float64(7)})
	fields := fieldsByName(t, proxyPayload["fields"])
	assertEqual(t, fields["host"], "http://127.0.0.1:8191")
	assertEqual(t, fields["requestTimeout"], float64(60))
}

func flareSolverrSchema() map[string]any {
	return map[string]any{
		"implementation": "FlareSolverr",
		"fields": []any{
			map[string]any{"name": "host", "value": "http://localhost:8191"},
			map[string]any{"name": "requestTimeout", "value": 60},
		},
		"tags": []any{},
	}
}

func transmissionSchema(categoryField string) map[string]any {
	return map[string]any{
		"implementation": "Transmission",
		"fields": []any{
			map[string]any{"name": "host", "value": "localhost"},
			map[string]any{"name": "port", "value": 9091},
			map[string]any{"name": "useSsl", "value": false},
			map[string]any{"name": "urlBase", "value": "/transmission/"},
			map[string]any{"name": "username", "value": nil},
			map[string]any{"name": "password", "value": nil},
			map[string]any{"name": categoryField, "value": nil},
		},
	}
}

func fieldsByName(t *testing.T, value any) map[string]any {
	t.Helper()
	fields, ok := value.([]any)
	if !ok {
		t.Fatalf("expected fields array, got %T", value)
	}
	result := make(map[string]any, len(fields))
	for _, value := range fields {
		field := value.(map[string]any)
		result[field["name"].(string)] = field["value"]
	}
	return result
}

func mapEnvironment(values map[string]string) environment {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

func writeJSON(t *testing.T, writer http.ResponseWriter, value any) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		t.Error(err)
	}
}

func assertEqual(t *testing.T, actual, expected any) {
	t.Helper()
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("got %v, expected %v", actual, expected)
	}
}
