#!/usr/bin/env bash
set -Eeuo pipefail

jq --arg dpkgSmokeTest '[ "$(dpkg --print-architecture)" = "amd64" ]' '
	.matrix.include += [
		.matrix.include[]
		| select(.name | test(" [(].+[)]") | not) # ignore any existing munged builds
		| select(.os | startswith("windows-") | not)
		| .name += " (i386)"
		| .meta.froms as $froms
		| .runs.pull = ([
			"# pull i386 variants of base images for multi-architecture testing",
			$dpkgSmokeTest,
			(
				$froms[]
				| ("i386/" + . | @sh) as $i386
				| (
					"docker pull " + $i386,
					"docker tag " + $i386 + " " + @sh
				)
			)
		] | join("\n"))
		# adjust "docker buildx build" lines to include appropriate "--build-context" flags (https://github.com/docker/buildx/pull/1886)
		| .runs.build |= gsub("docker buildx build "; "docker buildx build " + ($froms | unique | map(@sh "--build-context \(.)=docker-image://\("i386/" + .)") | join(" ")) + " ")
	]
' "$@"
