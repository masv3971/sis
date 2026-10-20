#!/usr/bin/env bash
# Convert an ELM credential to OEAPI through SIS (POST /api/v1/parse), as a
# customer (default LiU) would, and print the converted document on stdout.
# Nothing is stored in SIS.
#
# Usage:
#   developer_tools/scripts/test_parse-elm-oeapi.sh [elm-file] > oeapi.json
#
# Without elm-file the bundled sample internal/convert/testdata/edc_bsc.json
# is used. Conversion warnings and errors go to stderr.
#
# Environment (defaults fit `make start` on the host):
#   SIS_URL        https://localhost:8080
#   TOKEN_URL      https://localhost:9443/realms/sis/protocol/openid-connect/token
#   CLIENT_ID      liu
#   CLIENT_SECRET  liu-secret
#   CA_FILE        developer_tools/pki/rootCA.crt
#
# Requires curl and jq.
set -euo pipefail

FORMAT=oeapi

REPO_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
ELM_FILE="${1:-${REPO_DIR}/internal/convert/testdata/edc_bsc.json}"

SIS_URL="${SIS_URL:-https://localhost:8080}"
TOKEN_URL="${TOKEN_URL:-https://localhost:9443/realms/sis/protocol/openid-connect/token}"
CLIENT_ID="${CLIENT_ID:-liu}"
CLIENT_SECRET="${CLIENT_SECRET:-liu-secret}"
CA_FILE="${CA_FILE:-${REPO_DIR}/developer_tools/pki/rootCA.crt}"

fail() { echo "error: $*" >&2; exit 1; }

for cmd in curl jq; do
    command -v "$cmd" >/dev/null || fail "$cmd is not installed"
done
[[ -f "$ELM_FILE" ]] || fail "no such file: $ELM_FILE"
[[ -f "$CA_FILE" ]] || fail "no CA file: $CA_FILE (run make pki)"

resp="$(mktemp)"
trap 'rm -f "$resp"' EXIT

TOKEN="$(curl -sS --cacert "$CA_FILE" -X POST "$TOKEN_URL" \
    -u "${CLIENT_ID}:${CLIENT_SECRET}" -d grant_type=client_credentials |
    jq -r '.access_token // empty')" || fail "token request to ${TOKEN_URL} failed"
[[ -n "$TOKEN" ]] || fail "no access token for ${CLIENT_ID} from ${TOKEN_URL}"

status="$(jq -n --slurpfile d "$ELM_FILE" --arg out "$FORMAT" \
    '{metadata: {input_format: "elm", output_format: $out}, data: $d[0]}' |
    curl -sS --cacert "$CA_FILE" -o "$resp" -w '%{http_code}' \
        -X POST "${SIS_URL}/api/v1/parse" \
        -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" \
        --data-binary @-)" || fail "request to ${SIS_URL}/api/v1/parse failed"
if [[ "$status" != 200 ]]; then
    cat "$resp" >&2
    echo >&2
    fail "SIS answered HTTP ${status}"
fi

jq -r '.warnings[]? | "warning: \(.)"' "$resp" >&2
jq .data "$resp"
