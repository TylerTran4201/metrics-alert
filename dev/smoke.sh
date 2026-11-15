#!/bin/sh
set -eu
cd "$(dirname "$0")/.."

wait_for() {
    for attempt in $(seq 1 35); do
        if "$@"; then
            return 0
        fi
        sleep 1
    done
    printf 'Timed out waiting for %s\n' "$*" >&2
    docker compose logs --tail=15 primary secondary webhook >&2
    return 1
}

has_alert() {
    docker compose logs --no-color --since "$since" webhook | grep -Fq "$1"
}

primary_healthy() {
    curl --silent --fail --max-time 2 http://127.0.0.1:19080/healthz >/dev/null
}

since=$(date -u +%Y-%m-%dT%H:%M:%SZ)
docker compose up --build -d --force-recreate primary secondary
wait_for primary_healthy
wait_for has_alert '[FIRING] demo-node-memory'
wait_for has_alert '[FIRING] demo-rabbitmq-connections'

since=$(date -u +%Y-%m-%dT%H:%M:%SZ)
docker compose exec -T redis redis-cli BLPOP metrics-alert-smoke 12 >/dev/null &
client=$!
trap 'docker compose start primary >/dev/null 2>&1 || true' EXIT
wait_for has_alert '[FIRING] demo-redis-clients'
wait "$client"
wait_for has_alert '[RESOLVED] demo-redis-clients'

docker compose stop primary
wait_for has_alert '[FIRING] Metrics Alert primary is unavailable'
sleep 5
docker compose start primary
wait_for primary_healthy
wait_for has_alert '[RESOLVED] Metrics Alert primary is healthy'

for event in '[FIRING] Metrics Alert primary is unavailable' '[RESOLVED] Metrics Alert primary is healthy'; do
    count=$(docker compose logs --no-color --since "$since" webhook | grep -Fc "$event")
    test "$count" -eq 1
done
printf 'Exporter rules, Slack payload delivery, primary outage, and recovery passed.\n'
