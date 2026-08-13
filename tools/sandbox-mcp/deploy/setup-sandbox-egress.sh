#!/usr/bin/env bash
# Create the dedicated sandbox egress network and install idempotent host rules.
# Review on the target host before running. Development tooling never executes this script.
set -euo pipefail

NETWORK_NAME="${SANDBOX_NETWORK_NAME:-deeix-sandbox-egress}"
SUBNET="${SANDBOX_NETWORK_SUBNET:-172.30.0.0/24}"
GATEWAY="${SANDBOX_NETWORK_GATEWAY:-172.30.0.1}"
FORWARD_CHAIN="DEEIX-SANDBOX-EGRESS"
HOST_CHAIN="DEEIX-SANDBOX-HOST"

require_command() {
  command -v "$1" >/dev/null 2>&1 || {
    printf 'required command not found: %s\n' "$1" >&2
    exit 1
  }
}

ensure_chain() {
  local table="$1"
  local chain="$2"
  "$table" -nL "$chain" >/dev/null 2>&1 || "$table" -N "$chain"
  "$table" -F "$chain"
}

ensure_jump() {
  local table="$1"
  local parent="$2"
  local bridge="$3"
  local target="$4"
  "$table" -C "$parent" -i "$bridge" -j "$target" 2>/dev/null || \
    "$table" -I "$parent" 1 -i "$bridge" -j "$target"
}

require_command docker
require_command iptables

if ! docker network inspect "$NETWORK_NAME" >/dev/null 2>&1; then
  docker network create \
    --driver bridge \
    --subnet "$SUBNET" \
    --gateway "$GATEWAY" \
    --opt com.docker.network.bridge.enable_icc=false \
    "$NETWORK_NAME" >/dev/null
fi

enable_ipv6="$(docker network inspect -f '{{.EnableIPv6}}' "$NETWORK_NAME")"
if [[ "$enable_ipv6" != "false" ]]; then
  printf 'sandbox network %s must be IPv4-only; got EnableIPv6=%s\n' "$NETWORK_NAME" "$enable_ipv6" >&2
  exit 1
fi

bridge_name="$(docker network inspect -f '{{index .Options "com.docker.network.bridge.name"}}' "$NETWORK_NAME")"
if [[ -z "$bridge_name" || "$bridge_name" == "<no value>" ]]; then
  network_id="$(docker network inspect -f '{{.Id}}' "$NETWORK_NAME")"
  bridge_name="br-${network_id:0:12}"
fi
if [[ ! -d "/sys/class/net/$bridge_name" ]]; then
  printf 'sandbox bridge interface not found: %s\n' "$bridge_name" >&2
  exit 1
fi

# Traffic addressed to any host-local IPv4 address uses INPUT rather than
# DOCKER-USER. Drop it unconditionally for the sandbox bridge, including the
# bridge gateway and the host's public addresses.
ensure_chain iptables "$HOST_CHAIN"
iptables -A "$HOST_CHAIN" -j DROP
ensure_jump iptables INPUT "$bridge_name" "$HOST_CHAIN"

# Forwarded sandbox traffic may reach only globally routed IPv4 destinations.
# Docker's embedded DNS remains inside the container namespace; DNS answers that
# resolve to a denied range are blocked here before leaving the bridge.
ensure_chain iptables "$FORWARD_CHAIN"
for cidr in \
  0.0.0.0/8 \
  10.0.0.0/8 \
  100.64.0.0/10 \
  127.0.0.0/8 \
  169.254.0.0/16 \
  172.16.0.0/12 \
  192.0.0.0/24 \
  192.0.2.0/24 \
  192.168.0.0/16 \
  198.18.0.0/15 \
  198.51.100.0/24 \
  203.0.113.0/24 \
  224.0.0.0/4 \
  240.0.0.0/4; do
  iptables -A "$FORWARD_CHAIN" -d "$cidr" -j DROP
done
iptables -A "$FORWARD_CHAIN" -j RETURN
ensure_jump iptables DOCKER-USER "$bridge_name" "$FORWARD_CHAIN"

# The network is deliberately created without IPv6. If a host still exposes an
# IPv6 Docker forwarding chain, reject any packet arriving from this bridge as
# defense in depth. Absence of ip6tables/DOCKER-USER is valid for IPv4-only Docker.
if command -v ip6tables >/dev/null 2>&1 && ip6tables -nL DOCKER-USER >/dev/null 2>&1; then
  ensure_chain ip6tables "$FORWARD_CHAIN"
  ip6tables -A "$FORWARD_CHAIN" -j DROP
  ensure_jump ip6tables DOCKER-USER "$bridge_name" "$FORWARD_CHAIN"
fi
if command -v ip6tables >/dev/null 2>&1 && ip6tables -nL INPUT >/dev/null 2>&1; then
  ensure_chain ip6tables "$HOST_CHAIN"
  ip6tables -A "$HOST_CHAIN" -j DROP
  ensure_jump ip6tables INPUT "$bridge_name" "$HOST_CHAIN"
fi

printf 'sandbox egress rules installed for %s (%s)\n' "$NETWORK_NAME" "$bridge_name"
