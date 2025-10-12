#!/bin/bash

set -e

echo "Building incident-worker with optimized pool..."

# Clean previous builds
rm -f bin/incident-worker-optimized

# Build the optimized version
go build -o bin/incident-worker-optimized ./cmd/incident-worker/main_optimized.go

echo "Build completed: bin/incident-worker-optimized"

# Make executable
chmod +x bin/incident-worker-optimized

echo "Ready to test with: ./test_optimized.sh"
