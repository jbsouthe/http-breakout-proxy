#!/bin/sh

# at repo root
APP=http-breakout-proxy
VERSION=v0.1.0
DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
LD="-s -w -X main.version=$VERSION -X main.buildDate=$DATE"

set -x
# macOS (Apple Silicon)
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "$LD" -o dist/$APP-$VERSION-darwin-arm64 ./src

# macOS (Intel)
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$LD" -o dist/$APP-$VERSION-darwin-amd64 ./src

# Linux (x86_64)
GOOS=linux  GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$LD" -o dist/$APP-$VERSION-linux-amd64 ./src

# Windows (x86_64)
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$LD" -o dist/$APP-$VERSION-windows-amd64.exe ./src
