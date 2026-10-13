# ==============================================================================
# Configuration Variables
# ==============================================================================

NAME                    := sis
VERSION                 ?= local
NEWTAG                  ?= $(VERSION)
CURRENT_BRANCH          := $(shell git rev-parse --abbrev-ref HEAD)

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

start: ## Start services with docker-compose
	$(info Starting services)
	docker compose -f docker-compose.yaml up -d --remove-orphans

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
# Docker Build Targets
# ==============================================================================

docker-build: $(addprefix docker-build-,$(SERVICES)) ## Build all Docker images

# The Dockerfile itself fetches and builds the native libraries inside the
# builder stage (see dockerfiles/worker), so no host-side staging prereq
# fires here. Verifier picks up Longfellow + Vega via the runtime-verifier
# stage; other workers use the plain runtime stage.
define DOCKER_BUILD_WORKER_TEMPLATE
docker-build-$(1): _check-reserved-tag $$(GOBUILD_PREREQ) ## Build Docker image for $(1)
	$$(info Docker Building $(1) with tag: $$(VERSION))
	docker build --build-arg SERVICE_NAME=$(1) \
		--target $$(if $$(filter verifier,$(1)),runtime-verifier,runtime) \
		$$(if $$(filter apigw,$(1)),--build-arg BUILDTAG=$$(VERSION)) \
		--build-arg GOBUILD_IMAGE=$$(GOBUILD_IMAGE) \
		--tag $$(call docker-tag,$(1),$$(VERSION)) \
		--file dockerfiles/worker .

endef

$(foreach service,$(WORKER_SERVICES),$(eval $(call DOCKER_BUILD_WORKER_TEMPLATE,$(service))))

docker-build-gobuild: _check-reserved-tag ## Build gobuild Docker image
	$(info Docker Building gobuild with tag: $(VERSION))
	docker build --tag $(call docker-tag,gobuild,$(VERSION)) --file dockerfiles/gobuild .

docker-build-developer-tools: _check-reserved-tag ## Build developer tools Docker image
	$(info Docker Building developer tools with tag: $(VERSION))
	docker build --tag $(call docker-tag,developer-tools,$(VERSION)) \
		--file dockerfiles/developer-tools .

# ==============================================================================
# Release Management
# ==============================================================================

BUMP                    ?= patch
FORCE                   ?=

check_current_branch: ## Verify current branch is main
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

get_release-tag: ## Show current release version from latest git tag
	@git tag -l "v*" --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$$' | head -n1 || echo "v0.0.0"

#### Release target
# Creates a vX.Y.Z tag by bumping the latest existing tag.
# Usage:
#   make release                       # defaults to patch bump
#   make release BUMP=minor            # minor bump
#   make release BUMP=major            # major bump
#   make release FORCE=true            # release from any branch
#   make release BUMP=minor FORCE=true # combine options
release: check_current_branch ## Create and push a git tag (BUMP=major|minor|patch)
	@set -e; \
	echo "$(BUMP)" | grep -qE '^(major|minor|patch)$$' || \
		{ echo "Error: BUMP must be major, minor, or patch (got: $(BUMP))"; exit 1; }; \
	if [ "$(FORCE)" != "true" ] && ! git diff --quiet HEAD 2>/dev/null; then \
		echo "Error: working tree is dirty — commit or stash changes first (use FORCE=true to override)"; exit 1; \
	fi; \
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
	echo ""; \
	echo "Bumping $$LATEST -> $$NEW_TAG ($(BUMP))"; \
	echo ""; \
	echo "Building Docker images for $$NEW_TAG before creating the git tag..."; \
	echo ""; \
	$(MAKE) docker-build VERSION=$$NEW_TAG _RELEASE_MODE=1; \
	echo ""; \
	echo "==> Docker images built for $$NEW_TAG. Creating and pushing git tag..."; \
	echo ""; \
	git tag -a "$$NEW_TAG" -m "Release $$NEW_TAG"; \
	git push origin "$$NEW_TAG"; \
	echo ""; \
	echo "==> Release $$NEW_TAG created and pushed"; \
	echo ""; \
	echo "Pushing Docker images for $$NEW_TAG..."; \
	echo ""; \
	$(MAKE) docker-push VERSION=$$NEW_TAG _RELEASE_MODE=1; \
	$(MAKE) docker-tag VERSION=$$NEW_TAG NEWTAG=dev _RELEASE_MODE=1; \
	$(MAKE) docker-push VERSION=dev _RELEASE_MODE=1; \
	echo ""; \
	echo "==> Docker images built and pushed for $$NEW_TAG (:dev)"; \
	echo ""; \
	echo "Requesting Go module proxy indexing for $$NEW_TAG..."; \
	for i in 5 4 3 2 1; do printf "\r    Waiting for tag to propagate... $$i "; sleep 1; done; printf "\r                                       \r"; \
	curl -sf "https://proxy.golang.org/github.com/SUNET/vc/@v/$${NEW_TAG}.info" > /dev/null && \
		echo "==> Go module proxy indexed $$NEW_TAG" || \
		echo "==> Warning: Go module proxy indexing request failed (will be indexed on first fetch)"; \
	echo ""

#### Prod promotion
# Pulls the semver-tagged images, re-tags them as `:latest`, pushes.
# No rebuild.
# Usage:
#   make release-prod              # promotes latest vX.Y.Z semver tag to :latest
#   make release-prod TAG=v1.2.3   # promotes v1.2.3 to :latest
release-prod: ## Promote a release tag to prod
	@$(MAKE) --no-print-directory _promote NEWTAG=latest TAG="$(TAG)"

#### Demo promotion
# Same as release-prod but re-tags to `:demo`. No rebuild.
# Usage:
#   make release-demo              # promotes latest vX.Y.Z semver tag to :demo
#   make release-demo TAG=v1.2.3   # promotes v1.2.3 to :demo
release-demo: ## Promote a release tag to demo
	@$(MAKE) --no-print-directory _promote NEWTAG=demo TAG="$(TAG)"

_promote:
	@set -e; \
	if [ -n "$(TAG)" ]; then \
		SRC_TAG=$$(echo "$(TAG)" | sed 's#^refs/tags/##'); \
	else \
		SRC_TAG=$$(git tag -l "v*" --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$$' | head -n1); \
		if [ -z "$$SRC_TAG" ]; then \
			echo "Error: no version tags found. Run 'make release' first."; exit 1; \
		fi; \
	fi; \
	echo "$$SRC_TAG" | grep -qE '^v[0-9]+\.[0-9]+\.[0-9]+$$' || \
		{ echo "Error: TAG must match vX.Y.Z (got: $$SRC_TAG)"; exit 1; }; \
	echo ""; \
	echo "Promoting $$SRC_TAG -> $(NEWTAG)"; \
	echo ""; \
	$(MAKE) docker-pull VERSION=$$SRC_TAG _RELEASE_MODE=1 && \
	$(MAKE) docker-tag VERSION=$$SRC_TAG NEWTAG=$(NEWTAG) _RELEASE_MODE=1 && \
	$(MAKE) docker-push VERSION=$(NEWTAG) _RELEASE_MODE=1; \
	echo ""; \
	echo "==> Promotion complete for $$SRC_TAG (:$(NEWTAG))"; \
	echo ""
