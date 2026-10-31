#!/bin/bash

# GOWA Manager Installer Script
# Usage: curl -fsSL https://raw.githubusercontent.com/fadlee/gowa-manager/main/install.sh | bash

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
REPO="fadlee/gowa-manager" # Replace with your GitHub repo
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
BINARY_NAME="gowa-manager"

echo -e "${BLUE}🚀 GOWA Manager Installer${NC}"
echo ""

# Detect OS and architecture
OS=""
ARCH=""

case "$(uname -s)" in
    Darwin*)    OS="macos";;
    Linux*)     OS="linux";;
    CYGWIN*|MINGW*|MSYS*) OS="windows";;
    *)          echo -e "${RED}❌ Unsupported operating system: $(uname -s)${NC}"; exit 1;;
esac

case "$(uname -m)" in
    x86_64|amd64)   ARCH="x64";;
    arm64|aarch64)  ARCH="arm64";;
    *)              echo -e "${RED}❌ Unsupported architecture: $(uname -m)${NC}"; exit 1;;
esac

PLATFORM="${OS}-${ARCH}"
if [ "$OS" = "windows" ]; then
    BINARY_NAME="${BINARY_NAME}-${PLATFORM}.exe"
else
    BINARY_NAME="${BINARY_NAME}-${PLATFORM}"
fi

echo -e "${BLUE}📋 Detected platform: ${PLATFORM}${NC}"

# Get latest release
echo -e "${BLUE}🔍 Fetching latest release...${NC}"
LATEST_URL="https://api.github.com/repos/${REPO}/releases/latest"
RELEASE_DATA=$(curl -s "$LATEST_URL")

if [ $? -ne 0 ]; then
    echo -e "${RED}❌ Failed to fetch release information${NC}"
    exit 1
fi

if command -v jq >/dev/null 2>&1; then
    TAG_NAME=$(echo "$RELEASE_DATA" | jq -r '.tag_name')
else
    TAG_NAME=$(echo "$RELEASE_DATA" | grep '"tag_name":' | sed -E 's/.*"tag_name":\s*"([^"]+)",?/\1/')
fi
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${TAG_NAME}/${BINARY_NAME}"

if [ -z "$TAG_NAME" ]; then
    echo -e "${RED}❌ Could not determine latest version${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Latest version: ${TAG_NAME}${NC}"

# Create install directory
mkdir -p "$INSTALL_DIR"

# Download binary
TEMP_FILE="/tmp/${BINARY_NAME}"
echo -e "${BLUE}📥 Downloading ${BINARY_NAME}...${NC}"
echo "URL: $DOWNLOAD_URL"

if command -v curl >/dev/null 2>&1; then
    curl -L -o "$TEMP_FILE" "$DOWNLOAD_URL"
elif command -v wget >/dev/null 2>&1; then
    wget -O "$TEMP_FILE" "$DOWNLOAD_URL"
else
    echo -e "${RED}❌ Neither curl nor wget found. Please install one of them.${NC}"
    exit 1
fi

if [ $? -ne 0 ]; then
    echo -e "${RED}❌ Download failed${NC}"
    exit 1
fi

# Move to install directory and make executable
INSTALL_PATH="${INSTALL_DIR}/gowa-manager"
mv "$TEMP_FILE" "$INSTALL_PATH"
chmod +x "$INSTALL_PATH"

echo -e "${GREEN}✅ Installed to: ${INSTALL_PATH}${NC}"

# Check if install directory is in PATH
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    echo ""
    echo -e "${YELLOW}⚠️  Warning: ${INSTALL_DIR} is not in your PATH${NC}"
    echo -e "${YELLOW}   Add this to your shell profile (~/.bashrc, ~/.zshrc, etc.):${NC}"
    echo -e "${BLUE}   export PATH=\"${INSTALL_DIR}:\$PATH\"${NC}"
    echo ""
fi

# Test installation
echo -e "${BLUE}🧪 Testing installation...${NC}"
if "$INSTALL_PATH" --help >/dev/null 2>&1 || true; then
    echo -e "${GREEN}✅ Installation successful!${NC}"
else
    echo -e "${YELLOW}⚠️  Binary installed but may not work on this system${NC}"
fi

echo ""
echo -e "${GREEN}🎉 GOWA Manager has been installed!${NC}"
echo ""
echo -e "${BLUE}Usage:${NC}"
echo -e "  ${INSTALL_DIR}/gowa-manager --data-dir \"\$HOME/gowa-data\"     # Start (keep data in a fixed folder)"
echo -e "  ${INSTALL_DIR}/gowa-manager --port 8080                       # Custom port"
echo -e "  ${INSTALL_DIR}/gowa-manager --help                            # All options"
echo ""
echo -e "${BLUE}Then open: ${GREEN}http://localhost:3000${NC} ${BLUE}(or custom port)${NC}"
echo -e "${BLUE}First start: register the first account (superadmin) with the setup token printed in the output.${NC}"
echo -e "${YELLOW}Without --data-dir, data is stored in ./data relative to the current folder.${NC}"
echo ""

if [[ ":$PATH:" == *":$INSTALL_DIR:"* ]]; then
    echo -e "${GREEN}You can now run: ${BLUE}gowa-manager${NC}"
fi
