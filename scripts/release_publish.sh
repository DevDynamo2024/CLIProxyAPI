#!/usr/bin/env bash

# Release helper for publishing backend/frontend tags that trigger GitHub Actions releases.
# This is intended to be run manually when you want installers to pick up the latest release.

set -euo pipefail

BACKEND_DIR="${BACKEND_DIR:-/Users/taylor/code/tools/CLIProxyAPI}"
FRONTEND_DIR="${FRONTEND_DIR:-/Users/taylor/code/tools/Cli-Proxy-API-Management-Center}"

BACKEND_REMOTE="${BACKEND_REMOTE:-origin}"
FRONTEND_REMOTE="${FRONTEND_REMOTE:-origin}"

MODE="${1:-all}" # all|backend|frontend

die() {
  echo "[ERROR] $*" >&2
  exit 1
}

require_clean_tree() {
  local dir="$1"
  local name="$2"
  if [[ -n "$(git -C "$dir" status --porcelain)" ]]; then
    die "$name repo has uncommitted changes: $dir"
  fi
}

latest_tag() {
  local dir="$1"
  local pattern="$2"
  git -C "$dir" tag --list "$pattern" --sort=v:refname | tail -n 1
}

bump_patch() {
  local tag="$1"
  tag="${tag#v}"
  IFS='.' read -r major minor patch <<<"$tag"
  [[ -n "${major:-}" && -n "${minor:-}" && -n "${patch:-}" ]] || die "unexpected tag format: $1"
  patch=$((patch + 1))
  echo "v${major}.${minor}.${patch}"
}

push_branch() {
  local dir="$1"
  local remote="$2"
  git -C "$dir" push "$remote"
}

tag_and_push() {
  local dir="$1"
  local remote="$2"
  local tag="$3"

  if git -C "$dir" rev-parse "$tag" >/dev/null 2>&1; then
    die "tag already exists in repo: $tag ($dir)"
  fi

  git -C "$dir" tag -a "$tag" -m "$tag"
  git -C "$dir" push "$remote" "$tag"
}

release_backend() {
  require_clean_tree "$BACKEND_DIR" "backend"
  push_branch "$BACKEND_DIR" "$BACKEND_REMOTE"

  local prev
  prev="$(latest_tag "$BACKEND_DIR" "v6.*")"
  [[ -n "$prev" ]] || die "no existing backend tags found (pattern v6.*)"

  local next
  next="$(bump_patch "$prev")"

  echo "[INFO] backend: $prev -> $next"
  tag_and_push "$BACKEND_DIR" "$BACKEND_REMOTE" "$next"
}

release_frontend() {
  require_clean_tree "$FRONTEND_DIR" "frontend"
  push_branch "$FRONTEND_DIR" "$FRONTEND_REMOTE"

  local prev
  prev="$(latest_tag "$FRONTEND_DIR" "v1.*")"
  [[ -n "$prev" ]] || die "no existing frontend tags found (pattern v1.*)"

  local next
  next="$(bump_patch "$prev")"

  echo "[INFO] frontend: $prev -> $next"
  tag_and_push "$FRONTEND_DIR" "$FRONTEND_REMOTE" "$next"
}

case "$MODE" in
  all)
    release_backend
    release_frontend
    ;;
  backend)
    release_backend
    ;;
  frontend)
    release_frontend
    ;;
  *)
    die "usage: $0 [all|backend|frontend]"
    ;;
esac

echo "[SUCCESS] tag(s) pushed; GitHub Actions should create releases shortly."

