#!/usr/bin/env bash
# fetch-fonts.sh — Download Liberation Sans TTF files for local development.
# In Docker, these are installed via apt-get (see Dockerfile build stage).
# Usage: bash scripts/fetch-fonts.sh
set -euo pipefail

FONTS_DIR="$(dirname "$0")/../internal/pdf/fonts"
mkdir -p "$FONTS_DIR"

BASE="https://github.com/liberationfonts/liberation-fonts/raw/main/src"

echo "Downloading Liberation Sans Regular..."
curl -fsSL "$BASE/LiberationSans-Regular.ttf" -o "$FONTS_DIR/LiberationSans-Regular.ttf"

echo "Downloading Liberation Sans Bold..."
curl -fsSL "$BASE/LiberationSans-Bold.ttf" -o "$FONTS_DIR/LiberationSans-Bold.ttf"

echo "Done. Font files written to $FONTS_DIR/"
