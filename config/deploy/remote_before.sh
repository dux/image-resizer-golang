#!/usr/bin/env bash
# remote_before.sh - runs on the SERVER in <app_dir>/new-release/, before the swap.
# Non-zero exit aborts the deploy and release/ keeps serving.
#
# govips links libvips through cgo, so the binary is built here against the box's
# libvips-dev. Go itself comes from ./mise.toml (lux-deploy has run `mise install`).
set -euo pipefail

CGO_ENABLED=1 mise exec -- go build -o bin/server ./app
