#!/bin/bash

# SPDX-FileCopyrightText: 2026 The Samoyed Authors
# SPDX-License-Identifier: GPL-2.0-or-later

set -euo pipefail

# Designed to be compatible with Debian
# https://www.debian.org/doc/debian-policy/ch-controlfields.html
# "The upstream_version must contain only alphanumerics and the characters . + - ~ (full stop, plus, hyphen, tilde) and should start with a digit."

# Year.Month.Day.EpochSeconds-Commit
echo "$(date --utc +%Y.%m.%d.%s)"-"$(git rev-parse --short HEAD)"
