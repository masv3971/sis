# sis

Student Information System: one canonical store (PostgreSQL) served through
three API surfaces and an admin web UI.

- `/api/v1`: native read/write API
- `/oeapi`: Open Education API v6.0 (read-only)
- `/eduapi`: Edu-API v1.0 (read-only)
- Web UI: React + TypeScript admin SPA

## Quick start

```bash
cp config.yaml.sample config.yaml   # then edit; see comments in the file
make start                          # sis + postgres + mellon (OIDC test IdP for testing) via docker compose
```

Under compose, set `sis.httpserver.web.root: "/web"` (the image's SPA) and
point `sis.adapter.outbound.postgres.connection_string` at host `postgres`
instead of `localhost`. To consume commands from the compose RabbitMQ (see
[AMQP 1.0](#amqp-10-write-commands-from-a-broker)), also set:

```yaml
sis:
  adapter:
    inbound:
      amqpapi:
        enabled: true
        url: "amqp://sis:sis@rabbitmq:5672"
        address: "/queues/sis.commands"
        scopes: [sis.write]   # dev only: trust messages without a token
```

and run `make restart` after editing `config.yaml`.

The compose setup includes mellon, a test token issuer on
<https://localhost:9443> (`realm.json`). It has the machine clients `liu`
(`sis.read` + `sis.write`) and `sunet-vc` (`sis.read`), and the web UI user
`registrar` / `password`. See [Auth](#auth).

SIS listens on <https://localhost:8080> (loopback only by default; see
`SIS_PUBLISH_ADDR` in `docker-compose.yaml`).

| What | URL |
| ---- | --- |
| Web UI (admin SPA) | <https://localhost:8080/> |
| Swagger UI (`/api/v1`) | <https://localhost:8080/swagger/index.html> |
| OpenAPI spec (JSON) | <https://localhost:8080/swagger/doc.json> |
| Health check | <https://localhost:8080/healthz> |

### Development TLS

`make start` first runs `make pki`, which creates a development CA and TLS
certificates in `developer_tools/pki/` (git-ignored, kept between runs):

| File | Used by |
| ---- | ------- |
| `rootCA.crt` | Your browser and curl: trust it to accept the certificates below |
| `tls_sis.*` | SIS, for `sis`, `localhost`, `127.0.0.1` and `172.28.0.10` |
| `tls_mellon.*` | mellon, for `mellon`, `localhost`, `127.0.0.1` and `172.28.0.11` |

Compose mounts them into the containers, and `sis.httpserver.tls` points SIS
at its pair. Import `rootCA.crt` as a trusted authority in your browser (or
use `curl --cacert developer_tools/pki/rootCA.crt`). Delete
`developer_tools/pki/` to start over. These certificates are for development
only.

The web UI's sign-in only works on https pages or on `http://localhost`: the
browser offers the crypto it needs (`crypto.subtle`) only there.

## Useful endpoints

### Web UI

The built SPA is served from the directory in `sis.httpserver.web.root`
(`/web` inside the Docker image). It answers every GET that is not an
API path, so deep links work. Leave `web.root` empty to serve the APIs only.

For frontend development, run the Vite dev server instead. It serves on
<http://localhost:5173> and proxies the API paths to `https://localhost:8080`,
trusting the development CA (`NODE_EXTRA_CA_CERTS` in `web/package.json`):

```bash
make web-dev
```

### Swagger

`/swagger/index.html` documents `/api/v1`, including parse/populate. The page
itself is public; use **Authorize** with a Bearer token (see below) to send
requests from it. Disable with `sis.httpserver.swagger.enabled: false`.
Regenerate the docs after changing handler annotations:

```bash
make swagger
```

### Auth

SIS issues no tokens and stores no passwords. Callers get an access token
(a JWT) from a **token issuer** that SIS trusts, such as an OIDC provider,
and send it as `Authorization: Bearer <token>`. The token's `scope` claim
says what the caller may do. People and machine clients are treated the same.

```mermaid
sequenceDiagram
    participant C as Caller (e.g. sunet-vc, or a browser)
    participant I as Token issuer
    participant S as SIS
    C->>I: client credentials, or sign-in
    I-->>C: JWT {iss, aud, exp, scope: "sis.read"}
    C->>S: GET /eduapi/persons (Bearer JWT)
    S->>S: signed by a trusted issuer? not expired? right aud?
    S->>S: scope contains sis.read?
    S-->>C: 200, 401 or 403
```

Grant, change and revoke access **at the issuer**; SIS needs no restart.
Tokens already handed out stay valid until they expire, so keep their
lifetime short (5–15 minutes).

#### Trusted issuers

```yaml
sis:
  auth:
    issuers:
      - issuer: "https://idp.example.org/realms/sis"   # must equal the token's iss
        audience: "sis"                                # must be in the token's aud
        jwks_url: ""                                   # optional, see below
```

- SIS checks the token's signature against the issuer's public keys. It finds
  them through the issuer's discovery document, or fetches them from
  `jwks_url` if set. Use `jwks_url` when SIS reaches the issuer at another
  address than the one in `iss`, e.g. a container name.
- New signing keys are picked up automatically. An issuer that is down at
  startup does not stop SIS; its tokens get 401 until it is reachable.
- You can list several issuers. Adding or removing one needs a restart,
  because that changes who SIS trusts, not who has access.
- With `common.production: true`, every issuer must use https and set an
  `audience`. Without an audience SIS accepts every token the issuer mints,
  including tokens meant for other services.

#### Permissions: `sis.read` and `sis.write`

| Scope | Allows |
| ----- | ------ |
| `sis.read`  | `GET` on `/api/v1/*`, `POST /api/v1/parse`, everything under `/oeapi` and `/eduapi` |
| `sis.write` | `POST`, `PUT`, `DELETE` on `/api/v1/*`, `POST /api/v1/populate` |

- **The scopes are independent.** `sis.write` does not include `sis.read`.
  A token with only `sis.write` can create a record but not read it back.
  Most integrations need both.
- **The issuer decides the scopes.** SIS only reads them. Configure at the
  issuer which client or user may get which scope.
- **Errors:** a missing, invalid or expired token, or one from an untrusted
  issuer, gets `401 unauthorized`. A valid token without the needed scope
  gets `403 forbidden`, e.g. `"token lacks scope sis.write"`.

#### Example: LiU writes, SUNET vc reads

With the local test issuer from `docker compose` (mellon, `realm.json`),
`liu` gets both scopes and `sunet-vc` only `sis.read`:

```bash
MELLON=https://localhost:9443/realms/sis/protocol/openid-connect
LIU=$(curl -s --cacert developer_tools/pki/rootCA.crt -X POST $MELLON/token -u liu:liu-secret \
  -d grant_type=client_credentials | jq -r .access_token)
VC=$(curl -s --cacert developer_tools/pki/rootCA.crt -X POST $MELLON/token -u sunet-vc:sunet-vc-secret \
  -d grant_type=client_credentials | jq -r .access_token)

# LiU registers a student: 201
curl -s --cacert developer_tools/pki/rootCA.crt -X POST https://localhost:8080/api/v1/persons \
  -H "Authorization: Bearer $LIU" -H "Content-Type: application/json" \
  -d '{"sourcedId":"liu:nils.ny","givenName":"Nils","familyName":"Ny","status":"active"}'

# SUNET vc reads it through Edu-API (by sourcedId): 200
curl -s --cacert developer_tools/pki/rootCA.crt -H "Authorization: Bearer $VC" https://localhost:8080/eduapi/persons/liu:nils.ny

# SUNET vc tries to create one: 403 forbidden
curl -s --cacert developer_tools/pki/rootCA.crt -X POST https://localhost:8080/api/v1/persons \
  -H "Authorization: Bearer $VC" -H "Content-Type: application/json" \
  -d '{"givenName":"Mallory","familyName":"X","status":"active"}'
```

To see what a token grants, decode its payload:

```bash
jq -R 'split(".")[1] | gsub("-";"+") | gsub("_";"/") | @base64d | fromjson | {iss, sub, aud, scope}' <<<"$VC"
```

#### Web UI sign-in

The browser signs in at the issuer itself (authorization code flow with
PKCE) and sends the token to SIS. `GET /auth/config` tells it where:

```yaml
sis:
  adapter:
    inbound:
      authapi:
        issuer: "https://idp.example.org/realms/sis"   # must be in sis.auth.issuers
        client_id: "sis-web"                           # public client at the issuer
        scope: "openid sis.read sis.write"             # requested; the issuer decides
```

At the issuer, register `sis-web` as a public client with PKCE (S256), the
web UI's root (e.g. `https://sis.example.org/`) as redirect URI, and the web
UI's origin in `webOrigins` so the browser may call the token endpoint (CORS).
Leave `issuer` empty to turn sign-in off.

### Fake students: `file_import`

SIS can load records from JSON files at every start, e.g. the fake students
in `testdata/fake_students.json`: two universities (LiU, KTH), courses,
programmes, ten people, enrollments and results.

```yaml
sis:
  file_import:
    files:
      - testdata/fake_students.json   # relative to the config file
```

- The file has one array per record type (`organizations`,
  `academicSessions`, `courses`, `programs`, `offerings`, `persons`,
  `enrollments`, `results`) with the `/api/v1` field names. See
  `internal/fileimport` for the full format.
- Every record needs a `sourcedId`, and references (`orgId`, `personId`, ...)
  are sourcedIds. A reference must point to a record in the file or one
  already stored.
- Records are upserted by sourcedId and never expire. Each start resets them
  to the file's content and leaves other records alone.
- The whole file is checked before anything is stored. Any error stops
  startup.

### `/api/v1`: native read/write

Each resource has `GET` (list, `?limit=&offset=`), `POST`, `GET /:id`,
`PUT /:id` and `DELETE /:id`:

`persons`, `organizations`, `academic-sessions`, `courses`, `offerings`,
`enrollments`, `programs`, `results`

```bash
curl -s --cacert developer_tools/pki/rootCA.crt -H "Authorization: Bearer $TOKEN" https://localhost:8080/api/v1/persons | jq
```

Resources are addressed by canonical `id`. References between them
(`orgId`, `courseId`, …) must be canonical ids too.

### ELM import

| Method | Path | Scope | Purpose |
| ------ | ---- | ----- | ------- |
| POST | `/api/v1/parse` | read | Convert an ELM v3 JSON-LD credential to Edu-API or OEAPI; nothing is stored |
| POST | `/api/v1/populate` | write | Convert and store it, optionally with a `ttl` after which it is hidden and swept |

```bash
# Parse to Edu-API (output_format: eduapi | oeapi)
jq -n --slurpfile d internal/convert/testdata/edc_bsc.json \
  '{metadata: {input_format: "elm", output_format: "eduapi"}, data: $d[0]}' \
| curl -s --cacert developer_tools/pki/rootCA.crt -X POST https://localhost:8080/api/v1/parse \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" --data-binary @- | jq

# Populate with a 1-hour TTL (omit ttl to keep the records permanently)
jq -n --slurpfile d internal/convert/testdata/edc_bsc.json \
  '{metadata: {input_format: "elm"}, ttl: "1h", data: $d[0]}' \
| curl -s --cacert developer_tools/pki/rootCA.crt -X POST https://localhost:8080/api/v1/populate \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" --data-binary @- | jq
```

These two endpoints accept bodies up to `sis.adapter.inbound.convertapi.max_body_bytes`
(5 MiB by default). Every other endpoint is capped at 1 MiB. They also get
their own write timeout, `sis.adapter.inbound.convertapi.write_timeout` (2m by
default), instead of `sis.httpserver.write_timeout`.

### AMQP 1.0: write commands from a broker

With `sis.adapter.inbound.amqpapi.enabled`, SIS consumes one queue on an
AMQP 1.0 broker (RabbitMQ 4, Azure Service Bus, ActiveMQ Artemis, Qpid) and
runs each message as the matching `/api/v1` write. SIS creates no queues.
The broker admin creates the queue and gives it a delivery limit and a
dead-letter target. AMQP 0-9-1 clients can publish to RabbitMQ 4 too;
RabbitMQ hands their messages to SIS over 1.0.

| Message | Content |
| ------- | ------- |
| subject (0-9-1: `type`) | `person.create`, `person.update`, `person.delete`, the same for `organization`, `academicSession`, `course`, `offering`, `enrollment`, `program`, `result`, and `populate` |
| body | One data section of JSON, the `/api/v1` request body. Update carries the `id` in the body; delete is `{"id": "..."}` |
| application property `authorization` (0-9-1: header) | `Bearer <token>`, checked like an API token; needs `sis.write`. A string or UTF-8 binary value (RabbitMQ passes 0-9-1 headers over 4096 bytes on as binary) |
| `message-id`, `correlation-id` | Optional, logged |

There are no replies. The outcome is how SIS settles the message, so SIS
asks the broker to send messages unsettled. A broker that cannot do this
refuses the link, and SIS keeps retrying the connection.

| Outcome | When | Broker |
| ------- | ---- | ------ |
| accepted | done | removes it |
| rejected | the message's fault (the 4xx cases of `/api/v1`: bad body, unknown subject, missing record, conflict, no or bad token) | dead-letters it; the AMQP error describes why |
| modified, delivery failed | SIS's fault (5xx: database or token issuer down) | redelivers it, up to its delivery limit |

`max_message_bytes` caps the body (larger: rejected). SIS reads at most
64 KiB more for the rest of the message; a bigger message closes the link
before it is read, the broker hands it back, and its delivery limit
dead-letters it. Give the broker a matching publish limit (RabbitMQ:
`max_message_size`, as in `developer_tools/rabbitmq/rabbitmq.conf`) so such
messages are refused when published.

A message without `authorization` is only accepted when
`sis.adapter.inbound.amqpapi.scopes` is `[sis.write]`. That setting trusts
everyone who may publish to the queue.

Redelivery runs a command again (e.g. when the write committed but the
settlement was lost). A repeated create only gets a conflict, and is
dead-lettered, when the body carries a stable `id` or `sourcedId` and the
record still exists. Without both, SIS generates fresh identifiers on every
attempt, so a redelivered create can store a duplicate record. `message-id`
is only logged; it does not deduplicate commands. Publishers that need
exactly-once creates must send an `id` or `sourcedId`.

`make start` runs RabbitMQ (`developer_tools/rabbitmq/`, management UI on
http://localhost:15673, sis/sis; AMQP on localhost:5673). With the
`amqpapi` settings from [Quick start](#quick-start) SIS consumes
`sis.commands` without tokens (`config.yaml.sample` ships with `amqpapi`
disabled). To send a command, open the queue
`sis.commands` in the UI and use **Publish message**: set the property
`type` to `person.create` and paste a person as the payload. Rejected
messages appear in `sis.commands.dlq`.

### `/oeapi`: Open Education API v6.0 (read-only)

`GET` list (`?pageNumber=&pageSize=`) and `GET /:id` for:

`persons`, `organizations`, `academic-sessions`, `courses`, `offerings`,
`associations`, `programs`, `results`

### `/eduapi`: Edu-API v1.0 (read-only)

`GET` list (`?limit=&offset=`) and `GET /:id` for:

`persons`, `orgs`, `academicSessions`, `courses`, `classes`, `enrollments`,
`programs`, `results`

A list response carries `X-Total-Count` (stored records) and `X-Limit` (the
page size applied: `limit` capped at `sis.adapter.inbound.eduapi.max_limit`).
To read everything, add `X-Limit` to `offset` until it reaches
`X-Total-Count`. Don't count the items instead: records whose required
references are gone are left out, so a page can be short without being the
last.

Both standards APIs address resources by `sourcedId`, not by canonical id:

```bash
curl -s --cacert developer_tools/pki/rootCA.crt -H "Authorization: Bearer $TOKEN" https://localhost:8080/eduapi/courses | jq
```

## Development

```bash
make build        # ./bin/sis
make test         # go test ./...
make web          # npm install + build the SPA into web/dist
make swagger      # regenerate docs/restadmin
make docker-build # docker.sunet.se/iam_vc/sis:local
make diagram      # render docs/diagrams/*.puml
```

Run the binary directly with `SIS_CONFIG_YAML=./config.yaml ./bin/sis`. That
environment variable is the only one SIS reads; every setting lives in the
YAML file and is documented in `config.yaml.sample` and `pkg/model/config.go`.
An empty `sis.adapter.outbound.postgres.connection_string` keeps all data in
memory, which is fine for trying SIS out but is lost on restart.

The Postgres integration tests are skipped unless `SIS_TEST_DATABASE_URL`
is set.
