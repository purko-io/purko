#!/usr/bin/env bash
# Mirror the canonical Claude Code skill into the cross-agent .agents/ tree.
#
#   canonical:  .claude/skills/purko/   (owned; edit here)
#   mirror:     .agents/skills/purko/   (generated; never edit by hand)
#
# The two must stay byte-identical. CI runs `--check` and fails the PR if the
# mirror drifts; run this script (no args) to regenerate it after editing the
# canonical copy. A single source of truth avoids the two-copies-drift trap.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CANON="${ROOT}/.claude/skills/purko"
MIRROR="${ROOT}/.agents/skills/purko"

if [[ ! -d "${CANON}" ]]; then
  echo "ERROR: canonical skill not found at ${CANON}" >&2
  exit 1
fi

if [[ "${1:-}" == "--check" ]]; then
  if [[ ! -d "${MIRROR}" ]] || ! diff -r "${CANON}" "${MIRROR}" >/dev/null 2>&1; then
    echo "ERROR: .agents/skills/purko is out of sync with the canonical .claude/skills/purko." >&2
    echo "Fix: run scripts/sync-skill-mirror.sh and commit the result." >&2
    diff -r "${CANON}" "${MIRROR}" 2>&1 | head -40 || true
    exit 1
  fi
  echo "OK: skill mirror is in sync with the canonical copy."
  exit 0
fi

rm -rf "${MIRROR}"
mkdir -p "$(dirname "${MIRROR}")"
cp -R "${CANON}" "${MIRROR}"
echo "Synced ${CANON#${ROOT}/} -> ${MIRROR#${ROOT}/}"
