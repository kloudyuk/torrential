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
	uid            int
	gid            int
	sonarr         arrConfig
	radarr         arrConfig
	prowlarr       serviceConfig
	transmission   transmissionConfig
	plex           plexConfig
	seerr          seerrConfig
	directories    directoryConfig
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
}

type environment func(string) (string, bool)

func loadConfig(env environment) (config, error) {
	tv := envValue(env, "BOOTSTRAP_TV_ROOT", "/data/media/tv")
	movies := envValue(env, "BOOTSTRAP_MOVIE_ROOT", "/data/media/movies")

	timeoutMS, err := envInteger(env, "BOOTSTRAP_REQUEST_TIMEOUT_MS", 15000, 1)
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
	plex, err := loadPlexConfig(env, requestTimeout, tv, movies)
	if err != nil {
		return config{}, err
	}
	seerr, err := loadSeerrConfig(env)
	if err != nil {
		return config{}, err
	}

	return config{
		requestTimeout: requestTimeout,
		uid:            uid,
		gid:            gid,
		sonarr: arrConfig{
			serviceConfig: serviceConfig{sonarrURL, envValue(env, "SONARR_CONFIG_FILE", "/arr-config/sonarr/config.xml")},
			rootFolder:    tv,
			category:      envValue(env, "SONARR_CATEGORY", "sonarr"),
		},
		radarr: arrConfig{
			serviceConfig: serviceConfig{radarrURL, envValue(env, "RADARR_CONFIG_FILE", "/arr-config/radarr/config.xml")},
			rootFolder:    movies,
			category:      envValue(env, "RADARR_CATEGORY", "radarr"),
		},
		prowlarr: serviceConfig{prowlarrURL, envValue(env, "PROWLARR_CONFIG_FILE", "/arr-config/prowlarr/config.xml")},
		transmission: transmissionConfig{
			rpcURL: transmissionURL,
		},
		plex:  plex,
		seerr: seerr,
		directories: directoryConfig{
			complete:   envValue(env, "TRANSMISSION_COMPLETE_DIR", "/data/torrents/complete"),
			incomplete: envValue(env, "TRANSMISSION_INCOMPLETE_DIR", "/data/torrents/incomplete"),
			tv:         tv,
			movies:     movies,
		},
	}, nil
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
