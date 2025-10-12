#!/bin/bash

set -e

echo "Building pooled incident-worker..."

# Build the pooled version
echo "Compiling pooled version..."
go build -o bin/incident-worker-pooled cmd/incident-worker/main_pooled.go

echo "Build completed: bin/incident-worker-pooled"
echo ""
echo "Usage:"
echo "  export Q_BIN=q"
echo "  export QPROXY_POOL_SIZE=3"
echo "  export QPROXY_CONV_ROOT=./conversations"
echo "  export QPROXY_HTTP_ADDR=:8080"
echo "  ./bin/incident-worker-pooled"
