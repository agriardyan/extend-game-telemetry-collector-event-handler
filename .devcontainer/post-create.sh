#!/bin/bash
set -e

echo "🚀 Setting up development environment..."

# Install Extend Helper CLI
EXTEND_HELPER_CLI_VERSION="0.0.9"
echo "📥 Installing Extend Helper CLI..."

GOARCH_SUFFIX=$(case "$(uname -m)" in
    x86_64) echo "amd64" ;;
    aarch64) echo "arm64" ;;
    *) echo "amd64" ;;
esac)

OS_SUFFIX=$(case "${HOST_OS:-$(uname -s)}" in
    Linux) echo "linux" ;;
    Darwin) echo "darwin" ;;
    CYGWIN*|MINGW*|MSYS*) echo "windows" ;;
    *) echo "linux" ;;
esac)

if [ ! -f /usr/local/bin/extend-helper-cli ]; then
    sudo wget -O /usr/local/bin/extend-helper-cli https://github.com/AccelByte/extend-helper-cli/releases/download/v${EXTEND_HELPER_CLI_VERSION}/extend-helper-cli-${OS_SUFFIX}_${GOARCH_SUFFIX}
    sudo chmod +x /usr/local/bin/extend-helper-cli
    echo "✅ Extend Helper CLI installed"
else
    echo "✅ Extend Helper CLI already installed"
fi

# Install Go dependencies
echo "📦 Installing Go dependencies..."
go mod download
go mod tidy

# Make scripts executable
echo "🔧 Setting up scripts..."
chmod +x proto.sh

# Generate protobuf files
echo "✏️ Generating protocol buffer files..."
if command -v protoc &> /dev/null; then
    if [ -d "pkg/proto" ]; then
        ./proto.sh || echo "⚠️  Protocol buffer generation skipped"
    else
        echo "⚠️  Proto directory not found, skipping generation"
    fi
else
    echo "⚠️  protoc not found"
fi

# Configure git for safe directory
if [ -d ".git" ]; then
    echo "🔧 Setting up git..."
    git config --global --add safe.directory /workspace
fi

echo "✅ Development environment setup complete!"
echo ""
echo "🎯 Quick start commands:"
echo "  • Run Go service: set -a && source .env && set +a && go run main.go"
echo "  • Build Go gateway: cd gateway && go build"
echo "  • Generate protobuf: ./proto.sh"
echo ""
echo "🛟 Ports:"
echo "  • gRPC Server: 6565"
echo "  • gRPC Gateway: 8000"
echo "  • Prometheus Metrics: 8080"
