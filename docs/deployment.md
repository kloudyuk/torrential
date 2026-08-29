# Deployment and setup

Torrential supports its project-owned Docker Compose deployment on ARM64 and AMD64.

## Configuration

Copy the example environment file:

```sh
cp .env.example .env
```

Before the first start:

1. Select `VPN_SERVICE_PROVIDER` and set its `OPENVPN_USER` and `OPENVPN_PASSWORD`.
2. Review `PUID`, `PGID`, `TZ`, `PLEX_LIBRARY_LANGUAGE`, storage paths, and the
   shared web-interface bind address and published ports.
3. If required, change the Sonarr and Radarr quality-profile names selected for
   Seerr. Both default to `HD-1080p`.

Leave `PLEX_ADVERTISE_URL` empty unless Plex clients need an explicit LAN URL for
this server. When set, it advertises that URL to clients; it does not change the
published bind address or enable remote access.

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
./scripts/stack-up.sh
```

Compose downloads missing service images and builds the bootstrap image as needed.
If Gluetun has no persisted server list, the wrapper first downloads the current list
for `VPN_SERVICE_PROVIDER`. It then starts the long-lived services, waits for them to
become healthy, runs the one-shot bootstrap, streams its output, propagates its exit
status, and prints the local web-interface URLs after a successful run.

The dashboard is available at <http://127.0.0.1> by default and links to all six
operator interfaces using their configured ports. When opened through the host's LAN
address, its links use that same address. The stack binds these interfaces to the LAN
by default; set `WEB_BIND_ADDRESS=127.0.0.1` for host-only access. Change
`DASHBOARD_PORT` if host port 80 is already in use. Do not expose any stack ports
directly to the public internet. The bundled dashboard serves plain HTTP; Torrential
does not manage local TLS certificates or HTTPS termination.

On a fresh deployment, bootstrap prints one `app.plex.tv` authorization URL. Open it,
sign in to the Plex account that will own the server, and approve Torrential. The
script continues automatically; no credential needs to be copied. The authorization
request expires after 15 minutes. Rerun the script to generate a new one if
necessary. Plex's page can show a generic completion error after successful approval;
the bootstrap output is authoritative. Existing completed deployments skip this
step.

Plex startup and bootstrap perform these convergent operations:

- creates `/data/torrents/incomplete`, `/data/torrents/complete/{sonarr,radarr}`, and
  `/data/media/{tv,movies}`;
- configures Transmission's complete and incomplete directories;
- adds `/data/media/tv` to Sonarr and `/data/media/movies` to Radarr;
- registers Transmission in Sonarr and Radarr with separate categories;
- registers Sonarr and Radarr as full-sync Prowlarr applications;
- creates a tagged FlareSolverr indexer proxy in Prowlarr;
- marks Plex's first-run server setup complete before Plex starts;
- uses one Plex authorization to claim a new Plex server and create Seerr's initial
  administrator;
- creates Plex TV Shows and Movies libraries for the corresponding media roots;
- connects Seerr to Plex, enables both libraries, and adds Sonarr and Radarr as its
  default services using the configured quality profiles.

Rerunning it updates fixed connection fields and reuses matching resources rather
than creating duplicates. It leaves unrelated manual configuration untouched.

## Automated onboarding

Bootstrap automatically creates a TV Shows library at `/data/media/tv` and a Movies
library at `/data/media/movies`. Reruns reuse libraries already associated with
those paths.

The media mount is read-only in Plex by design. Sonarr and Radarr remain the sole
owners of imported media files. Plex keeps its downloaded metadata and artwork in
its writable `/config` mount. Deleting media from Plex or creating optimized versions
beside source files is therefore intentionally unavailable.

After bootstrap succeeds, Seerr is initialized and ready at
<http://127.0.0.1:5055>. Sign in with the same Plex account used during authorization.
Plex, Sonarr, Radarr, their media roots, and the selected quality profiles are already
configured. Sonarr, Radarr, and Prowlarr do not have separate login screens, and
Transmission runs without RPC authentication. Keep their published ports on a trusted
LAN and do not expose them directly to the public internet.

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

Use the host's LAN address to open the dashboard or an individual service from another
device. Do not publish the interfaces directly to the public internet; remote access
requires a trusted private network or a deliberately configured authenticated reverse
proxy.

Common lifecycle commands are:

```sh
docker compose ps
docker compose logs bootstrap
./scripts/stack-up.sh
docker compose down
```

`docker compose down` preserves bind-mounted state. Do not add `--volumes` or delete
`CONFIG_ROOT`/`DATA_ROOT` unless the data loss is intentional.

## Versions, backup, and upgrades

Every bundled image is pinned to a reviewed version and immutable multi-platform
digest. Do not replace pins with floating tags. An upgrade should update one pin at
a time, validate the Compose topology and VPN failure boundary, run bootstrap tests,
and record any manual migration requirement. After changing image pins or bootstrap
code, explicitly refresh the deployment before starting it:

```sh
docker compose pull --ignore-buildable
docker compose build bootstrap
./scripts/stack-up.sh
```

Back up all of `CONFIG_ROOT`; it contains service databases, settings, API keys, and
Plex metadata. Back up `DATA_ROOT/media` if the media cannot simply be reacquired.
Transmission working data and `/data/transcode` are normally lower-value.
