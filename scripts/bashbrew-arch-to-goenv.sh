#!/usr/bin/env bash
set -Eeuo pipefail

# usage: (from within another script)
#   shell="$(./bashbrew-arch-to-goenv.sh arm32v6)"
#   eval "$shell"
# since we need those new environment variables set in our other script

bashbrewArch="$1"; shift # "amd64", "arm32v5", "windows-amd64", etc.

os="${bashbrewArch%%-*}"
[ "$os" != "$bashbrewArch" ] || os='linux'
arch="${bashbrewArch#${os}-}"

declare -A envs=(
	# https://pkg.go.dev/cmd/go#hdr-Build_constraints
	# https://gist.github.com/asukakenji/f15ba7e588ac42795f421b48b8aede63

	[GOOS]="$os"
	[GOARCH]="$arch"

	# https://go.dev/wiki/MinimumRequirements#architectures
	[GO386]=
	[GOAMD64]=
	[GOARM64]=
	[GOARM]=
	[GOMIPS64]=
	[GOPPC64]=
	[GORISCV64]=
)

case "$arch" in
	amd64)
		envs[GOAMD64]='v1'
		;;

	arm32v*)
		envs[GOARCH]='arm'
		envs[GOARM]="${arch#arm32v}" # "6", "7", "8", etc
		;;

	arm64v*)
		envs[GOARCH]='arm64'
		version="${arch#arm64v}"
		if [ -z "${version%%[0-9]}" ]; then
			# if the version is just a raw number ("8", "9"), we should append ".0"
			# https://go-review.googlesource.com/c/go/+/559555/comment/e2049987_1bc3a065/
			# (Go has "v8.0" but no bare "v8")
			version+='.0'
		fi
		envs[GOARM64]="v$version" # "v8.0", "v9.0", etc
		;;

	i386)
		envs[GOARCH]='386'
		;;

	# TODO GOMIPS64?
	# TODO GOPPC64?
	# TODO GORISCV64?
esac

exports=
unsets=
for key in "${!envs[@]}"; do
	val="${envs[$key]}"
	if [ -n "$val" ]; then
		exports+=" $(printf '%s=%q' "$key" "$val")"
	else
		unsets+=" $key"
	fi
done
[ -z "$exports" ] || printf 'export%s\n' "$exports"
[ -z "$unsets" ] || printf 'unset%s\n' "$unsets"
