# ==============================================================================
# Configuration Variables
# ==============================================================================

NAME                    := sis
VERSION                 ?= local
NEWTAG                  ?= $(VERSION)
DOCKER_REGISTRY         ?= docker.sunet.se/iam_vc
DOCKER_IMAGE            ?= $(DOCKER_REGISTRY)/$(NAME)
# Lazy (=) so targets that don't need it, e.g. `make swagger` in the Docker
# build stage where there is no git or .git, never run git.
CURRENT_BRANCH          = $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null)

# ==============================================================================
# Phony Targets Declaration
# ==============================================================================

.PHONY: update gosec staticcheck vulncheck lint \
	pki start stop restart \
	vscode gh-install gh-auth \
	build test vet fmt fmt-check docker-build docker-push \
	swagger swagger-restadmin swagger-fmt swagger-check \
	web-install web-dev web-build web web-ci \
	diagram \
	release check_current_branch

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

# staticcheck and govulncheck run pinned through `go run pkg@version`, so
# local runs and CI use the same versions without an install step and their
# dependencies stay out of go.mod.
STATICCHECK_VERSION ?= v0.8.2
GOVULNCHECK_VERSION ?= v1.9.0

# Not part of lint/CI: the gosec v2.29.0 release cannot load Go 1.27
# packages. Suppress a finding with `// #nosec G<nnn> -- reason` (gosec
# ignores golangci-lint's //nolint).
gosec: ## Run gosec security scanner
	$(info Running gosec)
	gosec -color -tests ./...

staticcheck: ## Run staticcheck linter
	$(info Running staticcheck)
	go run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

vulncheck: ## Run vulnerability checker
	$(info Running vulncheck)
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) -scan package ./...

lint: fmt-check vet staticcheck vulncheck swagger-check ## Run every CI check


# ==============================================================================
# Docker Compose Operations
# ==============================================================================

pki: ## Create the development CA and TLS certificates in developer_tools/pki (keeps existing ones)
	$(info Creating development PKI)
	./developer_tools/scripts/create_pki.sh

start: pki ## Start services with docker-compose (rebuilds sis so code+SPA changes are picked up)
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
# Swagger Documentation
# ==============================================================================
# swag is pinned as a Go tool in go.mod (`go tool swag`), so the generator
# always matches the github.com/swaggo/swag library the docs package imports.
# --parseFuncBody picks up the free-standing per-entity blocks in
# restadmin/swagger.go (the generic handlers are mounted once per entity).

SWAG         := go tool swag
# The Dockerfile build stage runs `make swagger` before `go build`.
SWAGGER_OPTS := --packageName restadmin --parseInternal --parseFuncBody --quiet

swagger: swagger-fmt swagger-restadmin ## Generate all Swagger docs

swagger-fmt: ## Format Swagger annotations
	$(SWAG) fmt -d internal/adapter/inbound/restadmin,internal/adapter/inbound/convertapi -g swagger.go

swagger-restadmin: ## Generate /api/v1 Swagger docs into docs/restadmin
	$(info Generating /api/v1 Swagger docs)
	$(SWAG) init -d internal/adapter/inbound/restadmin,internal/adapter/inbound/convertapi,internal/command,internal/readmodel,internal/core/domain,internal/auth,pkg/helpers \
		-g swagger.go --output docs/restadmin $(SWAGGER_OPTS)

swagger-check: swagger ## Fail if the committed Swagger docs are stale
	@git diff --exit-code -- docs/ internal/adapter/inbound/restadmin/ \
		|| { echo "Swagger docs are stale: run 'make swagger' and commit the result"; exit 1; }

# ==============================================================================
# Diagrams
# ==============================================================================
# PlantUML sources live in docs/diagrams/*.puml; each renders to a PNG next
# to its source. plantuml is installed by 'make vscode'.

diagram: ## Generate PlantUML diagrams
	plantuml -charset UTF-8 -tpng docs/diagrams/*.puml

# ==============================================================================
# Build & Test
# ==============================================================================

build: ## Build the sis binary into ./bin
	$(info Building $(NAME))
	CGO_ENABLED=0 go build -o ./bin/$(NAME) ./cmd/sis

docker-build: ## Build the sis Docker image (override VERSION, DOCKER_IMAGE)
	$(info Docker building $(DOCKER_IMAGE):$(VERSION))
	docker build -t $(DOCKER_IMAGE):$(VERSION) .

docker-push: ## Push the sis Docker image (refuses :latest unless FORCE=true)
	@if [ "$(VERSION)" = "latest" ] && [ "$(FORCE)" != "true" ]; then \
		echo "Error: refusing to push $(DOCKER_IMAGE):$(VERSION). Set VERSION=vX.Y.Z or pass FORCE=true to override."; exit 1; \
	fi
	@echo "Pushing $(DOCKER_IMAGE):$(VERSION)"
	docker push $(DOCKER_IMAGE):$(VERSION)

# Extra `go test` flags, e.g. GOTEST_FLAGS="-race -count=1" (CI). The
# Postgres and AMQP integration tests run only when SIS_TEST_DATABASE_URL /
# SIS_TEST_AMQP_URL are set.
GOTEST_FLAGS ?=

test: ## Run all Go tests (override GOTEST_FLAGS)
	$(info Running tests)
	go test $(GOTEST_FLAGS) ./...

vet: ## Run go vet
	go vet ./...

fmt: ## Format Go code with gofumpt
	gofumpt -l -w .

fmt-check: ## Fail if any Go file is not gofmt-formatted
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then \
		echo "Not gofmt-formatted (run 'gofmt -w .'):"; echo "$$out"; exit 1; \
	fi

# ==============================================================================
# Web (React + TypeScript SPA)
# ==============================================================================

web-install: ## Install web SPA dependencies
	cd web && npm install

web-dev: pki ## Run the SPA dev server (its proxy trusts the dev CA)
	cd web && npm run dev

web-build: ## Build the SPA for production
	cd web && npm run build

web: web-install web-build ## Install SPA dependencies and build (one-shot)

web-ci: ## Install SPA dependencies from the lockfile and build (CI, as in the Dockerfile)
	cd web && npm ci && npm run build

# ==============================================================================
# Release Management
# ==============================================================================

BUMP                    ?= patch
FORCE                   ?=

check_current_branch:
	$(info Current branch: $(CURRENT_BRANCH))
ifeq ($(CURRENT_BRANCH),main)
	$(info On main branch)
else
ifneq ($(FORCE),true)
	$(error Not on main branch — use FORCE=true to override)
else
	$(warning Not on main branch — continuing because FORCE=true)
endif
endif

release: check_current_branch ## Build+push Docker image and create+push a git tag (BUMP=major|minor|patch)
	@echo "$(BUMP)" | grep -qE '^(major|minor|patch)$$' || \
		{ echo "Error: BUMP must be major, minor, or patch (got: $(BUMP))"; exit 1; }
	@if [ "$(FORCE)" != "true" ] && ! git diff --quiet HEAD 2>/dev/null; then \
		echo "Error: working tree is dirty — commit or stash changes first (use FORCE=true to override)"; exit 1; \
	fi
	@set -e; \
	git fetch --tags --quiet origin || true; \
	LATEST=$$(git tag -l "v*" --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$$' | head -n1); \
	if [ -z "$$LATEST" ]; then \
		echo "No existing version tags found, starting at v0.0.0"; \
		LATEST="v0.0.0"; \
	fi; \
	CURRENT=$$(echo "$$LATEST" | sed 's/^v//'); \
	MAJOR=$$(echo "$$CURRENT" | cut -d. -f1); \
	MINOR=$$(echo "$$CURRENT" | cut -d. -f2); \
	PATCH=$$(echo "$$CURRENT" | cut -d. -f3); \
	case "$(BUMP)" in \
		major) MAJOR=$$((MAJOR + 1)); MINOR=0; PATCH=0 ;; \
		minor) MINOR=$$((MINOR + 1)); PATCH=0 ;; \
		patch) PATCH=$$((PATCH + 1)) ;; \
	esac; \
	NEW_TAG="v$${MAJOR}.$${MINOR}.$${PATCH}"; \
	IMAGE_TAG="$(DOCKER_IMAGE):$$NEW_TAG"; \
	IMAGE_LATEST="$(DOCKER_IMAGE):latest"; \
	echo ""; \
	echo "Bumping $$LATEST -> $$NEW_TAG ($(BUMP))"; \
	echo ""; \
	echo "==> Building Docker image $$IMAGE_TAG"; \
	docker build --tag "$$IMAGE_TAG" --tag "$$IMAGE_LATEST" .; \
	echo "==> Pushing $$IMAGE_TAG"; \
	docker push "$$IMAGE_TAG"; \
	echo "==> Pushing $$IMAGE_LATEST (-> $$NEW_TAG)"; \
	docker push "$$IMAGE_LATEST"; \
	echo ""; \
	echo "==> Tagging git $$NEW_TAG"; \
	git tag -a "$$NEW_TAG" -m "Release $$NEW_TAG"; \
	git push origin "$$NEW_TAG"; \
	echo ""; \
	echo "==> Release $$NEW_TAG created and pushed"; \
	echo ""

