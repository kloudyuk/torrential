# Torrential

Torrential is an opinionated, self-hosted TV and movie acquisition stack. It packages
its services as one Docker Compose deployment.

| Service | Responsibility |
| --- | --- |
| Dashboard | Local launcher for the stack's web interfaces |
| [Seerr](https://docs.seerr.dev/) | Unified discovery and request interface |
| [Sonarr](https://sonarr.tv/) | TV acquisition, monitoring, import, and naming |
| [Radarr](https://radarr.video/) | Movie acquisition, monitoring, import, and naming |
| [Prowlarr](https://prowlarr.com/) | Indexer management and synchronization |
| [FlareSolverr](https://github.com/FlareSolverr/FlareSolverr) | Best-effort browser-challenge proxy for affected indexers |
| [Transmission](https://transmissionbt.com/) | BitTorrent downloads |
| [Plex Media Server](https://www.plex.tv/media-server-downloads/) | Media libraries and playback |
| [Gluetun](https://github.com/qdm12/gluetun) | Required VPN routing and kill switch for Transmission, Prowlarr, and FlareSolverr |
| Bootstrap | Host-directory preparation, Plex authorization, and idempotent stack configuration |

Use Torrential only with content and sources you are legally authorized to access.

## Quick start

Docker Desktop, or Docker Engine with Docker Compose 2.20.0 or newer, is required.

```sh
cp .env.example .env
```

Edit `.env`, select a Gluetun VPN provider, and set its OpenVPN credentials. VPN
routing is mandatory: Compose refuses to render without credentials, and the routed
services cannot start until Gluetun is healthy.

`TZ` controls clocks and `LOCALE` controls language and regional media defaults.
The example uses `Europe/London` and `en-GB`.

Set `TORRENTIAL_HOST` in `.env` to the Docker host's stable LAN IP address or local
DNS name, without a scheme or port. Torrential uses it for Plex's advertised server
URL and the dashboard completion banner. `DASHBOARD_PORT` and `PLEX_PORT` are applied
automatically.

Start the stack:

```sh
docker compose up -d
docker compose logs --follow bootstrap
```

Compose downloads missing service images, including Torrential's small prebuilt
bootstrap image.

On a fresh deployment, bootstrap prints one Plex authorization URL in its log and
waits. Open it, sign in to Plex, and approve Torrential. Bootstrap
uses that single authorization to claim Plex and create Seerr's administrator; there
is no token to copy or store. Follow the terminal output after approval; Plex's
authorization page can display a generic completion error even after approval
succeeds. Subsequent starts do not prompt again.

Open the default interfaces from the Docker host:

- Dashboard: <http://127.0.0.1>
- Seerr: <http://127.0.0.1:5055>
- Plex: <http://127.0.0.1:32400/web>
- Sonarr: <http://127.0.0.1:8989>
- Radarr: <http://127.0.0.1:7878>
- Prowlarr: <http://127.0.0.1:9696>
- Transmission: <http://127.0.0.1:9091/transmission/web/>

From another LAN device, replace `127.0.0.1` with the Docker host's LAN address or a
DNS name that resolves to it. The dashboard uses whichever hostname or IP address
opened it, together with each service's configured port, for all links.

Bootstrap creates the shared data directories, configures Transmission, connects the
manager and indexer services, creates the Plex libraries, and configures Seerr with
Plex, Sonarr, and Radarr. It also configures Sonarr and Radarr to ask Plex to scan
the relevant library after imports and upgrades. Sonarr, Radarr, Prowlarr, and
Transmission require no separate sign-in and are intended only for a trusted local
network. Indexer selection remains an operator decision. Follow
[Deployment and setup](docs/deployment.md).

Persistent state is stored under `./state` by default and excluded from Git.
Stop and remove the containers with `docker compose down`. After onboarding,
`docker compose up -d` starts them in the background.

## Development

The bootstrap utility is the project's only compiled component. It is a
standard-library Go program contained entirely in `bootstrap/`.

```sh
cd bootstrap
go test ./...
go build ./...
```

Use `compose.dev.yaml` to build and exercise bootstrap changes locally. Production
deployments use the versioned multi-platform image published to GHCR.

See [Architecture](docs/architecture.md), [Bootstrap](docs/bootstrap.md), and
[Advanced operator setup](docs/advanced-setup.md) for the supported boundaries.

## License

Torrential is available under the [MIT License](LICENSE).
