package main

import (
	"encoding/xml"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type config struct {
	requestTimeout time.Duration
	startupTimeout time.Duration
	dashboardURL   string
	uid            int
	gid            int
	sonarr         arrConfig
	radarr         arrConfig
	prowlarr       serviceConfig
	transmission   transmissionConfig
	plex           plexConfig
	seerr          seerrConfig
	directories    directoryConfig
	locale         localeConfig
}

type localeConfig struct {
	tag      string
	language string
	region   string
}

type serviceConfig struct {
	baseURL    string
	configFile string
}

type arrConfig struct {
	serviceConfig
	rootFolder string
	category   string
}

type transmissionConfig struct {
	rpcURL string
}

type directoryConfig struct {
	complete   string
	incomplete string
	tv         string
	movies     string
	transcode  string
}

type environment func(string) (string, bool)

func loadConfig(env environment) (config, error) {
	tv := envValue(env, "BOOTSTRAP_TV_ROOT", "/data/media/tv")
	movies := envValue(env, "BOOTSTRAP_MOVIE_ROOT", "/data/media/movies")
	locale, err := parseLocale(envValue(env, "LOCALE", "en-GB"))
	if err != nil {
		return config{}, err
	}

	timeoutMS, err := envInteger(env, "BOOTSTRAP_REQUEST_TIMEOUT_MS", 15000, 1)
	if err != nil {
		return config{}, err
	}
	startupTimeoutMS, err := envInteger(env, "BOOTSTRAP_STARTUP_TIMEOUT_MS", 300000, 1)
	if err != nil {
		return config{}, err
	}
	dashboardURL, err := loadDashboardURL(env)
	if err != nil {
		return config{}, err
	}
	uid, err := envInteger(env, "PUID", 1000, 0)
	if err != nil {
		return config{}, err
	}
	gid, err := envInteger(env, "PGID", 1000, 0)
	if err != nil {
		return config{}, err
	}

	sonarrURL, err := envURL(env, "SONARR_URL", "http://sonarr:8989")
	if err != nil {
		return config{}, err
	}
	radarrURL, err := envURL(env, "RADARR_URL", "http://radarr:7878")
	if err != nil {
		return config{}, err
	}
	prowlarrURL, err := envURL(env, "PROWLARR_URL", "http://prowlarr:9696")
	if err != nil {
		return config{}, err
	}
	transmissionURL, err := envURL(env, "TRANSMISSION_RPC_URL", "http://transmission:9091/transmission/rpc")
	if err != nil {
		return config{}, err
	}
	requestTimeout := time.Duration(timeoutMS) * time.Millisecond
	plex, err := loadPlexConfig(env, requestTimeout, tv, movies, locale)
	if err != nil {
		return config{}, err
	}
	seerr, err := loadSeerrConfig(env, locale)
	if err != nil {
		return config{}, err
	}

	return config{
		requestTimeout: requestTimeout,
		startupTimeout: time.Duration(startupTimeoutMS) * time.Millisecond,
		dashboardURL:   dashboardURL,
		uid:            uid,
		gid:            gid,
		sonarr: arrConfig{
			serviceConfig: serviceConfig{sonarrURL, envValue(env, "SONARR_CONFIG_FILE", "/config/sonarr/config.xml")},
			rootFolder:    tv,
			category:      envValue(env, "SONARR_CATEGORY", "sonarr"),
		},
		radarr: arrConfig{
			serviceConfig: serviceConfig{radarrURL, envValue(env, "RADARR_CONFIG_FILE", "/config/radarr/config.xml")},
			rootFolder:    movies,
			category:      envValue(env, "RADARR_CATEGORY", "radarr"),
		},
		prowlarr: serviceConfig{prowlarrURL, envValue(env, "PROWLARR_CONFIG_FILE", "/config/prowlarr/config.xml")},
		transmission: transmissionConfig{
			rpcURL: transmissionURL,
		},
		plex:   plex,
		seerr:  seerr,
		locale: locale,
		directories: directoryConfig{
			complete:   envValue(env, "TRANSMISSION_COMPLETE_DIR", "/data/torrents/complete"),
			incomplete: envValue(env, "TRANSMISSION_INCOMPLETE_DIR", "/data/torrents/incomplete"),
			tv:         tv,
			movies:     movies,
			transcode:  "/data/transcode",
		},
	}, nil
}

func loadDashboardURL(env environment) (string, error) {
	host, ok := env("TORRENTIAL_HOST")
	host = strings.TrimSpace(host)
	if !ok || host == "" {
		return "", fmt.Errorf("TORRENTIAL_HOST is required")
	}
	parsed, err := url.Parse("http://" + host)
	if err != nil || parsed.Hostname() == "" || parsed.Port() != "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("TORRENTIAL_HOST must be an IP address or DNS name without a scheme or port")
	}
	port, err := envInteger(env, "DASHBOARD_PORT", 80, 1)
	if err != nil {
		return "", err
	}
	if port > 65535 {
		return "", fmt.Errorf("DASHBOARD_PORT must be less than or equal to 65535")
	}
	if port == 80 {
		return "http://" + host, nil
	}
	return fmt.Sprintf("http://%s:%d", host, port), nil
}

func parseLocale(value string) (localeConfig, error) {
	parts := strings.Split(strings.TrimSpace(value), "-")
	if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 {
		return localeConfig{}, fmt.Errorf("LOCALE must use language-region form such as en-GB")
	}
	for _, character := range parts[0] + parts[1] {
		if character < 'A' || character > 'Z' && character < 'a' || character > 'z' {
			return localeConfig{}, fmt.Errorf("LOCALE must use language-region form such as en-GB")
		}
	}
	language := strings.ToLower(parts[0])
	region := strings.ToUpper(parts[1])
	return localeConfig{tag: language + "-" + region, language: language, region: region}, nil
}

func envValue(env environment, name, fallback string) string {
	if value, ok := env(name); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func envInteger(env environment, name string, fallback, minimum int) (int, error) {
	value, ok := env(name)
	if !ok {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < minimum {
		return 0, fmt.Errorf("%s must be an integer greater than or equal to %d", name, minimum)
	}
	return parsed, nil
}

func envURL(env environment, name, fallback string) (string, error) {
	value := envValue(env, name, fallback)
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("%s must be an absolute URL", name)
	}
	return strings.TrimRight(value, "/"), nil
}

func readAPIKey(filename string) (string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", filename, err)
	}
	var document struct {
		APIKey string `xml:"ApiKey"`
	}
	if err := xml.Unmarshal(data, &document); err != nil {
		return "", fmt.Errorf("cannot parse %s: %w", filename, err)
	}
	key := strings.TrimSpace(document.APIKey)
	if key == "" {
		return "", fmt.Errorf("%s does not contain an API key", filename)
	}
	return key, nil
}
