#!/bin/bash

# SPDX-FileCopyrightText: 2026 The Samoyed Authors
# SPDX-License-Identifier: GPL-2.0-or-later

set -euo pipefail

# Gets a fresh Claude Code on the web container ready to run `make all` and
# `make check` - see .claude/settings.json.
#
# Local sessions are left alone; run `make setup` yourself if you want the same
# dependencies.

if [ "${CLAUDE_CODE_REMOTE:-}" != "true" ]; then
    exit 0
fi

cd "${CLAUDE_PROJECT_DIR:-.}"

# The container's Go is older than our go.mod requires, so Go fetches a
# toolchain module instead - and the module it gets here has a trimmed
# pkg/tool, missing covdata and friends, which breaks `go test -cover`.
# Backfill whatever's missing from the container's own Go install.
backfill_go_tools() {
    local bundled active tool

    bundled="$(GOTOOLCHAIN=local go env GOTOOLDIR)"
    active="$(go env GOTOOLDIR)"

    if [ "$bundled" = "$active" ] || [ ! -d "$bundled" ] || [ ! -w "$active" ]; then
        return
    fi

    for tool in "$bundled"/*; do
        if [ ! -e "$active/$(basename "$tool")" ]; then
            echo "Backfilling $(basename "$tool") into $active" >&2
            cp "$tool" "$active/"
        fi
    done
}

./dev-setup.sh

go mod download

backfill_go_tools

# Pre-fetch the pinned linter so `make check` doesn't have to
make ./bin/golangci-lint
