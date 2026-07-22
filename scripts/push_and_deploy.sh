#!/usr/bin/env bash

set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_NAME="${REMOTE_NAME:-origin}"
REMOTE_HOST="${REMOTE_HOST:-thinh@35.236.137.161}"
# The SSH account is `thinh`, so deploy from its checked-out repository rather
# than `/root`, which is not traversable by non-root users. Override with
# REMOTE_DIR when deploying to a different server layout.
REMOTE_DIR="${REMOTE_DIR:-/home/thinh/wordbit-advanced}"
SERVICE_NAME="${SERVICE_NAME:-backend}"
COMMIT_MESSAGE="${1:-chore: deploy backend updates}"

cd "$REPO_DIR"

BRANCH_NAME="$(git branch --show-current)"
if [[ -z "$BRANCH_NAME" ]]; then
  echo "Unable to determine current git branch." >&2
  exit 1
fi

echo "Repo: $REPO_DIR"
echo "Branch: $BRANCH_NAME"
echo "Remote: $REMOTE_NAME"
echo "Deploy target: $REMOTE_HOST:$REMOTE_DIR"

git add -A

if git diff --cached --quiet; then
  echo "No staged changes to commit. Skipping commit."
else
  git commit -m "$COMMIT_MESSAGE"
fi

git push "$REMOTE_NAME" "$BRANCH_NAME"

ssh -o StrictHostKeyChecking=accept-new "$REMOTE_HOST" \
  "set -euo pipefail; \
   cd '$REMOTE_DIR'; \
   git fetch '$REMOTE_NAME' '$BRANCH_NAME'; \
   git checkout '$BRANCH_NAME'; \
   git pull --ff-only '$REMOTE_NAME' '$BRANCH_NAME'; \
   docker compose up -d --build '$SERVICE_NAME' caddy"

echo "Push and deploy completed."
