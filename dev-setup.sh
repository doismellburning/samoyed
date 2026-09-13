#!/bin/bash

# SPDX-FileCopyrightText: 2026 The Samoyed Authors
# SPDX-License-Identifier: GPL-2.0-or-later

set -euo pipefail

# Installs the system dependencies needed to build, test, and lint Samoyed.
#
# Safe (and cheap) to re-run. Used by CI, by agent/cloud environments, and by
# humans setting up a new machine - if you find yourself adding a dependency to
# one of those, add it here instead.
#
# Usage: ./dev-setup.sh [build|test|lint|docs|all]...
# (default: all)

APT_UPDATED=""

log() {
    echo "==> $*" >&2
}

# sudo resets the environment, so anything we want apt-get to see has to go
# through `env` on the far side of it rather than being set out here
as_root() {
    if [ "$(id -u)" -eq 0 ]; then
        "$@"
    else
        sudo "$@"
    fi
}

apt_install() {
    if [ -z "$APT_UPDATED" ]; then
        as_root apt-get update
        APT_UPDATED="yes"
    fi
    as_root env DEBIAN_FRONTEND=noninteractive apt-get install --yes --no-install-recommends "$@"
}

# pipx installs into its own bin directory, and `pipx ensurepath` only edits
# shell profiles - so a tool can be freshly installed and still not be findable
# by whatever runs next
pipx_install() {
    local package="$1"
    local tool="$2"
    local bindir

    pipx install "$package"
    pipx ensurepath

    if ! command -v "$tool" > /dev/null; then
        bindir="$(pipx environment --value PIPX_BIN_DIR 2> /dev/null || echo "$HOME/.local/bin")"
        log "WARNING: $tool is installed but not on this shell's PATH - start a new shell, or add $bindir to PATH, before running anything that needs it"
    fi
}

brew_install() {
    # `brew install` on an already-installed formula is a no-op but noisy
    for formula in "$@"; do
        brew list "$formula" > /dev/null 2>&1 || brew install "$formula"
    done
}

# The Go build uses cgo against hamlib/portaudio/etc., so these are needed even
# for `make cmds`.
install_build() {
    case "$OS" in
        Linux)
            apt_install libudev-dev libhamlib-dev portaudio19-dev libavahi-client-dev libbsd-dev libgps-dev
            ;;
        Darwin)
            brew_install hamlib portaudio
            ;;
    esac
}

# Dire Wolf provides `gen_packets`, which some tests use to check we parse what
# Dire Wolf produces; morse2ascii decodes our Morse output.
install_test() {
    case "$OS" in
        Linux)
            apt_install direwolf morse2ascii
            ;;
        Darwin)
            brew_install direwolf  # morse2ascii is not packaged for macOS; those tests skip
            ;;
    esac
}

install_lint() {
    case "$OS" in
        Linux)
            apt_install shellcheck pipx
            ;;
        Darwin)
            brew_install shellcheck pipx
            ;;
    esac

    pipx_install 'reuse[charset-normalizer]' reuse  # For `make reuse`

    # golangci-lint is pinned and installed by the Makefile, into ./bin
}

install_docs() {
    if command -v uv > /dev/null; then
        return
    fi

    case "$OS" in
        Linux)
            apt_install pipx
            pipx_install uv uv
            ;;
        Darwin)
            brew_install uv
            ;;
    esac
}

check_go() {
    if ! command -v go > /dev/null; then
        log "WARNING: go is not installed - see https://go.dev/doc/install (we don't install it here as there are too many reasonable ways to manage it)"
        return
    fi

    log "Using $(go version)"
}

OS="$(uname -s)"

case "$OS" in
    Linux)
        command -v apt-get > /dev/null || {
            echo "$0 only knows how to install dependencies on apt-based Linux distributions; see the install_* functions for the package list" >&2
            exit 1
        }
        ;;
    Darwin)
        command -v brew > /dev/null || {
            echo "$0 needs Homebrew on macOS - see https://brew.sh" >&2
            exit 1
        }
        ;;
    *)
        echo "$0 does not know how to install dependencies on $OS; see the install_* functions for the package list" >&2
        exit 1
        ;;
esac

groups=("${@:-all}")

for group in "${groups[@]}"; do
    case "$group" in
        all)
            log "Installing build dependencies"
            install_build
            log "Installing test dependencies"
            install_test
            log "Installing lint dependencies"
            install_lint
            log "Installing docs dependencies"
            install_docs
            ;;
        build | test | lint | docs)
            log "Installing $group dependencies"
            "install_$group"
            ;;
        *)
            echo "Unknown dependency group '$group' - expected one of: build, test, lint, docs, all" >&2
            exit 1
            ;;
    esac
done

check_go

log "Done"
