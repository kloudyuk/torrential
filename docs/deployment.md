# Deployment and setup

Torrential supports its project-owned Docker Compose deployment on ARM64 and AMD64
with Docker Compose 2.20.0 or newer.

## Configuration

Copy the example environment file:

```sh
cp .env.example .env
```

Before the first start:

1. Select `VPN_SERVICE_PROVIDER` and set its `OPENVPN_USER` and `OPENVPN_PASSWORD`.
2. Set `TORRENTIAL_HOST` to the Docker host's stable LAN IP address or local DNS
   name, without `http://` or a port. Review `PUID`, `PGID`, `TZ`, `LOCALE`, storage
   paths, the shared web-interface bind address, and published ports.
3. If required, change the Sonarr and Radarr quality-profile names selected for
   Seerr. Both default to `HD-1080p`.

`TZ` is an IANA time-zone identifier and controls clocks. It cannot reliably imply a
language or content market. `LOCALE` therefore uses explicit `language-REGION` form,
such as `en-GB`, and controls Seerr's display/discovery/streaming defaults plus Plex
library language and certification country. The region must be one Plex supports.

Torrential uses `TORRENTIAL_HOST` and `PLEX_PORT` to construct Plex's advertised
server URL. The same host and `DASHBOARD_PORT` form the dashboard URL printed after
bootstrap. Use a DHCP reservation or stable local DNS name so these addresses do not
change unexpectedly. This does not enable public remote access.

`.env` is read automatically by Compose; it is not a shell script and does not need
`export` statements or manual sourcing.

There is one supported topology. `compose.yaml` requires Gluetun and routes
Transmission, Prowlarr, and FlareSolverr through it. Missing VPN credentials cause
Compose configuration to fail before any containers are created. The example selects
NordVPN, but `VPN_SERVICE_PROVIDER`, `OPENVPN_USER`, and `OPENVPN_PASSWORD` use
Gluetun's generic OpenVPN provider interface. Follow the selected provider's Gluetun
setup page for credential and server-filter requirements.

Inspect the effective configuration after changing networking or published ports:

```sh
docker compose config
```

See [Advanced operator setup](advanced-setup.md) for the routing boundary, public-IP
verification, and failure testing.

## Start

```sh
docker compose up -d
docker compose logs --follow bootstrap
```

Compose downloads missing service images, including Torrential's prebuilt bootstrap
image. A one-shot `init` service creates `CONFIG_ROOT`, `DATA_ROOT`, and their
required subdirectories with the configured `PUID` and `PGID`. Plex and the
configuration coordinator start only after it succeeds. The coordinator then waits
for the other service files and APIs, performs the idempotent configuration, and
exits. The services run detached; the second command follows only the coordinator
and returns when it exits. Initialization or coordinator failures are visible with
`docker compose logs init bootstrap` and `docker compose ps --all`.
Successful bootstrap ends with a clearly marked completion banner containing the
dashboard URL built from `TORRENTIAL_HOST` and `DASHBOARD_PORT`.

Open the dashboard URL from bootstrap's completion banner. Its six service links use
whichever hostname or IP address opened the dashboard and the corresponding configured
ports. The stack binds these interfaces to the LAN by default; set
`WEB_BIND_ADDRESS=127.0.0.1` for host-only access. Change `DASHBOARD_PORT` if host port
80 is already in use. Do not expose any stack ports directly to the public internet.
The bundled dashboard serves plain HTTP; Torrential does not manage local TLS
certificates or HTTPS termination.

On a fresh deployment, bootstrap prints one `app.plex.tv` authorization URL in the
followed coordinator log. Open it, sign in to the Plex account that will own the
server, and approve Torrential. The coordinator continues automatically; no
credential needs to be copied. The
authorization request expires after 15 minutes. Run `docker compose up -d` again to
ensure the stack is running, then retry the coordinator directly with
`docker compose run --rm --no-deps bootstrap`. Plex's page can show a generic
completion error after successful approval; the bootstrap output is authoritative.
Existing completed deployments skip this step.

Plex startup and bootstrap perform these convergent operations:

- creates `/data/torrents/incomplete`, `/data/torrents/complete/{sonarr,radarr}`, and
  `/data/media/{tv,movies}`, plus `/data/transcode`;
- configures Transmission's complete and incomplete directories;
- adds `/data/media/tv` to Sonarr and `/data/media/movies` to Radarr;
- registers Transmission in Sonarr and Radarr with separate categories;
- registers Sonarr and Radarr as full-sync Prowlarr applications;
- creates a tagged FlareSolverr indexer proxy in Prowlarr;
- marks Plex's first-run server setup complete before Plex starts;
- uses one Plex authorization to claim a new Plex server and create Seerr's initial
  administrator;
- creates Plex TV Shows and Movies libraries for the corresponding media roots;
- applies `LOCALE` to Seerr and to Plex library language and certification country;
- configures Sonarr and Radarr Plex connections to request library scans after
  imports and upgrades;
- connects Seerr to Plex, enables both libraries, and adds Sonarr and Radarr as its
  default services using the configured quality profiles.

Rerunning bootstrap updates fixed connection fields and reuses matching resources
rather than creating duplicates. With the stack running, deliberately reconcile
changes to locale, quality-profile selection, or stack-owned connections with:

```sh
docker compose run --rm --no-deps bootstrap
```

Unrelated manual configuration remains untouched.

## Automated onboarding

Bootstrap automatically creates a TV Shows library at `/data/media/tv` and a Movies
library at `/data/media/movies`. Reruns reuse libraries already associated with
those paths and reconcile their language and certification country. Changing
`LOCALE` affects future metadata matches; refresh existing Plex metadata if its
language or ratings also need to be changed.

The media mount is read-only in Plex by design. Sonarr and Radarr remain the sole
owners of imported media files. Plex keeps its downloaded metadata and artwork in
its writable `/config` mount. Deleting media from Plex or creating optimized versions
beside source files is therefore intentionally unavailable.

After bootstrap succeeds, open Seerr from the dashboard and sign in with the same Plex
account used during authorization. Plex, Sonarr, Radarr, their media roots, and the
selected quality profiles are already configured. Sonarr, Radarr, and Prowlarr do not
have separate login screens, and Transmission runs without RPC authentication. Keep
their published ports on a trusted LAN and do not expose them directly to the public
internet.

## Add authorized indexers

Open Prowlarr and add only indexers and sources the operator is authorized to use.
Prowlarr automatically synchronizes compatible indexers to Sonarr and Radarr because
bootstrap has already registered both applications.

Bootstrap creates a FlareSolverr indexer proxy using `http://127.0.0.1:8191` and the
`flaresolverr` tag. Prowlarr and FlareSolverr share Gluetun's network namespace, so
loopback is the correct address. Assign the `flaresolverr` tag only to affected
indexers. FlareSolverr is best-effort and cannot guarantee bypass of every challenge.

## Routine operation

Use Seerr for ordinary discovery and requests. Use Sonarr and Radarr for acquisition
policy, queue investigation, manual searches, and import problems. Use Prowlarr for
indexer health and Transmission for transfer-level diagnosis.

Use the host's LAN address or a DNS name that resolves to it to open the dashboard or
an individual service from another device. Do not publish the interfaces directly to
the public internet; remote access requires a trusted private network or a deliberately
configured authenticated reverse proxy.

Common lifecycle commands are:

```sh
docker compose up -d
docker compose ps --all
docker compose logs bootstrap
docker compose run --rm --no-deps bootstrap
docker compose down
```

For the first deployment, follow only the coordinator so the Plex authorization URL
and result remain visible without streaming every service log:

```sh
docker compose up -d
docker compose logs --follow bootstrap
```

Later starts need only `docker compose up -d`. Long-lived services use
`restart: unless-stopped` and return
automatically when Docker starts after a host reboot; the completed coordinator
container remains exited.

`docker compose down` preserves bind-mounted state. Do not add `--volumes` or delete
`CONFIG_ROOT`/`DATA_ROOT` unless the data loss is intentional.

## Versions, backup, and upgrades

Images use patch-floating major/minor tags where the publisher provides them. Some
publishers expose only full application-version tags; those services use the
narrowest available stable tag without a digest. This accepts compatible patch or
image rebuild updates while preventing automatic minor or major upgrades whenever
the registry's tagging scheme permits it. Review minor and major version changes one
service at a time, validate the Compose topology and VPN failure boundary, run
bootstrap tests, and record any manual migration requirement. Pull updated tags
before starting when an image refresh is wanted:

```sh
docker compose pull
docker compose up -d
```

Back up all of `CONFIG_ROOT`; it contains service databases, settings, API keys, and
Plex metadata. Back up `DATA_ROOT/media` if the media cannot simply be reacquired.
Transmission working data and `/data/transcode` are normally lower-value.
