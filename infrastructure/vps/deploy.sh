#!/usr/bin/env bash
# Build ufacapitald from the current commit and install it as a systemd service.
# Run from the repository root as a user with sudo. Builds only from a clean tree,
# so the binary carries the exact git revision (see: go version -m).
# Does not touch Docker, other projects, the firewall or the env files.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
if [ -n "$(git status --porcelain)" ]; then
  echo "working tree is not clean; commit first" >&2
  exit 1
fi
rev=$(git rev-parse HEAD)
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT

CGO_ENABLED=0 go build -trimpath -o "$out/ufacapitald" ./server/cmd/ufacapitald

sudo test -f /opt/ufacapital.env || { echo "/opt/ufacapital.env is missing" >&2; exit 1; }
id ufacapital >/dev/null 2>&1 || sudo useradd --system --no-create-home --shell /usr/sbin/nologin ufacapital
sudo install -d -o root -g root -m 0755 /opt/ufacapital /opt/ufacapital/migrations
sudo install -o root -g root -m 0755 "$out/ufacapitald" /opt/ufacapital/ufacapitald.new
sudo mv /opt/ufacapital/ufacapitald.new /opt/ufacapital/ufacapitald
sudo find /opt/ufacapital/migrations -maxdepth 1 -name '*.sql' -delete
sudo install -o root -g root -m 0644 server/migrations/*.sql /opt/ufacapital/migrations/
sudo install -o root -g root -m 0644 infrastructure/vps/ufacapital.service /etc/systemd/system/ufacapital.service
sudo systemctl daemon-reload
sudo systemctl enable ufacapital >/dev/null
sudo systemctl restart ufacapital

for _ in $(seq 1 30); do
  if curl -fsS -m 2 http://127.0.0.1:8088/healthz >/dev/null 2>&1; then
    echo "deployed $rev"
    go version -m /opt/ufacapital/ufacapitald | grep vcs
    exit 0
  fi
  sleep 1
done
echo "healthz did not answer in 30 s" >&2
sudo journalctl -u ufacapital -n 30 --no-pager >&2
exit 1
