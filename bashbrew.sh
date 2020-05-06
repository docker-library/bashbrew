#!/usr/bin/env bash
set -Eeuo pipefail

# a small shell script to help compile bashbrew

dir="$(readlink -f "$BASH_SOURCE")"
dir="$(dirname "$dir")"

export GO111MODULE=on
(
	cd "$dir"
	go build -o bin/bashbrew ./cmd/bashbrew > /dev/null
)

exec "$dir/bin/bashbrew" "$@"
