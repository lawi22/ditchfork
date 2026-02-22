#!/usr/bin/env sh
set -e

REPO="yourusername/ditchfork"
INSTALL_DIR="/usr/local/bin"
BINARY_NAME="ditchfork"

# ── Detect OS ────────────────────────────────────────────────────────────────

OS=$(uname -s)
if [ "$OS" != "Linux" ]; then
  echo "This install script is for Linux only."
  echo "For macOS or Windows, download a binary from:"
  echo "  https://github.com/$REPO/releases/latest"
  exit 1
fi

# ── Detect architecture ───────────────────────────────────────────────────────

ARCH=$(uname -m)
case "$ARCH" in
  x86_64)          ASSET="ditchfork-linux-amd64" ;;
  aarch64 | arm64) ASSET="ditchfork-linux-arm64" ;;
  armv7l | armv6l) ASSET="ditchfork-linux-armv7" ;;
  *)
    echo "Unsupported architecture: $ARCH"
    echo "Please download a binary manually from:"
    echo "  https://github.com/$REPO/releases/latest"
    exit 1
    ;;
esac

# ── Fetch latest release tag ──────────────────────────────────────────────────

echo "Finding latest release..."
TAG=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
  | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": "\(.*\)".*/\1/')

if [ -z "$TAG" ]; then
  echo "Could not determine latest release. Check your internet connection."
  exit 1
fi

echo "Latest version: $TAG"

URL="https://github.com/$REPO/releases/download/$TAG/$ASSET"

# ── Download ──────────────────────────────────────────────────────────────────

TMP=$(mktemp)
echo "Downloading $ASSET..."
curl -fsSL "$URL" -o "$TMP"
chmod +x "$TMP"

# ── Install ───────────────────────────────────────────────────────────────────

if [ -w "$INSTALL_DIR" ]; then
  mv "$TMP" "$INSTALL_DIR/$BINARY_NAME"
else
  echo "Installing to $INSTALL_DIR (sudo required)..."
  sudo mv "$TMP" "$INSTALL_DIR/$BINARY_NAME"
fi

echo ""
echo "Ditchfork $TAG installed to $INSTALL_DIR/$BINARY_NAME"

# ── Systemd (optional) ────────────────────────────────────────────────────────

if command -v systemctl > /dev/null 2>&1; then
  echo ""
  printf "Set up Ditchfork to start automatically as a background service? [y/N] "
  read -r SETUP_SERVICE

  if [ "$SETUP_SERVICE" = "y" ] || [ "$SETUP_SERVICE" = "Y" ]; then
    DATA_DIR="/var/lib/ditchfork"
    SERVICE_FILE="/etc/systemd/system/ditchfork.service"

    # Create a dedicated user if it doesn't exist
    if ! id -u ditchfork > /dev/null 2>&1; then
      sudo useradd --system --no-create-home --shell /usr/sbin/nologin ditchfork
    fi

    # Create data directory
    sudo mkdir -p "$DATA_DIR"
    sudo chown ditchfork:ditchfork "$DATA_DIR"

    # Write service file
    sudo tee "$SERVICE_FILE" > /dev/null << EOF
[Unit]
Description=Ditchfork music blog
After=network.target

[Service]
ExecStart=$INSTALL_DIR/$BINARY_NAME
WorkingDirectory=$DATA_DIR
Restart=always
User=ditchfork
Environment=DITCHFORK_DB_PATH=$DATA_DIR/ditchfork.db
Environment=DITCHFORK_UPLOAD_DIR=$DATA_DIR/uploads

[Install]
WantedBy=multi-user.target
EOF

    sudo systemctl daemon-reload
    sudo systemctl enable --now ditchfork

    echo ""
    echo "Service started. Ditchfork is running at http://localhost:8080"
    echo "Manage it with:"
    echo "  sudo systemctl status ditchfork"
    echo "  sudo systemctl restart ditchfork"
    echo "  sudo systemctl stop ditchfork"
    echo "Data is stored in $DATA_DIR"
  fi
fi

# ── Done ──────────────────────────────────────────────────────────────────────

echo ""
echo "Run it now:  $BINARY_NAME"
echo "Then open:   http://localhost:8080"
