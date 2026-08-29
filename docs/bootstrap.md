# Bootstrap

Bootstrap is the project-owned configuration utility. It is a standard-library Go
program contained in `bootstrap/`. Its multi-stage Docker build produces a static
binary in a minimal `scratch` image. Normal deployments pull a versioned ARM64/AMD64
image from GitHub Container Registry rather than compiling it locally.

Compose first runs the bootstrap image as a one-shot `init` service in `prepare`
mode. It creates every bind-mounted configuration and data directory with the
configured `PUID` and `PGID`. Plex and the configuration coordinator do not start
until initialization succeeds. This avoids Docker-created native-Linux bind-mount
directories retaining unusable ownership.

The same image then runs once as the `bootstrap` coordinator. It shares Plex's
network namespace so the unclaimed-server authorization is local, waits up to five
minutes for generated configuration files and service APIs, configures the bundled
connections and libraries, and exits. The prepared paths include the Sonarr and
Radarr completed-download categories.

## Inputs

Compose supplies stable internal URLs, user/group IDs, locale, quality-profile names,
the operator-facing host and ports, and the shared configuration and data mounts.
Generated API keys and Plex's server token are read from those files and used only in
memory.

The `init` service changes only directory creation, mode, and ownership. The
coordinator reads generated configuration but makes application changes through HTTP
APIs; it never edits a service configuration file or database.

## Idempotency

Bootstrap identifies resources by their stack-owned meaning:

- exact media root path
- Transmission implementation and client name
- Prowlarr application name and implementation
- Prowlarr FlareSolverr proxy implementation and tag
- Plex library media type and root path
- Plex notification implementation in Sonarr and Radarr
- Seerr service name and internal endpoint

Existing matches are updated when fixed connection fields differ. Missing resources
are created. Unrelated resources are never deleted. A failure produces a non-zero
exit code visible directly in Compose output and `docker compose ps --all`.
Successful completion prints a prominent banner containing the dashboard URL.

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

To test the locally built image through Compose:

```sh
docker compose -f compose.yaml -f compose.dev.yaml build
docker compose -f compose.yaml -f compose.dev.yaml up -d
docker compose -f compose.yaml -f compose.dev.yaml logs --follow bootstrap
```

Release tags in `vMAJOR.MINOR.PATCH` form run
`.github/workflows/publish-bootstrap.yaml`, which publishes multi-platform
`linux/amd64` and `linux/arm64` images. Each build receives an immutable full-version
tag such as `v0.1.0` and updates its patch-floating minor tag such as `v0.1`.
`compose.yaml` references the minor tag, and the workflow verifies that relationship
before publishing. Full-version release tags must not be reused; the workflow refuses
to replace one that already exists.

After publishing the package for the first time, set its GHCR visibility to public
in the GitHub package settings. Public GHCR images can then be pulled anonymously by
Torrential deployments.

The standard-library unit tests cover configuration validation, API-key discovery,
provider schema handling, Transmission RPC negotiation, *Arr download-client
and Plex-notification creation, Prowlarr application synchronization, Plex PIN authorization and token
discovery, Prowlarr FlareSolverr proxy configuration, Plex library reconciliation,
and Seerr service and locale configuration. Compose smoke tests should use temporary
`CONFIG_ROOT` and `DATA_ROOT` values so they never modify an operator's real state.
