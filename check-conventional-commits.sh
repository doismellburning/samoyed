#!/bin/bash

# SPDX-FileCopyrightText: 2026 The Samoyed Authors
# SPDX-License-Identifier: GPL-2.0-or-later

set -euo pipefail

# Checks that commit subjects and pull request titles follow the Conventional
# Commits style described in AGENTS.md.
#
# Usage: ./check-conventional-commits.sh <subject>...
#
# Every subject given is checked, and every failure is reported, so one run
# tells you about the whole pull request rather than only its first problem.

# should-release.sh decides what to release by matching a subset of these
# against a merged branch's tip, so a type it has never heard of must not
# reach main: keep the two lists in step.
TYPES="build|chore|ci|docs|feat|fix|perf|refactor|revert|style|test"

# A scope is a bare token, so it cannot swallow the ": " that ends the header -
# without that, `feat(a: b): c` would look like a capitalised summary of "b): c".
HEADER="^($TYPES)(\\([^()[:space:]:]+\\))?!?: (.+)$"

failed=""

complain() {
    printf '%s\n  %s\n' "$1" "$2" >&2
    failed="yes"
}

check() {
    local subject="$1"
    local summary

    if [[ ! "$subject" =~ $HEADER ]]; then
        complain "Not <type>: <summary>, with type one of ${TYPES//|/, }:" "$subject"
        return
    fi

    summary="${BASH_REMATCH[3]}"

    if [[ ! "$summary" =~ ^[[:upper:]] ]]; then
        complain "Summary does not start with a capital letter:" "$subject"
    fi

    if [[ "$summary" == *. ]]; then
        complain "Summary ends in a full stop:" "$subject"
    fi
}

if [ "$#" -eq 0 ]; then
    echo "Usage: $0 <subject>..." >&2
    exit 2
fi

for subject in "$@"; do
    check "$subject"
done

if [ -n "$failed" ]; then
    echo "See https://www.conventionalcommits.org/ and the Git and PRs section of AGENTS.md" >&2
    exit 1
fi

echo "All $# subject(s) look conventional"
