#!/bin/bash

# SPDX-FileCopyrightText: 2026 The Samoyed Authors
# SPDX-License-Identifier: GPL-2.0-or-later

set -euo pipefail

# Determines whether the current HEAD commit warrants a release.
# For merge commits, inspects the tip of the merged branch.
# For regular commits, inspects the commit message directly.
# Prints "true" if a release should be made, "false" if it should be skipped.
# Exits non-zero on unexpected errors.

PROCEDURAL_PATTERN="^(build|chore|ci|docs|refactor|style|test)[:(]"

is_procedural() {
    grep -qE "$PROCEDURAL_PATTERN" <<< "$1"
}

if git rev-parse --verify HEAD^2 >/dev/null 2>&1; then
    # Merge commit: inspect the tip of the merged branch
    subject=$(git show --no-patch --format=%s HEAD^2)
else
    # Regular commit: inspect this commit's subject
    subject=$(git show --no-patch --format=%s HEAD)
fi

echo "Checking: $subject" >&2

if is_procedural "$subject"; then
    echo "Procedural - skipping release" >&2
    echo "false"
else
    echo "Non-procedural - release warranted" >&2
    echo "true"
fi
