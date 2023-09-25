#!/usr/bin/env bash
set -Eeuo pipefail

# a small shell script to help compile bashbrew

dir="$(readlink -f "$BASH_SOURCE")"
dir="$(dirname "$dir")"

: "${CGO_ENABLED:=0}"
export GO111MODULE=on CGO_ENABLED
(
	cd "$dir"
	go build -o bin/bashbrew ./cmd/bashbrew > /dev/null
)

exec "$dir/bin/bashbrew" "$@"
