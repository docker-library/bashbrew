#!/bin/sh
set -eu

# usage: (from within another script)
#   shell="$(./bashbrew-arch-to-goenv.sh arm32v6)"
#   eval "$shell"
# since we need those new environment variables set in our other script

bashbrewArch="$1"; shift # "amd64", "arm32v5", "windows-amd64", etc.

os="${bashbrewArch%%-*}"
[ "$os" != "$bashbrewArch" ] || os='linux'
printf 'export GOOS="%s"\n' "$os"

arch="${bashbrewArch#${os}-}"
case "$arch" in
	arm32v*)
		printf 'export GOARCH="%s"\n' 'arm'
		printf 'export GOARM="%s"\n' "${arch#arm32v}"
		;;

	arm64v*)
		printf 'export GOARCH="%s"\n' 'arm64'
		# no GOARM(64) for arm64 (yet?):
		#   https://github.com/golang/go/blob/be0e0b06ac53d3d02ea83b479790404057b6f19b/src/internal/buildcfg/cfg.go#L86
		#   https://github.com/golang/go/issues/60905
		#printf 'export GOARM64="v%s"\n' "${arch#arm64v}"
		printf 'unset GOARM\n'
		;;

	i386)
		printf 'export GOARCH="%s"\n' '386'
		printf 'unset GOARM\n'
		;;

	# TODO GOAMD64: https://github.com/golang/go/blob/be0e0b06ac53d3d02ea83b479790404057b6f19b/src/internal/buildcfg/cfg.go#L57-L70 (v1 implied)

	*)
		printf 'export GOARCH="%s"\n' "$arch"
		printf 'unset GOARM\n'
		;;
esac
