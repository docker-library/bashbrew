#!/usr/bin/env bash
set -Eeuo pipefail

image="${GITHUB_REPOSITORY##*/}" # "python", "golang", etc

[ -n "${GENERATE_STACKBREW_LIBRARY:-}" ] || [ -x ./generate-stackbrew-library.sh ] # sanity check

tmp="$(mktemp -d)"
trap "$(printf 'rm -rf %q' "$tmp")" EXIT

if ! command -v bashbrew &> /dev/null; then
	dir="$(readlink -f "$BASH_SOURCE")"
	dir="$(dirname "$dir")"
	dir="$(cd "$dir/../.." && pwd -P)"
	echo >&2 'Building bashbrew ...'
	"$dir/bashbrew.sh" --version > /dev/null
	export PATH="$dir/bin:$PATH"
	bashbrew --version >&2
fi

mkdir "$tmp/library"
export BASHBREW_LIBRARY="$tmp/library"

eval "${GENERATE_STACKBREW_LIBRARY:-./generate-stackbrew-library.sh}" > "$BASHBREW_LIBRARY/$image"

tags="$(bashbrew list --build-order --uniq "$image")"

# see https://github.com/docker-library/python/commit/6b513483afccbfe23520b1f788978913e025120a for the ideal of what this would be (minimal YAML in all 30+ repos, shared shell script that outputs fully dynamic steps list), if GitHub Actions were to support a fully dynamic steps list

order=()
declare -A metas=()
for tag in $tags; do
	echo >&2 "Processing $tag ..."
	meta="$(
		bashbrew cat --format '
			{{- $e := .TagEntry -}}
			{{- "{" -}}
				"name": {{- json ($e.Tags | first) -}},
				"tags": {{- json ($.Tags "" false $e) -}},
				"directory": {{- json $e.Directory -}},
				"file": {{- json $e.File -}},
				"constraints": {{- json $e.Constraints -}},
				"froms": {{- json ($.DockerFroms $e) -}}
			{{- "}" -}}
		' "$tag" | jq -c '
			{
				name: .name,
				os: (
					if (.constraints | contains(["windowsservercore-1809"])) or (.constraints | contains(["nanoserver-1809"])) then
						"windows-2019"
					elif .constraints | contains(["windowsservercore-ltsc2016"]) then
						"windows-2016"
					elif .constraints == [] or .constraints == ["!aufs"] then
						"ubuntu-latest"
					else
						# use an intentionally invalid value so that GitHub chokes and we notice something is wrong
						"invalid-or-unknown"
					end
				),
				meta: { entries: [ . ] },
				runs: {
					build: (
						[
							"docker build"
						]
						+ (
							.tags
							| map(
								"--tag " + (. | @sh)
							)
						)
						+ if .file != "Dockerfile" then
							[ "--file", (.file | @sh) ]
						else
							[]
						end
						+ [
							(.directory | @sh)
						]
						| join(" ")
					),
					history: ("docker history " + (.tags[0] | @sh)),
					test: ("~/oi/test/run.sh " + (.tags[0] | @sh)),
				},
			}
		'
	)"

	parent="$(bashbrew parents "$tag" | tail -1)" # if there ever exists an image with TWO parents in the same repo, this will break :)
	if [ -n "$parent" ]; then
		parent="$(bashbrew list --uniq "$parent")" # normalize
		parentMeta="${metas["$parent"]}"
		parentMeta="$(jq -c --argjson meta "$meta" '
			. + {
				name: (.name + ", " + $meta.name),
				os: (if .os == $meta.os then .os else "invalid-os-chain--" + .os + "+" + $meta.os end),
				meta: { entries: (.meta.entries + $meta.meta.entries) },
				runs: (
					.runs
					| to_entries
					| map(
						.value += "\n" + $meta.runs[.key]
					)
					| from_entries
				),
			}
		' <<<"$parentMeta")"
		metas["$parent"]="$parentMeta"
	else
		metas["$tag"]="$meta"
		order+=( "$tag" )
	fi
done

strategy="$(
	for tag in "${order[@]}"; do
		jq -c '
			.meta += {
				froms: (
					[ .meta.entries[].froms[] ]
					- [ .meta.entries[].tags[] ]
				),
				dockerfiles: [
					.meta.entries[]
					| .directory + "/" + .file
				],
			}
			| .runs += {
				prepare: ([
					(
						if .os | startswith("windows-") then
							"# enable symlinks on Windows (https://git-scm.com/docs/git-config#Documentation/git-config.txt-coresymlinks)",
							"git config --global core.symlinks true",
							"# ... make sure they are *real* symlinks (https://github.com/git-for-windows/git/pull/156)",
							"export MSYS=winsymlinks:nativestrict",
							"# make sure line endings get checked out as-is",
							"git config --global core.autocrlf false"
						else
							empty
						end
					),
					"git clone --depth 1 https://github.com/docker-library/official-images.git -b master ~/oi",
					"# create a dummy empty image/layer so we can --filter since= later to get a meanginful image list",
					"{ echo FROM " + (
						if (.os | startswith("windows-")) then
							"mcr.microsoft.com/windows/servercore:ltsc" + (.os | ltrimstr("windows-"))
						else
							"busybox:latest"
						end
					) + "; echo RUN :; } | docker build --no-cache --tag image-list-marker -",
					(
						if .os | startswith("windows-") | not then
							(
								"# PGP Happy Eyeballs",
								"git clone --depth 1 https://github.com/tianon/pgp-happy-eyeballs.git ~/phe",
								"~/phe/hack-my-builds.sh",
								"rm -rf ~/phe"
							)
						else
							empty
						end
					)
				] | join("\n")),
				pull: ([ .meta.froms[] | "docker pull " + @sh ] | join("\n")),
				# build
				# history
				# test
				images: "docker image ls --filter since=image-list-marker",
			}
		' <<<"${metas["$tag"]}"
	done | jq -cs '
		{
			"fail-fast": false,
			matrix: { include: . },
		}
	'
)"

if [ -t 1 ]; then
	jq <<<"$strategy"
else
	cat <<<"$strategy"
fi
