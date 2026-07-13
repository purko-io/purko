#!/usr/bin/env sh
# install-skill.sh — install or update the Purko Claude Code skill into
# ~/.claude/skills/purko. Re-run any time to update to the latest published
# version (this is the "re-run your installer" path the skill's update notice
# points at).
#
#   curl -fsSL https://raw.githubusercontent.com/purko-io/purko/main/scripts/install-skill.sh | sh
#
# Overrides (env):
#   PURKO_SKILL_REF   git ref to install (default: main; use a tag to pin)
#   PURKO_SKILL_DEST  install directory (default: ~/.claude/skills/purko)
#   PURKO_SKILL_REPO  source repo (default: purko-io/purko)
set -eu

REPO="${PURKO_SKILL_REPO:-purko-io/purko}"
REF="${PURKO_SKILL_REF:-main}"
DEST="${PURKO_SKILL_DEST:-${HOME}/.claude/skills/purko}"
SKILL_SUBDIR=".claude/skills/purko"

echo "Installing the Purko skill from ${REPO}@${REF} -> ${DEST}"

# The skill's helper scripts are Python 3 stdlib-only — no pip installs.
if ! command -v python3 >/dev/null 2>&1; then
  echo "ERROR: python3 is required to run the skill (stdlib only, no pip installs)." >&2
  exit 1
fi

TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT INT TERM

URL="https://github.com/${REPO}/archive/${REF}.tar.gz"
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "${URL}" | tar -xz -C "${TMP}"
elif command -v wget >/dev/null 2>&1; then
  wget -qO- "${URL}" | tar -xz -C "${TMP}"
else
  echo "ERROR: need curl or wget to download the skill." >&2
  exit 1
fi

SRC="$(find "${TMP}" -type d -path "*/${SKILL_SUBDIR}" | head -1)"
if [ -z "${SRC}" ]; then
  echo "ERROR: ${SKILL_SUBDIR} not found in ${REPO}@${REF} (has the skill merged to that ref yet?)." >&2
  exit 1
fi

mkdir -p "$(dirname "${DEST}")"
rm -rf "${DEST}"
cp -R "${SRC}" "${DEST}"
# Never carry local caches into the install.
find "${DEST}" -name __pycache__ -type d -prune -exec rm -rf {} + 2>/dev/null || true
find "${DEST}" -name '*.pyc' -delete 2>/dev/null || true

VER="$(cat "${DEST}/VERSION" 2>/dev/null || echo '?')"
echo "Installed Purko skill v${VER}."
echo "Run /purko in Claude Code (from any directory) to open Mission Control."
