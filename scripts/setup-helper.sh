#!/bin/bash

# Medha API - Environment Setup Helper
# This script helps developers choose the right setup method
# Usage: ./scripts/setup-helper.sh

echo "🧭 Medha API - Setup Helper"
echo "   Helping you choose the best setup method for your environment"
echo ""

# Detect OS
case "$(uname -s)" in
    Darwin)  OS="macos" ;;
    Linux)   OS="linux" ;;
    CYGWIN*|MINGW*|MSYS*) OS="windows" ;;
    *)       OS="unknown" ;;
esac

# Detect package manager (Linux)
if [ "$OS" = "linux" ]; then
    if command -v apt-get &> /dev/null; then
        PKG_MGR="apt (Ubuntu/Debian)"
    elif command -v dnf &> /dev/null; then
        PKG_MGR="dnf (Fedora/RHEL)"
    elif command -v yum &> /dev/null; then
        PKG_MGR="yum (CentOS/RHEL)"
    elif command -v pacman &> /dev/null; then
        PKG_MGR="pacman (Arch)"
    else
        PKG_MGR="none found"
    fi
fi

# Detect if Docker is available
if command -v docker &> /dev/null && command -v docker-compose &> /dev/null; then
    HAS_DOCKER=true
else
    HAS_DOCKER=false
fi

# Detect if Go/PostgreSQL are already installed
HAS_GO=false
HAS_PG=false
if command -v go &> /dev/null; then HAS_GO=true; fi
if command -v psql &> /dev/null; then HAS_PG=true; fi

echo "📋 Your Environment:"
echo "   Operating System: $OS"
if [ "$OS" = "linux" ]; then
    echo "   Package Manager: $PKG_MGR"
fi
echo "   Docker Available: $HAS_DOCKER"
echo "   Go Installed: $HAS_GO"
echo "   PostgreSQL Installed: $HAS_PG"
echo ""

# Recommendations
echo "💡 Recommended Setup Methods:"
echo ""

if [ "$HAS_DOCKER" = true ]; then
    echo "1. 🐳 Docker (Easiest - Works everywhere)"
    echo "   make docker-setup && make docker-dev"
    echo "   ✅ No system dependencies required"
    echo "   ✅ Consistent across all platforms"
    echo "   ✅ Isolated environment"
    echo ""
fi

if [ "$HAS_GO" = true ] && [ "$HAS_PG" = true ]; then
    echo "2. ⚡ Native (Fastest - If dependencies are ready)"
    echo "   make setup && make dev"
    echo "   ✅ Best performance"
    echo "   ✅ Full IDE integration"
    echo ""
fi

case "$OS" in
    macos)
        echo "3. 🍎 macOS Native"
        echo "   brew install go postgresql@17 postgis"
        echo "   make setup && make dev"
        echo "   ✅ Official Homebrew packages"
        echo ""
        ;;
    linux)
        echo "3. 🐧 Linux Native ($PKG_MGR)"
        case "$PKG_MGR" in
            "apt (Ubuntu/Debian)")
                echo "   sudo apt install golang-go postgresql postgis"
                ;;
            "dnf (Fedora/RHEL)")
                echo "   sudo dnf install golang postgresql postgis"
                ;;
            "yum (CentOS/RHEL)")
                echo "   sudo yum install golang postgresql postgis"
                ;;
            "pacman (Arch)")
                echo "   sudo pacman -S go postgresql postgis"
                ;;
            *)
                echo "   Install Go and PostgreSQL manually"
                ;;
        esac
        echo "   make setup && make dev"
        echo "   ✅ Uses system packages"
        echo ""
        ;;
    windows)
        echo "3. 🪟 Windows Options"
        echo "   Option A - Docker:"
        echo "   .\scripts\setup.ps1 -UseDocker"
        echo ""
        echo "   Option B - WSL (Recommended):"
        echo "   Install WSL2, then use Linux instructions above"
        echo ""
        echo "   Option C - Native:"
        echo "   .\scripts\setup.ps1"
        echo "   ⚠️  More complex on Windows"
        echo ""
        ;;
esac

echo "📖 For detailed instructions, see SETUP.md"
echo ""
echo "🔧 Quick commands:"
echo "   make docker-setup  # Docker setup"
echo "   make setup         # Native setup"
echo "   make dev           # Start development server"
echo "   make test          # Run tests"
echo ""

if [ "$HAS_DOCKER" = true ]; then
    echo "🚀 Suggested: make docker-setup && make docker-dev"
else
    echo "🚀 Suggested: make setup && make dev"
fi