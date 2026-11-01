#!/usr/bin/env bash
# Creates the GitHub Release of a tag, using its CHANGELOG.md section as notes
# (falls back to GitHub's generated notes). Requires GH_TOKEN. Idempotent.
set -euo pipefail

TAG="${1:?usage: release.sh vX.Y.Z}"

notes() {
  awk -v t="$TAG" '
    index($0, "## [" t "]") == 1 { found = 1; next }
    found && /^## \[/ { exit }
    found { print }
  ' CHANGELOG.md
}

if [ "${2:-}" = "--dry-run" ]; then
  notes
  exit 0
fi

if gh release view "$TAG" >/dev/null 2>&1; then
  echo "Release $TAG already exists. Skipping."
  exit 0
fi

NOTES="$(notes)"
if [ -n "$(printf '%s' "$NOTES" | tr -d '[:space:]')" ]; then
  gh release create "$TAG" --verify-tag --title "$TAG" --notes "$NOTES"
else
  echo "No CHANGELOG section for $TAG, using generated notes."
  gh release create "$TAG" --verify-tag --title "$TAG" --generate-notes
fi
