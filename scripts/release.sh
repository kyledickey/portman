#!/usr/bin/env bash
# Tag the current commit and push it to origin to trigger a portman release.
# Usage: bash scripts/release.sh beta|stable
set -euo pipefail

if [[ $# != 1 || ( $1 != beta && $1 != stable ) ]]; then
	echo "Usage: $0 beta|stable" >&2
	exit 2
fi
channel=$1

refs=$(git ls-remote --tags --refs origin 'v*')
tags=() versions=()
while read -r _ ref; do
	tag=${ref#refs/tags/}
	if [[ $tag =~ ^(v[0-9]+\.[0-9]+\.[0-9]+)(-beta\.[0-9]+)?$ ]]; then
		tags+=("$tag")
		versions+=("${BASH_REMATCH[1]}")
	fi
done <<< "$refs"

if ((${#versions[@]} == 0)); then
	echo "No stable or beta tags found on origin; create the first tag manually" >&2
	exit 1
fi
version=$(printf '%s\n' "${versions[@]}" | sort -V | tail -n 1)
stable=false
betas=()
for tag in "${tags[@]}"; do
	if [[ $tag == "$version" ]]; then
		stable=true
	elif [[ $tag == "$version-beta."* ]]; then
		betas+=("$tag")
	fi
done

if [[ $channel == stable ]]; then
	if $stable || ((${#betas[@]} == 0)); then
		echo "No unreleased beta to promote for $version" >&2
		exit 1
	fi
	next="$version"
elif $stable; then
	patch=${version##*.}
	next="${version%.*}.$((10#$patch + 1))-beta.1"
else
	latest=$(printf '%s\n' "${betas[@]}" | sort -V | tail -n 1)
	number=${latest##*.}
	next="${latest%.*}.$((10#$number + 1))"
fi

echo "Creating and pushing $next at $(git rev-parse --short HEAD)"
git tag "$next"
if ! git push origin "refs/tags/$next"; then
	echo "Push failed; local tag $next still exists. Inspect it before retrying." >&2
	exit 1
fi
