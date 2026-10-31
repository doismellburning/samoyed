#!/bin/bash

# SPDX-FileCopyrightText: 2026 The Samoyed Authors
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

# Designed to be compatible with Debian
# https://www.debian.org/doc/debian-policy/ch-controlfields.html
# "The upstream_version must contain only alphanumerics and the characters . + - ~ (full stop, plus, hyphen, tilde) and should start with a digit."

# Year.Month.Day.EpochSeconds-Commit
#
# Each part is worked out on its own, so that set -e stops the script, with
# nothing printed, when one fails - outside a git checkout, say - rather than
# printing a version with a part missing.
date=$(date -u +%Y.%m.%d.%s)
commit=$(git rev-parse --short HEAD)
echo "$date-$commit"
