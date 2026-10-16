# ------------------------------------------------------------------------------
# Web stage: build the React SPA. Dependencies are installed from the committed
# lockfile (npm ci) so the image is reproducible; the output lands in
# /web/dist and is copied into the runtime image unchanged.
# ------------------------------------------------------------------------------
FROM node:22-alpine AS web
WORKDIR /web

# Install first against the lockfile only, so this layer is cached across
# unrelated source changes.
COPY web/package.json web/package-lock.json ./
RUN npm ci

# Build the SPA. tsc --noEmit runs as part of `npm run build` (see web/package.json).
COPY web/ ./
RUN npm run build

# ------------------------------------------------------------------------------
# Go stage: build the sis binary. Module downloads are cached in a separate
# layer so unrelated source changes do not re-download the module graph.
# ------------------------------------------------------------------------------
FROM golang:1.27-alpine AS build
WORKDIR /src

# make drives Swagger generation below, so the image uses the same recipe as
# local builds.
RUN apk add --no-cache make

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Generate the /api/v1 OpenAPI package (docs/restadmin) before building.
# internal/httpserver imports it unconditionally, so the build fails without
# it even when Swagger is disabled in config. Regenerating here (rather than
# relying on a committed copy) keeps the image in step with the annotations.
# swag is the Go tool pinned in go.mod, so no extra install is needed.
RUN make swagger && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags "-w -s" -o /out/sis ./cmd/sis

# ------------------------------------------------------------------------------
# Runtime stage: distroless + non-root. Ships the Go binary and the built SPA
# at /web, which sis serves via its httpserver (see sis.web.root in config).
# ------------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/sis /sis
COPY --from=web /web/dist /web
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/sis"]
