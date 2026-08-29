# Advanced Operator Setup

This document explains the required VPN boundary and its verification. Apply these
instructions only to systems and traffic you are authorized to use.

## Required VPN egress

### Goal

Transmission, Prowlarr, and FlareSolverr must not reach the public internet except
through the configured VPN. Torrential enforces this in its only supported Compose
topology; relying on a host VPN route alone is insufficient.

On macOS, Docker Desktop runs containers inside a Linux VM. Outbound container
connections are created on the host by Docker Desktop's `com.docker.backend`
process. A macOS tunnel interface such as `utun4` is therefore not directly visible
inside a Compose container, and its numeric name may change after a reconnect.

Do not use `network_mode: host` or refer to a macOS interface such as `utun4` in the
Compose file. Neither gives a container a stable, enforceable route through that
host interface. Torrential includes a versioned
[Gluetun](https://github.com/qdm12/gluetun), whose network namespace and firewall are
shared by the three routed services.

### Credentials and startup

Set these values in the ignored local `.env` file:

```dotenv
VPN_SERVICE_PROVIDER=<Gluetun provider identifier>
OPENVPN_USER=<provider OpenVPN username>
OPENVPN_PASSWORD=<provider OpenVPN password>
OPENVPN_PROTOCOL=udp
SERVER_COUNTRIES="United Kingdom"
```

Use the provider identifier, credentials, and server filters documented in the
[Gluetun provider directory](https://github.com/qdm12/gluetun-wiki/tree/main/setup/providers).
The example `.env` selects NordVPN, but the OpenVPN username and password variables
are shared by Gluetun's supported OpenVPN providers.

OpenVPN uses UDP by default. If Gluetun repeatedly reports TLS negotiation timeouts
against multiple servers, set `OPENVPN_PROTOCOL=tcp` and recreate the stack. TCP
usually traverses restrictive networks more reliably, while UDP generally offers
better throughput when it is available.

Gluetun updates the active provider's persisted server list every 480 hours after the
VPN tunnel is established. If the embedded and persisted data are too stale to make
the initial connection, perform the documented manual recovery update before
starting the stack:

```bash
docker compose run --rm --no-deps gluetun update -enduser -providers nordvpn
```

Replace `nordvpn` with the configured provider. Some providers need additional
updater-specific arguments; consult Gluetun's
[server-list update guidance](https://github.com/qdm12/gluetun-wiki/blob/main/setup/servers.md#update-the-vpn-servers-list).
The update resolves VPN server hostnames outside the tunnel and can expose those DNS
queries to the host network.

After configuring `.env` as described in [Deployment and setup](deployment.md), start
the stack in the background and follow only the bootstrap log. On a new deployment,
approve the single Plex authorization URL shown there. Stop with ordinary Compose:

```bash
docker compose up -d
docker compose logs --follow bootstrap
docker compose down
```

Important properties of this arrangement:

- Transmission, Prowlarr, and FlareSolverr have no independent Compose network or
  alternative default route.
- Their outbound traffic uses Gluetun's network namespace and VPN firewall.
- Transmission and Prowlarr management ports are published by `gluetun`, not by the
  application containers. FlareSolverr remains internal-only.
- Gluetun's `transmission` and `prowlarr` network aliases preserve stable access from
  Sonarr, Radarr, and bootstrap outside the VPN namespace.
- Prowlarr reaches FlareSolverr at `http://127.0.0.1:8191` because they share one
  network namespace. FlareSolverr has no externally reachable alias or host port.
- `depends_on` handles startup order. Gluetun's firewall, rather than startup order,
  provides the runtime failure boundary.

Keep VPN credentials in an ignored, permission-restricted environment file or an
appropriate local secret store. Never commit them to the repository or include them
in diagnostic output.

### Why these three services use the VPN

- Transmission makes BitTorrent peer and tracker connections.
- Prowlarr makes ordinary indexer requests.
- For a tagged protected indexer, FlareSolverr's browser makes the challenged request
  on Prowlarr's behalf. Routing only Prowlarr would not protect that request.

Do not put Sonarr, Radarr, Seerr, Plex, or bootstrap behind the VPN merely for
uniformity. Plex must remain directly reachable by LAN clients, and routing every
service through one network namespace increases coupling. Expand the boundary only
for a concrete privacy requirement.

### Verification

With the VPN healthy:

```bash
docker compose exec -T gluetun \
  wget -qO- https://api.ipify.org
```

The result must be the VPN public address, not the host ISP address. Also verify that
Sonarr and Radarr can still reach Transmission using its stable internal URL. Test
one tagged indexer and verify that Prowlarr reaches FlareSolverr through their shared
namespace.

Test the failure boundary before relying on it:

1. Keep the Gluetun container running but temporarily make its VPN connection
   unhealthy using a controlled test method supported by the selected Gluetun
   version.
2. Confirm the diagnostic request fails rather than returning the host ISP address.
3. Confirm Transmission cannot make progress during the outage and that any
   VPN-routed FlareSolverr indexer test also fails closed.
4. Restore the VPN, verify the reported public address again, and confirm recovery.

Do not treat a successful startup or one public-IP check as proof of a kill switch.
Repeat this test after material Docker Desktop, Gluetun, VPN, or Compose changes.

### NordVPN port-forwarding limitation

Standard NordVPN servers do not provide incoming port forwarding. Transmission can
still establish outbound peer connections, but it may have fewer reachable peers
and lower performance than a VPN service that supplies an inbound port. Publishing
Transmission's peer port on the Mac does not create a forwarded port through the VPN
and should not be used as a workaround.

## Support boundary

The VPN topology and fail-closed container wiring are project-owned. Provider
selection, credentials, endpoint policy, and runtime public-IP verification are
operator-managed. The stack does not inspect the public IP or distinguish a VPN
outage from another networking failure. Inspect the configuration with
`docker compose config`; the routed services must not gain independent networks or
ports that conflict with `network_mode: "service:gluetun"`.

## References

- [Docker Desktop networking](https://docs.docker.com/desktop/features/networking/)
- [Docker Desktop network and VM FAQ](https://docs.docker.com/security/faqs/networking-and-vms/)
- [Gluetun NordVPN setup](https://github.com/qdm12/gluetun-wiki/blob/main/setup/providers/nordvpn.md)
- [Gluetun firewall behavior](https://github.com/qdm12/gluetun-wiki/blob/main/faq/firewall.md)
- [NordVPN manual service credentials](https://support.nordvpn.com/hc/en-us/articles/19685514639633-Changes-to-the-login-process-on-third-party-apps-and-routers)
- [NordVPN port-forwarding guidance](https://support.nordvpn.com/hc/en-us/articles/19684449567121-What-open-ports-does-NordVPN-offer)
