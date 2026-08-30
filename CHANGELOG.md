# Changelog

All notable changes to Torrential are recorded here. The project follows
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [1.0.0] - 2026-08-30

First stable release.

### Added

- Long-running Go controller for Docker-reported service state, health, start time,
  and allowlisted restarts.
- Dashboard uptime, health-aware tiles, per-service restart controls, and an ordered
  stack restart that waits for Gluetun before restarting VPN-routed services.
- Custom restart confirmation, polished local service launcher, Torrential branding,
  and a GitHub project link.
- Automated GitHub Release creation after successful multi-platform runtime image
  publication.

### Changed

- The one project-owned Go image now provides filesystem initialization,
  configuration bootstrap, and runtime controller modes.
- Dashboard availability now reflects Docker health rather than independent
  application probes.
- Byparr health-check timing is tolerant of its comparatively slow browser probe.
- Documentation now describes the Docker control boundary, trusted-LAN model, and
  complete stable deployment architecture.

### Security

- The controller is internal-only, resolves containers within the configured Compose
  project, and restricts restart targets to a fixed allowlist.
- Restart requests require same-origin browser context and an explicit confirmation
  header. Docker socket access remains privileged and is documented as a trusted-LAN
  boundary.

## [0.3.1] - 2026-08-30

- Clarified the stack's responsibilities and corrected documentation wording.

## [0.3.0] - 2026-08-30

- Added the local service dashboard, application availability indicators, bundled
  SVG marks, and Torrential branding.

## [0.2.2] - 2026-08-29

- Extended bootstrap request timeouts for slower hosts such as Raspberry Pi.

## [0.2.1] - 2026-08-29

- Made existing Prowlarr Byparr proxy reconciliation reliable and idempotent.

## [0.2.0] - 2026-08-29

- Replaced FlareSolverr with Byparr and automated its tagged Prowlarr proxy.

## [0.1.1] - 2026-08-29

- Clarified deployment and bootstrap operation documentation.

## [0.1.0] - 2026-08-29

- Introduced the Compose stack, Go bootstrap coordinator, automated Plex/Seerr
  onboarding, required Gluetun routing, and persistent storage contract.

[Unreleased]: https://github.com/kloudyuk/torrential/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/kloudyuk/torrential/compare/v0.3.1...v1.0.0
[0.3.1]: https://github.com/kloudyuk/torrential/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/kloudyuk/torrential/compare/v0.2.2...v0.3.0
[0.2.2]: https://github.com/kloudyuk/torrential/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/kloudyuk/torrential/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/kloudyuk/torrential/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/kloudyuk/torrential/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/kloudyuk/torrential/releases/tag/v0.1.0
