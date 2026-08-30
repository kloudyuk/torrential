# Architecture

Torrential is the complete deployment described in this document.

```text
User
  +--> Dashboard
  +--> Seerr --> Sonarr/Radarr --> Transmission --> /data/torrents
                   ^                                      |
                   |                                      v
                Prowlarr <--- indexers       import and rename
                   ^                                      |
                   |                                      v
              Byparr when tagged               /data/media/{tv,movies}
                                                          |
                   Sonarr/Radarr -- scan request --> Plex <+
```

The bootstrap image first runs as a one-shot filesystem init service. After it
succeeds, the same image runs once as the configuration coordinator. The coordinator
waits for the other services, performs Plex account authorization when needed,
configures fixed paths, connections, and libraries, and exits.
Gluetun supplies the only network namespace available to Transmission,
Prowlarr, and Byparr.

## Ownership

| Concern | Owner |
| --- | --- |
| Local service navigation | Dashboard |
| Discovery, requests, approvals, and request status | Seerr |
| TV metadata, monitoring, release choice, import, and naming | Sonarr |
| Movie metadata, monitoring, release choice, import, and naming | Radarr |
| Indexers and manager synchronization | Prowlarr |
| Browser challenges for explicitly tagged indexers | Byparr |
| Torrent transfer state and files | Transmission |
| Library indexing, playback, and media-server users | Plex |
| Plex first-run state | Plex startup hook |
| Host-directory ownership, Plex authorization, locale defaults, stable paths, libraries, and internal service connections | Bootstrap |
| LAN identity, VPN credentials, indexer eligibility, quality-profile selection, and Plex approval | Operator |

Seerr delegates TV requests to Sonarr and movie requests to Radarr. Prowlarr
synchronizes its indexers into Sonarr and Radarr, which search for releases and send
their selections to Transmission. Sonarr and Radarr then import completed files into
their media roots, and Plex indexes those roots. Sonarr and Radarr own media import
and naming, then explicitly notify Plex after imports and upgrades so discovery does
not depend on bind-mount filesystem notifications.

## Storage contract

Sonarr, Radarr, Transmission, and bootstrap mount the same `DATA_ROOT` at `/data`:

```text
/data/
├── torrents/
│   ├── incomplete/
│   └── complete/
│       ├── sonarr/
│       └── radarr/
├── media/
│   ├── tv/
│   └── movies/
└── transcode/
```

Plex sees `/data/media` read-only and uses `/transcode` for temporary transcoding
work. Matching container paths allow Sonarr and Radarr to import without remote path
mappings and allow hard links when the host filesystem supports them.

Plex stores its library database, downloaded posters, descriptions, thumbnails, and
watch state under its writable configuration directory. It can also read sidecar
assets created by Sonarr or Radarr without writing to the media roots. Keeping media
read-only prevents Plex from deleting manager-owned files. Features that write beside
the source media, such as Plex optimized versions, require a separate writable
library location if enabled later.

Configuration databases live separately below `CONFIG_ROOT`. These directories and
`DATA_ROOT` are the persistence boundary; containers are disposable.

## Network boundary

The stack uses one private Compose network and stable DNS names. Its web interfaces
bind to all host interfaces by default for trusted-LAN access. Sonarr, Radarr,
Prowlarr, and Transmission do not require application authentication, so none of
these published ports may be exposed directly to the public internet. Set
`WEB_BIND_ADDRESS=127.0.0.1` to restrict every interface to the host instead.
Dashboard links preserve the hostname or IP address used to open it. Plex's
advertised server URL uses the operator-supplied `TORRENTIAL_HOST` and `PLEX_PORT`.
The dashboard proxies fixed, read-only readiness requests to its six linked services
and displays application availability without mounting the Docker socket. Its service
marks and Torrential favicon are bundled SVG assets, so they remain sharp and do not
depend on another service or an internet connection to render.

Transmission, Prowlarr, and Byparr unconditionally share Gluetun's network
namespace and have no independent fallback route. Gluetun publishes the Transmission
and Prowlarr management ports and provides their internal DNS aliases. Sonarr,
Radarr, Seerr, and Plex remain on the normal Compose network. The coordinator shares
Plex's network namespace only so the initial claim request originates from Plex
loopback; it still has normal Compose-network access and never inherits acquisition
VPN routing. Plex remains directly reachable by LAN clients.

The aliases mean other services still use `transmission:9091` and `prowlarr:9696`;
those names resolve to Gluetun's namespace. Prowlarr reaches Byparr through
shared loopback at `127.0.0.1:8191`.

## Automation boundary

Bootstrap converges stable local configuration through service APIs. It reads
generated service API keys and Plex's server token from read-only configuration
mounts, and does not edit service configuration files or databases.

On a new deployment it uses Plex PIN authorization to claim the server and create
Seerr's administrator with one approval. The Plex account token exists transiently
in bootstrap memory and is handed to Seerr through its normal sign-in API; bootstrap
does not retain it.

Before Plex Media Server starts, a small idempotent container hook accepts its EULA
and marks first-run setup complete. Bootstrap then claims the server and creates its
libraries, so opening Plex does not launch the redundant server setup wizard.

Policy-bearing choices supplied by the operator are:

- Plex account approval
- Stable LAN IP address or local DNS name, selected in `.env`
- Time zone and locale, selected in `.env`
- Seerr quality profiles, selected by name in `.env`
- Prowlarr indexers, credentials, tags, and Byparr assignment
- Sonarr/Radarr quality profiles, custom formats, and naming preferences

Bootstrap creates a Prowlarr proxy named Byparr and its `byparr` tag. Prowlarr calls
the compatible proxy implementation FlareSolverr. The operator assigns the tag only
to affected indexers. This boundary automates deterministic wiring without storing
extra account credentials or guessing which sources the operator is authorized to
use.
