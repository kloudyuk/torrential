package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	plexProduct         = "Torrential"
	plexVersion         = "1"
	plexPlatform        = "Docker"
	plexPlatformVersion = "1"
	plexDevice          = "Torrential"
	plexDeviceName      = "Torrential Bootstrap"
)

type plexTVClient struct {
	baseURL      string
	authorizeURL string
	clientID     string
	http         *http.Client
	authTimeout  time.Duration
	pollInterval time.Duration
}

type plexPIN struct {
	ID        int    `json:"id"`
	Code      string `json:"code"`
	AuthToken string `json:"authToken"`
}

func bootstrapPlexAndSeerr(
	plexConfiguration plexConfig,
	seerrConfiguration seerrConfig,
	sonarr *arrClient,
	radarr *arrClient,
	sonarrKey string,
	radarrKey string,
) error {
	seerr, err := newSeerrClient(seerrConfiguration, plexConfiguration.timeout)
	if err != nil {
		return err
	}
	public, err := seerr.publicSettings()
	if err != nil {
		return err
	}
	hasAdministrator, err := seerr.hasAdministrator()
	if err != nil {
		return err
	}
	seerrPlexConfigured := false
	if hasAdministrator {
		seerrPlexConfigured, err = seerr.isPlexConfigured()
		if err != nil {
			return err
		}
	}

	serverToken, tokenErr := readPlexToken(plexConfiguration.configFile)
	if tokenErr != nil && !isPlexUnclaimed(tokenErr) {
		return tokenErr
	}

	var accountToken string
	if serverToken == "" || !hasAdministrator || !seerrPlexConfigured {
		plexTV := newPlexTVClient(plexConfiguration, public.PlexClientIdentifier)
		accountToken, err = plexTV.authorize()
		if err != nil {
			return err
		}
		logMessage("Plex account authorized")
		if err := seerr.authenticatePlex(accountToken); err != nil {
			return err
		}
		if !hasAdministrator {
			logMessage("Seerr administrator created from Plex account")
		} else {
			logMessage("Seerr administrator Plex authorization refreshed")
		}
		if serverToken == "" {
			claimToken, err := plexTV.claimToken(accountToken)
			if err != nil {
				return err
			}
			if err := claimPlexServer(plexConfiguration, claimToken, public.PlexClientIdentifier); err != nil {
				return err
			}
			serverToken, err = waitForPlexToken(plexConfiguration)
			if err != nil {
				return err
			}
			logMessage("Plex server claimed")
		}
	}

	if err := configurePlexLibraries(plexConfiguration, serverToken); err != nil {
		return err
	}
	state, err := sonarr.ensurePlexNotification(plexConfiguration.notificationURL, serverToken)
	if err != nil {
		return err
	}
	logMessage("Sonarr Plex connection " + state)
	state, err = radarr.ensurePlexNotification(plexConfiguration.notificationURL, serverToken)
	if err != nil {
		return err
	}
	logMessage("Radarr Plex connection " + state)
	if err := seerr.configureLocale(); err != nil {
		return err
	}
	logMessage("Seerr locale configured")
	if err := seerr.configurePlex(); err != nil {
		return err
	}
	logMessage("Seerr Plex server and libraries configured")
	state, err = seerr.ensureArr("sonarr", sonarr, sonarrKey, seerrConfiguration.sonarrQualityProfile)
	if err != nil {
		return err
	}
	logMessage("Seerr Sonarr service " + state)
	state, err = seerr.ensureArr("radarr", radarr, radarrKey, seerrConfiguration.radarrQualityProfile)
	if err != nil {
		return err
	}
	logMessage("Seerr Radarr service " + state)
	if err := seerr.initialize(public.Initialized); err != nil {
		return err
	}
	if !public.Initialized {
		logMessage("Seerr initialization completed")
	}
	return nil
}

func newPlexTVClient(configuration plexConfig, clientID string) *plexTVClient {
	return &plexTVClient{
		baseURL:      configuration.plexTVURL,
		authorizeURL: configuration.authorizeURL,
		clientID:     clientID,
		http:         &http.Client{Timeout: configuration.timeout},
		authTimeout:  configuration.authTimeout,
		pollInterval: configuration.authPollInterval,
	}
}

func (client *plexTVClient) authorize() (string, error) {
	var pin plexPIN
	if err := client.request(http.MethodPost, "/api/v2/pins?strong=true", "", &pin); err != nil {
		return "", err
	}
	if pin.ID == 0 || strings.TrimSpace(pin.Code) == "" {
		return "", fmt.Errorf("Plex PIN response was incomplete")
	}
	values := url.Values{
		"clientID":                         {client.clientID},
		"code":                             {pin.Code},
		"context[device][product]":         {plexProduct},
		"context[device][version]":         {plexVersion},
		"context[device][platform]":        {plexPlatform},
		"context[device][platformVersion]": {plexPlatformVersion},
		"context[device][device]":          {plexDevice},
		"context[device][deviceName]":      {plexDeviceName},
	}
	fmt.Printf("\n[bootstrap] Authorize Torrential with Plex by opening:\n\n%s\n\n", client.authorizationURL(values))

	deadline := time.Now().Add(client.authTimeout)
	for time.Now().Before(deadline) {
		pollPath := fmt.Sprintf("/api/v2/pins/%d?code=%s", pin.ID, url.QueryEscape(pin.Code))
		if err := client.request(http.MethodGet, pollPath, "", &pin); err != nil {
			return "", err
		}
		if token := strings.TrimSpace(pin.AuthToken); token != "" {
			return token, nil
		}
		delay := client.pollInterval
		if delay <= 0 {
			delay = time.Second
		}
		if remaining := time.Until(deadline); delay > remaining {
			delay = remaining
		}
		time.Sleep(delay)
	}
	return "", fmt.Errorf("Plex authorization timed out after %s", client.authTimeout)
}

func (client *plexTVClient) authorizationURL(values url.Values) string {
	return strings.TrimRight(client.authorizeURL, "/#!?") + "/#!?" + values.Encode()
}

func (client *plexTVClient) claimToken(accountToken string) (string, error) {
	var response struct {
		Token string `json:"token"`
	}
	if err := client.request(http.MethodGet, "/api/claim/token.json", accountToken, &response); err != nil {
		return "", err
	}
	if strings.TrimSpace(response.Token) == "" {
		return "", fmt.Errorf("Plex did not return a server claim token")
	}
	return response.Token, nil
}

func (client *plexTVClient) request(method, path, accountToken string, result any) error {
	request, err := http.NewRequest(method, client.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("create Plex account request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Plex-Client-Identifier", client.clientID)
	request.Header.Set("X-Plex-Product", plexProduct)
	request.Header.Set("X-Plex-Version", plexVersion)
	request.Header.Set("X-Plex-Platform", plexPlatform)
	request.Header.Set("X-Plex-Platform-Version", plexPlatformVersion)
	request.Header.Set("X-Plex-Device", plexDevice)
	request.Header.Set("X-Plex-Device-Name", plexDeviceName)
	if accountToken != "" {
		request.Header.Set("X-Plex-Token", accountToken)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("Plex account request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("Plex account API returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(result); err != nil {
		return fmt.Errorf("Plex account API returned invalid JSON: %w", err)
	}
	return nil
}

func claimPlexServer(configuration plexConfig, claimToken, clientID string) error {
	values := url.Values{"token": {claimToken}}
	request, err := http.NewRequest(http.MethodPost, configuration.baseURL+"/myplex/claim?"+values.Encode(), nil)
	if err != nil {
		return fmt.Errorf("create Plex claim request: %w", err)
	}
	request.Header.Set("X-Plex-Client-Identifier", clientID)
	request.Header.Set("X-Plex-Product", plexProduct)
	request.Header.Set("X-Plex-Version", plexVersion)
	request.Header.Set("X-Plex-Platform", plexPlatform)
	request.Header.Set("X-Plex-Platform-Version", plexPlatformVersion)
	request.Header.Set("X-Plex-Device", plexDevice)
	request.Header.Set("X-Plex-Device-Name", plexDeviceName)
	request.Header.Set("X-Plex-Provides", "controller")
	language, _, _ := strings.Cut(configuration.language, "-")
	request.Header.Set("X-Plex-Language", language)
	claimTimeout := configuration.readinessTimeout
	if claimTimeout <= 0 {
		claimTimeout = configuration.timeout
	}
	response, err := (&http.Client{Timeout: claimTimeout}).Do(request)
	if err != nil {
		return fmt.Errorf("Plex server claim failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("Plex server claim returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}

func waitForPlexToken(configuration plexConfig) (string, error) {
	deadline := time.Now().Add(configuration.readinessTimeout)
	for time.Now().Before(deadline) {
		token, err := readPlexToken(configuration.configFile)
		if err == nil {
			return token, nil
		}
		if !isPlexUnclaimed(err) {
			return "", err
		}
		time.Sleep(configuration.readinessPollInterval)
	}
	return "", fmt.Errorf("Plex did not persist its server token within %s", configuration.readinessTimeout)
}

func isPlexUnclaimed(err error) bool {
	return errors.Is(err, errPlexUnclaimed)
}
