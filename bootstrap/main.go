package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	configuration, err := loadConfig(os.LookupEnv)
	if err == nil {
		err = bootstrap(configuration)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "[bootstrap] failed: %v\n", err)
		os.Exit(1)
	}
}

func bootstrap(configuration config) error {
	if err := ensureDirectories(dataDirectories(configuration), configuration.uid, configuration.gid); err != nil {
		return err
	}
	logMessage("data directories ready")

	sonarrKey, err := readAPIKey(configuration.sonarr.configFile)
	if err != nil {
		return err
	}
	radarrKey, err := readAPIKey(configuration.radarr.configFile)
	if err != nil {
		return err
	}
	prowlarrKey, err := readAPIKey(configuration.prowlarr.configFile)
	if err != nil {
		return err
	}
	logMessage("discovered generated API keys")

	transmission := newTransmissionClient(configuration.transmission, configuration.requestTimeout)
	changed, err := transmission.configureDirectories(configuration.directories.complete, configuration.directories.incomplete)
	if err != nil {
		return err
	}
	if changed {
		logMessage("Transmission directories configured")
	} else {
		logMessage("Transmission directories already correct")
	}

	sonarr := newArrClient("Sonarr", configuration.sonarr, sonarrKey, configuration.requestTimeout)
	if err := configureArr(sonarr, configuration.transmission); err != nil {
		return err
	}
	radarr := newArrClient("Radarr", configuration.radarr, radarrKey, configuration.requestTimeout)
	if err := configureArr(radarr, configuration.transmission); err != nil {
		return err
	}

	prowlarr := newProwlarrClient(configuration.prowlarr, prowlarrKey, configuration.requestTimeout)
	state, err := prowlarr.ensureApplication("Sonarr", configuration.sonarr.baseURL, sonarrKey)
	if err != nil {
		return err
	}
	logMessage("Prowlarr Sonarr application " + state)
	state, err = prowlarr.ensureApplication("Radarr", configuration.radarr.baseURL, radarrKey)
	if err != nil {
		return err
	}
	logMessage("Prowlarr Radarr application " + state)
	state, err = prowlarr.ensureFlareSolverrProxy()
	if err != nil {
		return err
	}
	logMessage("Prowlarr FlareSolverr proxy " + state)
	if err := bootstrapPlexAndSeerr(
		configuration.plex,
		configuration.seerr,
		sonarr,
		radarr,
		sonarrKey,
		radarrKey,
	); err != nil {
		return err
	}
	logMessage("bootstrap completed successfully")
	return nil
}

func dataDirectories(configuration config) []string {
	return []string{
		configuration.directories.complete,
		filepath.Join(configuration.directories.complete, configuration.sonarr.category),
		filepath.Join(configuration.directories.complete, configuration.radarr.category),
		configuration.directories.incomplete,
		configuration.directories.tv,
		configuration.directories.movies,
	}
}

func configureArr(client *arrClient, transmission transmissionConfig) error {
	created, err := client.ensureRootFolder()
	if err != nil {
		return err
	}
	if created {
		logMessage(client.name + " root folder created")
	} else {
		logMessage(client.name + " root folder already present")
	}
	state, err := client.ensureTransmission(transmission)
	if err != nil {
		return err
	}
	logMessage(client.name + " Transmission client " + state)
	return nil
}

func ensureDirectories(directories []string, uid, gid int) error {
	owned := make(map[string]struct{})
	for _, directory := range directories {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", directory, err)
		}
		owned[directory] = struct{}{}
		owned[filepath.Dir(directory)] = struct{}{}
	}
	if os.Geteuid() != 0 {
		return nil
	}
	for directory := range owned {
		if err := os.Chown(directory, uid, gid); err != nil {
			return fmt.Errorf("set ownership on %s: %w", directory, err)
		}
	}
	return nil
}

func logMessage(message string) {
	fmt.Printf("[bootstrap] %s\n", message)
}
