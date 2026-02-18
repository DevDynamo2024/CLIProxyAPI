#!/usr/bin/env bash
set -euo pipefail

# Run Claude Code with an isolated HOME directory so ~/.claude usage/logs won't be written to your real account.
#
# Examples:
#   scripts/run_claude_sandbox.sh -- claude --dangerously-skip-permissions
#   scripts/run_claude_sandbox.sh --keep -- claude
#
# Notes:
# - By default, this seeds only ~/.claude/settings.json into the sandbox (if present).
# - If you need auth/session carried over, set CLAUDE_SANDBOX_SEED_AUTH=1 (copies ~/.claude/auth.json if present).

keep=0
if [[ "${1:-}" == "--keep" ]]; then
  keep=1
  shift
fi

if [[ "${1:-}" == "--" ]]; then
  shift
fi

cmd=( "$@" )
if [[ ${#cmd[@]} -eq 0 ]]; then
  cmd=( "claude" )
fi

base_dir="${CLAUDE_SANDBOX_BASE_DIR:-${HOME}/.claude_sandboxes}"
mkdir -p "${base_dir}"

sandbox_home="$(mktemp -d "${base_dir}/claude-home.XXXXXX")"

cleanup() {
  if [[ "${keep}" -eq 1 ]]; then
    echo "Claude sandbox HOME kept at: ${sandbox_home}" >&2
    return 0
  fi
  rm -rf "${sandbox_home}"
}
trap cleanup EXIT

mkdir -p "${sandbox_home}/.claude"

if [[ -f "${HOME}/.claude/settings.json" ]]; then
  cp -f "${HOME}/.claude/settings.json" "${sandbox_home}/.claude/settings.json"
fi

if [[ "${CLAUDE_SANDBOX_SEED_AUTH:-0}" == "1" ]]; then
  if [[ -f "${HOME}/.claude/auth.json" ]]; then
    cp -f "${HOME}/.claude/auth.json" "${sandbox_home}/.claude/auth.json"
    chmod 600 "${sandbox_home}/.claude/auth.json" || true
  fi
fi

export HOME="${sandbox_home}"
export XDG_CONFIG_HOME="${sandbox_home}/.config"
export XDG_DATA_HOME="${sandbox_home}/.local/share"
export XDG_CACHE_HOME="${sandbox_home}/.cache"

exec "${cmd[@]}"

