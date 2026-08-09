#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
mkdir -p "$repo_root/dist"
rm -f "$repo_root/dist/checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
  (cd "$repo_root/dist" && find . -maxdepth 1 -type f ! -name checksums.txt -exec sha256sum {} \; | sort > checksums.txt)
else
  (cd "$repo_root/dist" && find . -maxdepth 1 -type f ! -name checksums.txt -exec shasum -a 256 {} \; | sort > checksums.txt)
fi
