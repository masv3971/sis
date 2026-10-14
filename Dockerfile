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

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-w -s" -o /out/sis ./cmd/sis

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
