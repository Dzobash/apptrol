# Apptrol — developer shortcuts. CI runs the same checks (see .github/workflows/ci.yml).

BINARY  := apptrol
PKG     := github.com/Dzobash/apptrol
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse HEAD 2>/dev/null)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Commit=$(COMMIT) \
	-X $(PKG)/internal/version.Date=$(DATE)

# Minimum total test coverage in percent; keep in sync with COVERAGE_MIN in ci.yml.
COVERAGE_MIN ?= 75

# go-licenses lists the libraries compiled into the binary with their licenses (#33).
GO_LICENSES := github.com/google/go-licenses/v2@v2.0.1

.PHONY: all check build test test-audio test-desktop test-launcher cover vet fmt lint vulncheck third-party-licenses snapshot release-check clean help

all: check build ## Run all checks, then build

check: fmt vet lint test ## Everything CI checks, except the vulnerability scan

build: ## Build bin/apptrol
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/apptrol

test: ## Run tests with the race detector
	go test -race ./...

test-audio: ## Run the audio integration tests against your running PipeWire/PulseAudio
	APPTROL_PULSE_TEST=1 go test -race -count=1 -run Integration -v ./internal/audio/pulse

test-desktop: ## Run the D-Bus integration tests in a private session bus (not your desktop's)
	dbus-run-session -- env APPTROL_DBUS_TEST=1 go test -race -count=1 -run Integration -v ./internal/desktop ./internal/power

test-launcher: ## Start harmless test units (`true`) in your systemd user manager, to check starting apps
	APPTROL_SYSTEMD_TEST=1 go test -race -count=1 -run Integration -v ./internal/launcher

cover: ## Run tests with coverage, enforce COVERAGE_MIN, write coverage.html
	go test -race -covermode=atomic -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@total=$$(go tool cover -func=coverage.out | awk '/^total:/ {sub("%","",$$3); print $$3}'); \
	echo "Total coverage: $$total% (minimum $(COVERAGE_MIN)%) - details in coverage.html"; \
	awk -v t="$$total" -v m="$(COVERAGE_MIN)" 'BEGIN { exit (t+0 < m+0) }' || { echo "Coverage below minimum"; exit 1; }

vet: ## Run go vet
	go vet ./...

fmt: ## Check formatting (fails if files need gofmt)
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "Needs gofmt:"; echo "$$out"; exit 1; fi

lint: ## Run golangci-lint (install: https://golangci-lint.run/welcome/install/)
	golangci-lint run ./...

vulncheck: ## Check dependencies for known vulnerabilities
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

third-party-licenses: ## Write THIRD_PARTY_LICENSES: every bundled library's license, and Go's; fail on an unknown or restricted license
	go run $(GO_LICENSES) check ./cmd/apptrol --ignore $(PKG) --disallowed_types=forbidden,restricted,unknown
	go run $(GO_LICENSES) report ./cmd/apptrol --ignore $(PKG) --template packaging/third-party-licenses.tpl > THIRD_PARTY_LICENSES
	@# Apache-2.0 libraries' NOTICE files must ship too (e.g. go-systemd's).
	@tmp=$$(mktemp -d) && trap 'rm -rf "$$tmp"' EXIT && \
		go run $(GO_LICENSES) save ./cmd/apptrol --ignore $(PKG) --save_path="$$tmp/l" 2>/dev/null && \
		find "$$tmp/l" -iname 'NOTICE*' | sort | while read -r f; do \
			printf -- '\n%s\n%s\n%s\n\n' \
				'--------------------------------------------------------------------------------' \
				"NOTICE of $$(dirname "$${f#$$tmp/l/}")" \
				'--------------------------------------------------------------------------------'; \
			cat "$$f"; \
		done >> THIRD_PARTY_LICENSES
	@{ printf -- '\n%s\n%s\n%s\n%s\n%s\n\n' \
		'--------------------------------------------------------------------------------' \
		"The Go standard library and runtime $$(go env GOVERSION)" \
		'License: BSD-3-Clause' \
		'Source:  https://go.dev/LICENSE' \
		'--------------------------------------------------------------------------------'; \
		cat packaging/licenses/go.LICENSE; } >> THIRD_PARTY_LICENSES
	@echo "THIRD_PARTY_LICENSES: $$(grep -c '^License:' THIRD_PARTY_LICENSES) components"

snapshot: ## Trial release build into dist/, publishes nothing (needs goreleaser)
	goreleaser release --snapshot --clean

release-check: ## Validate .goreleaser.yaml (needs goreleaser)
	goreleaser check

clean: ## Remove build and coverage output
	rm -rf bin dist coverage.out coverage.html THIRD_PARTY_LICENSES

help: ## List targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-14s %s\n", $$1, $$2}'
