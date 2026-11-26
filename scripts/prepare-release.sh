#!/usr/bin/env bash
# Run from a clean Release checkout with a supported Go 1.27 patch installed.
# Updates the actual Go module graph and checksums; does not commit or publish.
set -euo pipefail
cd "$(dirname "$0")/.."
if [[ ! -f go.mod ]] || ! grep -qx 'module github.com/MrAmperage/ReportBoxBackendCore' go.mod; then
    echo 'Run in ReportBoxBackendCore' >&2
    exit 1
fi
# Known advisory floors, not an exhaustive claim of security. MVS may select
# newer transitive versions. Do not use hand-written checksums or disable sumdb.
go get go@1.27.4
go get \
    go.opentelemetry.io/otel@v1.45.0 \
    go.opentelemetry.io/otel/trace@v1.45.0 \
    go.opentelemetry.io/otel/sdk@v1.45.0 \
    go.opentelemetry.io/otel/exporters/otlp/otlptrace@v1.45.0 \
    google.golang.org/grpc@v1.85.0 \
    golang.org/x/net@v0.62.0
go mod tidy
go mod verify
go list -m all
