#!/usr/bin/env bash
set -Eeuo pipefail

bashbrew="$(which bashbrew)"
bashbrewLibrary="${BASHBREW_LIBRARY:-$HOME/docker/official-images/library}"
[ -n "$BASHBREW_ARCH_NAMESPACES" ]

dockerConfig="${DOCKER_CONFIG:-$HOME/.docker}"
[ -s "$dockerConfig/config.json" ]

args=(
	-v "$bashbrew":/usr/local/bin/bashbrew:ro
	-v "$bashbrewLibrary":/library:ro
	-e BASHBREW_LIBRARY=/library
	-e BASHBREW_ARCH_NAMESPACES

	-v "$dockerConfig":/.docker:ro
	-e DOCKER_CONFIG='/.docker'

	-e DOCKERHUB_PUBLIC_PROXY

	#-e MOJO_CLIENT_DEBUG=1
	#-e MOJO_IOLOOP_DEBUG=1

	# localhost!
	--network host

	# no signal handlers 😅
	--init
)

if [ -t 0 ] && [ -t 1 ]; then
	args+=( -it )
fi

dir="$(dirname "$BASH_SOURCE")"
img="$(docker build -q -t oi/put-multiarch --cache-from oi/put-multiarch "$dir")"

#exec docker run --rm "${args[@]}" "$img" perl -MCarp::Always bin/put-multiarch.pl "$@"
exec docker run --rm "${args[@]}" "$img" bin/put-multiarch.pl "$@"
