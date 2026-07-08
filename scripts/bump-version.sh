#!/usr/bin/env bash
set -euo pipefail

if [ $# -ne 1 ]; then
  echo "Usage: $0 <new-version>"
  echo "  e.g. $0 2.1.0"
  exit 1
fi

VERSION="$1"
REPO="$(cd "$(dirname "$0")/.." && pwd)"

echo "$VERSION" > "$REPO/VERSION"

# npm (ts/package.json)
cd "$REPO/ts"
npm version "$VERSION" --no-git-tag-version --allow-same-version

# Rust (rust/Cargo.toml)
cd "$REPO/rust"
sed -i '' "s/^version = \".*\"/version = \"$VERSION\"/" Cargo.toml

# Python (python/pyproject.toml)
cd "$REPO/python"
sed -i '' "s/^version = \".*\"/version = \"$VERSION\"/" pyproject.toml

echo ""
echo "All versions bumped to $VERSION"
echo ""
echo "Changes:"
cd "$REPO"
git diff --stat
