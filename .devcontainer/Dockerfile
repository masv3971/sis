FROM mcr.microsoft.com/devcontainers/base:trixie

ARG GO_VERSION=1.26.5
# Set automatically by BuildKit to the platform being built (amd64, arm64,
# ...), which matches go.dev's tarball naming. Declared without a default:
# a default here would override BuildKit's value (docker/buildx#510) and
# install amd64 Go into an ARM64 image. The legacy (non-BuildKit) builder
# leaves it empty, so the shell expansion below falls back to amd64.
ARG TARGETARCH

# Install Go from official tarball for the target architecture, so the
# toolchain can run the go install step below on ARM64 hosts too.
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH:-amd64}.tar.gz" | tar -C /usr/local -xz

ENV GOPATH=/home/vscode/go
ENV PATH=/usr/local/go/bin:${GOPATH}/bin:${PATH}

# Install common Go tools as vscode user
USER vscode
RUN go install golang.org/x/tools/gopls@latest \
    && go install github.com/go-delve/delve/cmd/dlv@latest \
    && go install honnef.co/go/tools/cmd/staticcheck@latest

USER root
