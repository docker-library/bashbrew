#!/usr/bin/env bash
set -Eeuo pipefail

found() {
	echo "$@"
	exit
}

arch=
if command -v apk > /dev/null && tryArch="$(apk --print-arch)"; then
	arch="$tryArch"
elif command -v dpkg > /dev/null && tryArch="$(dpkg --print-architecture)"; then
	arch="${tryArch##*-}"
elif command -v rpm > /dev/null && tryArch="$(rpm --query --queryformat='%{ARCH}' rpm)"; then
	arch="$tryArch"
elif command -v uname > /dev/null && tryArch="$(uname -m)"; then
	echo >&2 "warning: neither of 'dpkg' or 'apk' found, falling back to 'uname'"
	arch="$tryArch"

	os="$(uname -o 2>/dev/null || :)"
	case "$os" in
		Cygwin | Msys)
			# TODO support non-amd64 Windows
			found 'windows-amd64'
			;;
	esac
fi

case "$arch" in
	amd64 | x86_64)    found 'amd64'    ;;
	arm64 | aarch64)   found 'arm64v8'  ;;
	armel)             found 'arm32v5'  ;;
	armv6*)            found 'arm32v6'  ;;
	armv7*)            found 'arm32v7'  ;;
	i[3456]86 | x86)   found 'i386'     ;;
	mips64el)          found 'mips64le' ;; # TODO "uname -m" is just "mips64" (which is also "apk --print-arch" on big-endian MIPS) so we ought to disambiguate that somehow
	ppc64el | ppc64le) found 'ppc64le'  ;;
	riscv64)           found 'riscv64'  ;;
	s390x)             found 's390x'    ;;

	armhf)
		if [ -s /etc/os-release ] && id="$(grep -Em1 '^ID=[^[:space:]]+$' /etc/os-release)"; then
			eval "$id"
			case "${ID:-}" in
				alpine | raspbian) found 'arm32v6' ;;
				*)                 found 'arm32v7' ;;
			esac
		else
			echo >&2 "warning: '$arch' is ambiguous (and '/etc/os-release' is missing 'ID=xxx'), assuming 'arm32v6' for safety"
			found 'arm32v6'
		fi
		;;

	*)
		echo >&2 "error: unknown architecture: '$arch'"
		exit 1
		;;
esac
