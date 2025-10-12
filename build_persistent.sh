#!/bin/bash

set -e

echo "Building incident-worker with persistent pool..."

# Clean previous builds
rm -f bin/incident-worker-persistent

# Build the persistent version
go build -o bin/incident-worker-persistent ./cmd/incident-worker/main_persistent.go

echo "Build completed: bin/incident-worker-persistent"

# Make executable
chmod +x bin/incident-worker-persistent

echo "Ready to test with: ./test_persistent.sh"
