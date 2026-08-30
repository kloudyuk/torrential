package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	component := "bootstrap"
	var err error
	switch {
	case len(os.Args) == 1:
		var configuration config
		configuration, err = loadConfig(os.LookupEnv)
		if err == nil {
			err = bootstrap(configuration)
		}
	case len(os.Args) == 2 && os.Args[1] == "prepare":
		var configuration config
		configuration, err = loadConfig(os.LookupEnv)
		if err == nil {
			err = prepareDirectories(configuration, "/config")
		}
		if err == nil {
			logMessage("host directories ready")
		}
	case len(os.Args) == 2 && os.Args[1] == "controller":
		component = "controller"
		if err = controllerSocketExists(os.LookupEnv); err == nil {
			err = runController(os.LookupEnv)
		}
	case len(os.Args) == 2 && os.Args[1] == "probe":
		component = "controller"
		err = probeController()
	default:
		err = fmt.Errorf("usage: bootstrap [prepare|controller|probe]")
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] failed: %v\n", component, err)
		os.Exit(1)
	}
}

func prepareDirectories(configuration config, configRoot string) error {
	directories := []string{
		configRoot,
		filepath.Join(configRoot, "gluetun"),
		filepath.Join(configRoot, "sonarr"),
		filepath.Join(configRoot, "radarr"),
		filepath.Join(configRoot, "seerr"),
		filepath.Join(configRoot, "seerr", "logs"),
		filepath.Join(configRoot, "plex"),
		filepath.Join(configRoot, "prowlarr"),
		filepath.Join(configRoot, "transmission"),
	}
	directories = append(directories, dataDirectories(configuration)...)
	return ensureDirectories(directories, configuration.uid, configuration.gid)
}

func bootstrap(configuration config) error {
	if err := waitForPrerequisites(configuration); err != nil {
		return err
	}
	logMessage("services ready")

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
	state, err = prowlarr.ensureByparrProxy()
	if err != nil {
		return err
	}
	logMessage("Prowlarr Byparr proxy " + state)
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
	printCompletionBanner(configuration.dashboardURL)
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
		configuration.directories.transcode,
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
		if parent := filepath.Dir(directory); parent != string(filepath.Separator) {
			owned[parent] = struct{}{}
		}
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

func printCompletionBanner(dashboardURL string) {
	fmt.Printf(`[bootstrap] ############################################################
[bootstrap]
[bootstrap]                     TORRENTIAL IS READY
[bootstrap]
[bootstrap] Dashboard: %s
[bootstrap]
[bootstrap] ############################################################
`, dashboardURL)
}
