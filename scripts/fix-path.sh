#!/bin/bash

# This script restores the full system PATH to access package managers and system tools

# Save the original restricted PATH
echo "Original PATH: $PATH"
echo ""

# Reset to system defaults by unsetting the custom paths and using standard locations
export PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

# Add Go binaries if they exist
if [ -d "$HOME/go/bin" ]; then
    export PATH="$PATH:$HOME/go/bin"
fi

# Add any .local/bin
if [ -d "$HOME/.local/bin" ]; then
    export PATH="$PATH:$HOME/.local/bin"
fi

echo "Fixed PATH: $PATH"
echo ""
echo "✅ Your PATH has been restored. You can now:"
echo "  1. Run 'make setup' to install dependencies"
echo "  2. Or manually run: sudo dnf install golang postgresql postgresql-contrib postgis openssl"
