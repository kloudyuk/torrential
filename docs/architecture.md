# Architecture

Torrential is the complete deployment described in this document.

```text
User
  |\
  | +--> Dashboard (links to all web interfaces)
  |
  v
Seerr -------------------------------> Plex Media Server
  |                                       ^
  +--> Sonarr (TV) ---+                    |
  |                   +--> Transmission   |
  +--> Radarr (movie)-+        |           |
          ^                    v           |
          |              /data/torrents    |
       Prowlarr                 |           |
          |                     v           |
       indexers          Sonarr/Radarr import and rename
          ^                     |
          |                     v
   FlareSolverr when tagged  /data/media/{tv,movies}
```

Bootstrap performs the one-time Plex account authorization when needed, configures
fixed paths, service connections, and libraries, then exits.
Gluetun supplies the only network namespace available to Transmission,
Prowlarr, and FlareSolverr.

## Ownership

| Concern | Owner |
| --- | --- |
| Local service navigation | Dashboard |
| Discovery, requests, approvals, and request status | Seerr |
| TV metadata, monitoring, release choice, import, and naming | Sonarr |
| Movie metadata, monitoring, release choice, import, and naming | Radarr |
| Indexers and manager synchronization | Prowlarr |
| Browser challenges for explicitly tagged indexers | FlareSolverr |
| Torrent transfer state and files | Transmission |
| Library indexing, playback, and media-server users | Plex |
| Plex first-run state | Plex startup hook |
| Plex authorization, stable paths, libraries, and internal service connections | Bootstrap |
| VPN credentials, indexer eligibility, quality-profile selection, and Plex approval | Operator |

Seerr delegates TV requests to Sonarr and movie requests to Radarr. Prowlarr
synchronizes its indexers into Sonarr and Radarr, which search for releases and send
their selections to Transmission. Sonarr and Radarr then import completed files into
their media roots, and Plex indexes those roots. Sonarr and Radarr own media import
and naming.

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

Transmission, Prowlarr, and FlareSolverr unconditionally share Gluetun's network
namespace and have no independent fallback route. Gluetun publishes the Transmission
and Prowlarr management ports and provides their internal DNS aliases. Sonarr,
Radarr, Seerr, and Plex remain on the normal network. The one-shot bootstrap shares
Plex's network namespace so Plex sees its server-claim request as local; it retains
access to the normal Compose network and never inherits acquisition VPN routing.
Plex remains directly reachable by LAN clients.

The aliases mean other services still use `transmission:9091` and `prowlarr:9696`;
those names resolve to Gluetun's namespace. Prowlarr reaches FlareSolverr through
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
- Seerr quality profiles, selected by name in `.env`
- Prowlarr indexers, credentials, tags, and FlareSolverr assignment
- Sonarr/Radarr quality profiles, custom formats, and naming preferences

Bootstrap creates the FlareSolverr proxy and its `flaresolverr` tag. The operator
assigns that tag only to affected indexers. This boundary automates deterministic
wiring without storing extra account credentials or guessing which sources the
operator is authorized to use.
