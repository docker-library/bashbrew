#!/usr/bin/env bash
set -Eeuo pipefail

# docker run -dit --name registry --restart always -p 5000:5000 registry

arches=( amd64 arm32v5 arm32v6 arm32v7 arm64v8 i386 ppc64le s390x )
image='busybox:latest'
target='localhost:5000'

BASHBREW_ARCH_NAMESPACES=
for arch in "${arches[@]}"; do
	docker image inspect "$arch/$image" &> /dev/null || docker pull "$arch/$image"
	docker tag "$arch/$image" "$target/$arch/$image"
	docker push "$target/$arch/$image"
	[ -z "$BASHBREW_ARCH_NAMESPACES" ] || BASHBREW_ARCH_NAMESPACES+=', '
	BASHBREW_ARCH_NAMESPACES+="$arch = $target/$arch"
done
export BASHBREW_ARCH_NAMESPACES

./put-multiarch.sh --dry-run --insecure "$target/library/$image"

exec ./put-multiarch.sh --insecure "$target/library/$image"
