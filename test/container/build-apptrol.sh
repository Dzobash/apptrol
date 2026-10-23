#!/bin/bash
# Called only after run.sh clears the caller environment: Make's build variables
# must never become shell source or Go tool options here (QA-16, ADR 0034).
set -euo pipefail
export PATH=/usr/local/go/bin:/usr/bin:/bin GOENV=off GOTOOLCHAIN=local
version=$(git describe --tags --always 2>/dev/null || printf dev)
commit=$(git rev-parse HEAD)
date=$(date -u +%Y-%m-%dT%H:%M:%SZ)
pkg=github.com/Dzobash/apptrol
go build -trimpath -ldflags "-s -w -X $pkg/internal/version.Version=$version -X $pkg/internal/version.Commit=$commit -X $pkg/internal/version.Date=$date" \
    -o bin/apptrol ./cmd/apptrol
