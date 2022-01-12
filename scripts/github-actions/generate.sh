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
	if [ ! -x "$dir/bin/bashbrew" ]; then
		echo >&2 'Building bashbrew ...'
		"$dir/bashbrew.sh" --version > /dev/null
		"$dir/bin/bashbrew" --version >&2
	fi
	export PATH="$dir/bin:$PATH"
	bashbrew --version > /dev/null
fi

mkdir "$tmp/library"
export BASHBREW_LIBRARY="$tmp/library"

eval "${GENERATE_STACKBREW_LIBRARY:-./generate-stackbrew-library.sh}" > "$BASHBREW_LIBRARY/$image"

# if we don't appear to be able to fetch the listed commits, they might live in a PR branch, so we should force them into the Bashbrew cache directly to allow it to do what it needs
if ! bashbrew from "$image" &> /dev/null; then
	bashbrewGit="${BASHBREW_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/bashbrew}/git"
	if [ ! -d "$bashbrewGit" ]; then
		# if we're here, it's because "bashbrew from" failed so our cache directory might not have been created
		bashbrew from https://github.com/docker-library/official-images/raw/HEAD/library/hello-world:latest > /dev/null
	fi
	git -C "$bashbrewGit" fetch --quiet --update-shallow "$PWD" HEAD > /dev/null
	bashbrew from "$image" > /dev/null
fi

tags="$(bashbrew list --build-order --uniq "$image")"

# see https://github.com/docker-library/python/commit/6b513483afccbfe23520b1f788978913e025120a for the ideal of what this would be (minimal YAML in all 30+ repos, shared shell script that outputs fully dynamic steps list), if GitHub Actions were to support a fully dynamic steps list

order=()
declare -A metas=()
for tag in $tags; do
	echo >&2 "Processing $tag ..."
	bashbrewImage="${tag##*/}" # account for BASHBREW_NAMESPACE being set
	meta="$(
		bashbrew cat --format '
			{{- $e := .TagEntry -}}
			{{- $arch := $e.HasArchitecture arch | ternary arch ($e.Architectures | first) -}}
			{{- "{" -}}
				"name": {{- json ($e.Tags | first) -}},
				"tags": {{- json ($.Tags namespace false $e) -}},
				"directory": {{- json ($e.ArchDirectory $arch) -}},
				"file": {{- json ($e.ArchFile $arch) -}},
				"constraints": {{- json $e.Constraints -}},
				"froms": {{- json ($.ArchDockerFroms $arch $e) -}}
			{{- "}" -}}
		' "$bashbrewImage" | jq -c '
			{
				name: .name,
				os: (
					if (.constraints | contains(["windowsservercore-ltsc2022"])) or (.constraints | contains(["nanoserver-ltsc2022"])) then
						"windows-2022"
					elif (.constraints | contains(["windowsservercore-1809"])) or (.constraints | contains(["nanoserver-1809"])) then
						"windows-2019"
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
							[ "--file", ((.directory + "/" + .file) | @sh) ]
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

	parent="$(bashbrew parents "$bashbrewImage" | tail -1)" # if there ever exists an image with TWO parents in the same repo, this will break :)
	if [ -n "$parent" ]; then
		parentBashbrewImage="${parent##*/}" # account for BASHBREW_NAMESPACE being set
		parent="$(bashbrew list --uniq "$parentBashbrewImage")" # normalize
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
					| unique
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
					"# create a dummy empty image/layer so we can --filter since= later to get a meaningful image list",
					"{ echo FROM " + (
						if .os | startswith("windows-") then
							"mcr.microsoft.com/windows/servercore:ltsc" + (.os | ltrimstr("windows-"))
						else
							"busybox:latest"
						end
					) + "; echo RUN :; } | docker build --no-cache --tag image-list-marker -",
					(
						if (env.BASHBREW_GENERATE_SKIP_PGP_PROXY) or (.os | startswith("windows-")) then
							empty
						else
							(
								"# PGP Happy Eyeballs",
								"git clone --depth 1 https://github.com/tianon/pgp-happy-eyeballs.git ~/phe",
								"~/phe/hack-my-builds.sh",
								"rm -rf ~/phe"
							)
						end
					)
				] | join("\n")),
				pull: ([ .meta.froms[] | select(. != "scratch") | "docker pull " + @sh ] | join("\n")),
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
