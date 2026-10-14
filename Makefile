# ==============================================================================
# Configuration Variables
# ==============================================================================

NAME                    := sis
VERSION                 ?= local
NEWTAG                  ?= $(VERSION)
DOCKER_REGISTRY         ?= docker.sunet.se/iam_vc
DOCKER_IMAGE            ?= $(DOCKER_REGISTRY)/$(NAME)
CURRENT_BRANCH          := $(shell git rev-parse --abbrev-ref HEAD)

# ==============================================================================
# Phony Targets Declaration
# ==============================================================================

.PHONY: update gosec staticcheck vulncheck \
	start stop restart \
	vscode gh-install gh-auth \
	build test fmt docker-build \
	web-install web-dev web-build web

# ==============================================================================
# Go Dependency Management
# ==============================================================================

update: ## Update all Go dependencies and pinned GitHub Actions to their latest versions
	$(info Updating all Go dependencies...)
	GOFLAGS="" go get -u ./...
	GOFLAGS="" go mod tidy
	go mod vendor
	$(info Updating pinned GitHub Actions SHAs...)
	@command -v pinact >/dev/null 2>&1 || { echo "pinact not installed; run 'make install-tools' or 'make vscode'"; exit 1; }
	pinact run --update
	$(info Done — Go dependencies and GitHub Actions pins updated)


# ==============================================================================
# Code Quality & Security
# ==============================================================================

gosec: ## Run gosec security scanner
	$(info Running gosec)
	gosec -color -tests -exclude-dir=internal/gen ./...

staticcheck: ## Run staticcheck linter
	$(info Running staticcheck)
	staticcheck ./...

vulncheck: ## Run vulnerability checker
	$(info Running vulncheck)
	govulncheck -scan package ./...


# ==============================================================================
# Docker Compose Operations
# ==============================================================================

start: ## Start services with docker-compose (rebuilds sis so code+SPA changes are picked up)
	$(info Starting services)
	docker compose -f docker-compose.yaml up -d --build --remove-orphans

stop: ## Stop services
	$(info Stopping VC)
	docker compose -f docker-compose.yaml rm -s -f

restart: stop start ## Restart services

vscode: gh-install ## Set up VS Code development environment
	$(info Installing APT packages)
	sudo apt-get update && sudo apt-get install -y \
		protobuf-compiler \
		netcat-openbsd \
		plantuml \
		cmake \
		pkg-config \
		nodejs \
		npm
	$(info Installing yq)
	sudo wget https://github.com/mikefarah/yq/releases/latest/download/yq_linux_amd64 -O /usr/local/bin/yq &&\
    sudo chmod +x /usr/local/bin/yq
	$(info Installing act for local GitHub Actions testing)
	curl -sfL https://raw.githubusercontent.com/nektos/act/master/install.sh | sudo bash -s -- -b /usr/local/bin
	$(info Installing go packages)
	go install golang.org/x/tools/cmd/deadcode@latest && \
	go install github.com/securego/gosec/v2/cmd/gosec@latest && \
	go install golang.org/x/vuln/cmd/govulncheck@latest && \
	go install honnef.co/go/tools/cmd/staticcheck@latest && \
	go install mvdan.cc/gofumpt@latest
	$(info Installing web SPA dependencies)
	cd web && npm install
	$(info )
	$(info ==> vscode setup complete.)
	$(info     'make build' and 'make docker-build' now work out of the box.)

gh-install: ## Install GitHub CLI (gh)
	$(info Install GitHub CLI)
	@if ! command -v gh >/dev/null 2>&1; then \
		sudo mkdir -p -m 755 /etc/apt/keyrings && \
		curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | sudo tee /etc/apt/keyrings/githubcli-archive-keyring.gpg > /dev/null && \
		sudo chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg && \
		echo "deb [arch=$$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | sudo tee /etc/apt/sources.list.d/github-cli.list > /dev/null && \
		sudo apt-get update && \
		sudo apt-get install -y gh; \
	else \
		echo "gh already installed: $$(gh --version | head -1)"; \
	fi

gh-auth: gh-install ## Authenticate GitHub CLI
	$(info Authenticate GitHub CLI)
	@if gh auth status >/dev/null 2>&1; then \
		gh auth status; \
	else \
		gh auth login; \
	fi

# ==============================================================================
# Build & Test
# ==============================================================================

build: ## Build the sis binary into ./bin
	$(info Building $(NAME))
	CGO_ENABLED=0 go build -o ./bin/$(NAME) ./cmd/sis

docker-build: ## Build the sis Docker image (override VERSION, DOCKER_IMAGE)
	$(info Docker building $(DOCKER_IMAGE):$(VERSION))
	docker build -t $(DOCKER_IMAGE):$(VERSION) .

test: ## Run all Go tests
	$(info Running tests)
	go test ./...

fmt: ## Format Go code with gofumpt
	gofumpt -l -w .

# ==============================================================================
# Web (React + TypeScript SPA)
# ==============================================================================

web-install: ## Install web SPA dependencies
	cd web && npm install

web-dev: ## Run the SPA dev server
	cd web && npm run dev

web-build: ## Build the SPA for production
	cd web && npm run build

web: web-install web-build ## Install SPA dependencies and build (one-shot)

