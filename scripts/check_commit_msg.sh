#!/usr/bin/env bash
# Validates a single commit message (first line) against Conventional Commits.
# Single source of truth for the rule -- used by scripts/hooks/commit-msg
# (local pre-commit enforcement) and .github/workflows/commitlint.yml (CI).
#
# Usage: check_commit_msg.sh "<full commit message>"
set -euo pipefail

msg="$1"
first_line=$(printf '%s\n' "$msg" | head -1)

pattern='^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([a-z0-9./_-]+\))?!?: .+'

if [[ ! "$first_line" =~ $pattern ]]; then
  echo "commit message does not follow Conventional Commits:"
  echo "  $first_line"
  echo
  echo "expected: <type>(<optional scope>): <description>"
  echo "  types: feat fix docs style refactor perf test build ci chore revert"
  echo "  example: feat(myaccount/s3): add bucket versioning example"
  exit 1
fi

echo "ok: $first_line"
