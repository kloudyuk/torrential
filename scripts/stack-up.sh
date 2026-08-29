#!/bin/sh
set -eu

started_at=$(date -u "+%Y-%m-%dT%H:%M:%SZ")

compose_environment_value() {
  variable_name=$1
  docker compose config --environment |
    awk -F= -v name="$variable_name" '$1 == name {sub(/^[^=]*=/, ""); print; exit}'
}

update_vpn_servers_if_missing() {
  config_root=$(compose_environment_value CONFIG_ROOT)
  config_root=${config_root:-./state/config}
  servers_file=$config_root/gluetun/servers.json
  if [ -f "$servers_file" ]; then
    return
  fi

  vpn_provider=$(compose_environment_value VPN_SERVICE_PROVIDER)
  vpn_provider=${vpn_provider:-nordvpn}
  echo "VPN server list not found; downloading it for $vpn_provider"
  docker compose run --rm --no-deps gluetun update -enduser -providers "$vpn_provider"
}

wait_for_service() {
  waited_service=$1
  waited_id=$(docker compose ps --all --quiet "$waited_service")
  if [ -z "$waited_id" ]; then
    echo "$waited_service container was not created" >&2
    exit 1
  fi
  docker logs --follow --since "$started_at" "$waited_id" &
  waited_logs_pid=$!
  waited_exit=$(docker wait "$waited_id")
  wait "$waited_logs_pid" || true
}

print_service_url() {
  label=$1
  service=$2
  container_port=$3
  path=$4

  published=$(docker compose port "$service" "$container_port")
  address=${published%:*}
  port=${published##*:}
  case "$address" in
    0.0.0.0|"[::]") address=127.0.0.1 ;;
  esac
  printf '%s: <http://%s:%s%s>\n' "$label" "$address" "$port" "$path"
}

update_vpn_servers_if_missing

docker compose up -d --remove-orphans --wait --wait-timeout 300 \
  dashboard gluetun sonarr radarr seerr plex prowlarr flaresolverr transmission

docker compose up -d --no-deps --build bootstrap

wait_for_service bootstrap

if [ "$waited_exit" -ne 0 ]; then
  echo "bootstrap failed with exit code $waited_exit" >&2
  exit "$waited_exit"
fi

echo "media stack is healthy and bootstrapped"
print_service_url "Dashboard" dashboard 80 ""
print_service_url "Seerr" seerr 5055 ""
print_service_url "Plex" plex 32400 /web
print_service_url "Sonarr" sonarr 8989 ""
print_service_url "Radarr" radarr 7878 ""
print_service_url "Prowlarr" gluetun 9696 ""
print_service_url "Transmission" gluetun 9091 /transmission/web/
