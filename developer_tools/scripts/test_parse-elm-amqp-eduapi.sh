#!/usr/bin/env bash
# Load an ELM credential into SIS over AMQP, as a customer (default LiU) would,
# and print what SIS now serves for it in Edu-API shape on stdout.
#
# The AMQP API has no parse command and no replies, so this publishes a
# populate command (with a TTL) to the sis.commands queue through the
# RabbitMQ management HTTP API, waits until SIS has consumed it, and reads
# the records back from /eduapi. Unlike test_parse-elm-eduapi.sh the records
# ARE stored (until TTL expires).
#
# Usage:
#   developer_tools/scripts/test_parse-elm-amqp-eduapi.sh [elm-file] > eduapi.json
#
# Without elm-file the bundled sample internal/convert/testdata/edc_bsc.json
# is used. Progress and errors go to stderr; a rejected command lands in
# sis.commands.dlq (the reason is in the SIS log: docker compose logs sis).
#
# Records are matched by dateLastModified >= publish time, so anything else
# written to SIS meanwhile shows up too.
#
# Environment (defaults fit `make start` on the host):
#   SIS_URL            https://localhost:8080
#   TOKEN_URL          https://localhost:9443/realms/sis/protocol/openid-connect/token
#   CLIENT_ID          liu
#   CLIENT_SECRET      liu-secret
#   CA_FILE            developer_tools/pki/rootCA.crt
#   RABBITMQ_API       http://localhost:15673/api
#   RABBITMQ_USER      sis
#   RABBITMQ_PASSWORD  sis
#   QUEUE              sis.commands
#   DLQ                sis.commands.dlq
#   TTL                1h   (empty: keep the records permanently)
#   TIMEOUT            30   (seconds to wait for SIS)
#
# Requires curl and jq.
set -euo pipefail

FORMAT=eduapi

REPO_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
ELM_FILE="${1:-${REPO_DIR}/internal/convert/testdata/edc_bsc.json}"

SIS_URL="${SIS_URL:-https://localhost:8080}"
TOKEN_URL="${TOKEN_URL:-https://localhost:9443/realms/sis/protocol/openid-connect/token}"
CLIENT_ID="${CLIENT_ID:-liu}"
CLIENT_SECRET="${CLIENT_SECRET:-liu-secret}"
CA_FILE="${CA_FILE:-${REPO_DIR}/developer_tools/pki/rootCA.crt}"
RABBITMQ_API="${RABBITMQ_API:-http://localhost:15673/api}"
RABBITMQ_USER="${RABBITMQ_USER:-sis}"
RABBITMQ_PASSWORD="${RABBITMQ_PASSWORD:-sis}"
QUEUE="${QUEUE:-sis.commands}"
DLQ="${DLQ:-sis.commands.dlq}"
TTL="${TTL-1h}"
TIMEOUT="${TIMEOUT:-30}"

# Edu-API list endpoints (path == response key), in parse output order.
EDU_TYPES=(persons orgs academicSessions courses programs classes enrollments results)
PAGE=500

fail() { echo "error: $*" >&2; exit 1; }

for cmd in curl jq; do
    command -v "$cmd" >/dev/null || fail "$cmd is not installed"
done
[[ -f "$ELM_FILE" ]] || fail "no such file: $ELM_FILE"
[[ -f "$CA_FILE" ]] || fail "no CA file: $CA_FILE (run make pki)"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# rabbit METHOD PATH [curl args...]: call the management API.
rabbit() {
    local method=$1 path=$2
    shift 2
    curl -sS --fail-with-body -u "${RABBITMQ_USER}:${RABBITMQ_PASSWORD}" \
        -X "$method" "${RABBITMQ_API}${path}" "$@" ||
        fail "RabbitMQ management API ${method} ${path} failed"
}

# queue_messages QUEUE: messages in the queue (ready + unacknowledged).
queue_messages() {
    rabbit GET "/queues/%2F/$(jq -rn --arg q "$1" '$q|@uri')" | jq '.messages // 0'
}

# sis PATH [curl args...]: GET from SIS with the bearer token.
sis() {
    local path=$1
    shift
    curl -sS --fail-with-body --cacert "$CA_FILE" \
        -H "Authorization: Bearer ${TOKEN}" "${SIS_URL}${path}" "$@" ||
        fail "GET ${SIS_URL}${path} failed"
}

# header NAME: the value of response header NAME in $tmp/headers.
header() {
    tr -d '\r' <"$tmp/headers" |
        awk -F': *' -v h="$1" 'tolower($1) == tolower(h) {print $2}'
}

# edu_list TYPE: every TYPE record modified since T0, one JSON per line.
# SIS clamps limit to sis.adapter.inbound.eduapi.max_limit, so the offset
# advances by the page size it applied (X-Limit), not by PAGE or by the
# items returned (orphans are left out of a page).
edu_list() {
    local typ=$1 offset=0 total limit
    while :; do
        sis "/eduapi/${typ}?limit=${PAGE}&offset=${offset}" \
            -D "$tmp/headers" -o "$tmp/page" >/dev/null
        jq -c --arg k "$typ" --arg t "$T0" \
            '.[$k][]? | select((.dateLastModified // "") >= $t)' "$tmp/page"
        total="$(header X-Total-Count)"
        limit="$(header X-Limit)"
        [[ "$total" =~ ^[0-9]+$ ]] || fail "/eduapi/${typ}: no X-Total-Count header"
        [[ "$limit" =~ ^[1-9][0-9]*$ ]] || fail "/eduapi/${typ}: no X-Limit header"
        offset=$((offset + limit))
        ((offset < total)) || break
    done
}

# collect: the Edu-API records modified since T0, keyed like parse output.
collect() {
    local typ
    for typ in "${EDU_TYPES[@]}"; do
        edu_list "$typ" | jq -cs --arg k "$typ" '{($k): .}'
    done | jq -cs 'add | with_entries(select(.value | length > 0))'
}

# render_oeapi BUNDLE: fetch each record of an Edu-API bundle from /oeapi.
render_oeapi() {
    jq -r 'to_entries[] | .key as $k | .value[] | "\($k)\t\(.sourcedId)"' <<<"$1" |
        while IFS=$'\t' read -r typ sid; do
            case "$typ" in
            persons) path=persons key=persons ;;
            orgs) path=organizations key=organizations ;;
            academicSessions) path=academic-sessions key=academicSessions ;;
            courses) path=courses key=courses ;;
            programs) path=programs key=programs ;;
            classes) path=offerings key=offerings ;;
            enrollments) path=associations key=associations ;;
            results) path=results key=results ;;
            *) fail "unexpected type ${typ}" ;;
            esac
            sis "/oeapi/${path}/$(jq -rn --arg s "$sid" '$s|@uri')" |
                jq -c --arg k "$key" '{key: $k, value: .}'
        done | jq -s 'reduce .[] as $e ({}; .[$e.key] += [$e.value])'
}

TOKEN="$(curl -sS --cacert "$CA_FILE" -X POST "$TOKEN_URL" \
    -u "${CLIENT_ID}:${CLIENT_SECRET}" -d grant_type=client_credentials |
    jq -r '.access_token // empty')" || fail "token request to ${TOKEN_URL} failed"
[[ -n "$TOKEN" ]] || fail "no access token for ${CLIENT_ID} from ${TOKEN_URL}"

dlq_before="$(queue_messages "$DLQ")"
T0="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
message_id="elm-$(date +%s)-$$"

# The AMQP 0-9-1 type and headers reach SIS as the AMQP 1.0 subject
# (x-basic-type) and application properties.
routed="$(jq -n --slurpfile d "$ELM_FILE" --arg q "$QUEUE" --arg ttl "$TTL" \
    --arg tok "Bearer ${TOKEN}" --arg mid "$message_id" '{
        properties: {
            type: "populate", content_type: "application/json",
            delivery_mode: 2, message_id: $mid,
            headers: {authorization: $tok}
        },
        routing_key: $q,
        payload: ({metadata: {input_format: "elm"}, data: $d[0]}
            + (if $ttl == "" then {} else {ttl: $ttl} end) | tojson),
        payload_encoding: "string"
    }' | rabbit POST /exchanges/%2F/amq.default/publish \
    -H "Content-Type: application/json" --data-binary @- | jq -r .routed)"
[[ "$routed" == true ]] || fail "message not routed: is there a queue ${QUEUE}?"
echo "published populate ${message_id} to ${QUEUE}; waiting for SIS" >&2

# Done when the queue is drained and the records read back the same twice
# in a row (populate writes them one by one); rejected when the DLQ grows.
deadline=$((SECONDS + TIMEOUT))
prev=""
while :; do
    if (($(queue_messages "$DLQ") > dlq_before)); then
        fail "SIS rejected ${message_id}: dead-lettered to ${DLQ} (reason in the SIS log)"
    fi
    if (($(queue_messages "$QUEUE") == 0)); then
        bundle="$(collect)"
        [[ "$bundle" != "{}" && "$bundle" == "$prev" ]] && break
        prev="$bundle"
    fi
    ((SECONDS < deadline)) || fail "no result after ${TIMEOUT}s (is SIS consuming ${QUEUE}?)"
    sleep 1
done

if [[ "$FORMAT" == oeapi ]]; then
    render_oeapi "$bundle"
else
    jq . <<<"$bundle"
fi
