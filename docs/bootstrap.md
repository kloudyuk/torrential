# Bootstrap

Bootstrap is the project-owned configuration utility. It is a standard-library Go
program contained in `bootstrap/`. Its multi-stage Docker build produces a static
binary in a minimal `scratch` image.

Compose runs bootstrap once after all long-lived services are healthy. It runs as
root so it can prepare data directories for the configured `PUID` and `PGID`, then
configures the bundled service connections and libraries. This includes the Sonarr
and Radarr category directories below Transmission's completed-download directory.

## Inputs

Compose supplies stable internal URLs, user/group IDs, quality-profile names, and
read-only mounts of Sonarr, Radarr, Prowlarr, Plex, and Seerr configuration.
Generated API keys and Plex's server token are read from those files and used only
in memory.

The only mutable mount is `/data`. Application configuration changes are made through
HTTP APIs; bootstrap never edits a service configuration file or database.

## Idempotency

Bootstrap identifies resources by their stack-owned meaning:

- exact media root path
- Transmission implementation and client name
- Prowlarr application name and implementation
- Prowlarr FlareSolverr proxy implementation and tag
- Plex library media type and root path
- Seerr service name and internal endpoint

Existing matches are updated when fixed connection fields differ. Missing resources
are created. Unrelated resources are never deleted. A failure produces a non-zero
exit code, and `scripts/stack-up.sh` propagates it.

## Plex authorization

When Plex is unclaimed, Seerr has no administrator, or Seerr's Plex connection is
incomplete, bootstrap creates a strong Plex PIN and prints the corresponding
`app.plex.tv` URL. It polls only while the operator approves that request. The
resulting Plex account token is used to obtain a single-use server claim token when
needed and to call Seerr's Plex sign-in endpoint. Bootstrap does not log or persist
either token itself; Plex and Seerr retain the credentials they normally store for
operation.

Claiming, Seerr administrator creation, library association, and Seerr initialization
therefore require one user authorization. Reruns detect the stored service state and
do not request another authorization. A retry after interrupted onboarding may ask
again to repair incomplete state. Indexer credentials and eligibility remain manual
because they are operator choices.

## Development

```sh
cd bootstrap
go test ./...
go build ./...
```

The standard-library unit tests cover configuration validation, API-key discovery,
provider schema handling, Transmission RPC negotiation, *Arr download-client
creation, Prowlarr application synchronization, Plex PIN authorization and token
discovery, Prowlarr FlareSolverr proxy configuration, Plex library reconciliation,
and Seerr service configuration. Compose
smoke tests should use temporary `CONFIG_ROOT` and `DATA_ROOT` values so they never
modify an operator's real state.
