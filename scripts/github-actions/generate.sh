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
if ! bashbrew fetch "$image" &> /dev/null; then
	gitCache="$(bashbrew cat --format '{{ gitCache }}' "$image")"
	git -C "$gitCache" fetch --quiet --update-shallow "$PWD" HEAD > /dev/null
	bashbrew fetch "$image" > /dev/null
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
				"builder": {{- json ($e.ArchBuilder $arch) -}},
				"constraints": {{- json $e.Constraints -}},
				"froms": {{- json ($.ArchDockerFroms $arch $e) -}},
				"platform": {{- json (ociPlatform $arch).String -}}
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
							# https://github.com/docker-library/bashbrew/pull/43
							if .builder == "classic" or .builder == "" then
								"DOCKER_BUILDKIT=0 docker build"
							elif .builder == "buildkit" then
								"docker buildx build --progress plain --build-arg BUILDKIT_SYNTAX=\"$BASHBREW_BUILDKIT_SYNTAX\""
							# TODO elif .builder == "oci-import" then ????
							else
								"echo >&2 " + ("error: unknown/unsupported builder: " + .builder | @sh) + "\nexit 1\n#"
							end
						]
						+ [
							# TODO error out on unsupported platforms, or just let the emulation go wild?
							"--platform", .platform
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
					test: (
						[
							"set -- " + (.tags[0] | @sh),
							# https://github.com/docker-library/bashbrew/issues/46#issuecomment-1152567694 (allow local test config / tests)
							"if [ -s ./.test/config.sh ]; then set -- --config ~/oi/test/config.sh --config ./.test/config.sh \"$@\"; fi",
							"~/oi/test/run.sh \"$@\""
						] | join("\n")
					),
				},
			}
		'
	)"

	if parent="$(bashbrew parents --depth=1 "$bashbrewImage" | grep "^${tag%%:*}:")" && [ -n "$parent" ]; then
		if [ "$(wc -l <<<"$parent")" -ne 1 ]; then
			echo >&2 "error: '$tag' has multiple parents in the same repository and this script can't handle that yet!"
			echo >&2 "$parent"
			exit 1
		fi
		parent="$(bashbrew parents "$bashbrewImage" | grep "^${tag%%:*}:" | tail -1)" # get the "ultimate" this-repo parent
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
		# envObjectToGitHubEnvFileJQ converts from the output of ~/oi/.bin/bashbrew-buildkit-env-setup.sh into what GHA expects in $GITHUB_ENV
		# (in a separate env to make embedding/quoting easier inside this sub-jq that generates JSON that embeds shell scripts)
		envObjectToGitHubEnvFileJQ='
			to_entries | map(
				(.key | if test("[^a-zA-Z0-9_]+") then
					error("invalid env key: \(.)")
				else . end)
				+ "="
				+ (.value | if test("[\r\n]+") then
					error("invalid env value: \(.)")
				else . end)
			) | join("\n")
		' \
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

					(
						"# https://github.com/docker-library/bashbrew/pull/43",
						if ([ .meta.entries[].builder ] | index("buildkit")) then
							# https://github.com/docker-library/bashbrew/pull/70#issuecomment-1461033890 (we need to _not_ set BASHBREW_ARCH here)
							"if [ -x ~/oi/.bin/bashbrew-buildkit-env-setup.sh ]; then",
							"\t# https://github.com/docker-library/official-images/pull/14212",
							"\tbuildkitEnvs=\"$(~/oi/.bin/bashbrew-buildkit-env-setup.sh)\"",
							"\tjq <<<\"$buildkitEnvs\" -r \(env.envObjectToGitHubEnvFileJQ | @sh) | tee -a \"$GITHUB_ENV\"",
							"else",
							"\tBASHBREW_BUILDKIT_SYNTAX=\"$(< ~/oi/.bashbrew-buildkit-syntax)\"; export BASHBREW_BUILDKIT_SYNTAX",
							"\tprintf \"BASHBREW_BUILDKIT_SYNTAX=%q\\n\" \"$BASHBREW_BUILDKIT_SYNTAX\" >> \"$GITHUB_ENV\"",
							"fi",
							empty
						else
							empty
						end
					),

					"# create a dummy empty image/layer so we can --filter since= later to get a meaningful image list",
					"{ echo FROM " + (
						if .os | startswith("windows-") then
							"mcr.microsoft.com/windows/servercore:ltsc" + (.os | ltrimstr("windows-"))
						else
							"busybox:latest"
						end
					) + "; echo RUN :; } | docker build --no-cache --tag image-list-marker -"
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
