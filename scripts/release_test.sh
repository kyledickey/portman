#!/usr/bin/env bash
# Exercise releases against a disposable local remote, never the real origin.
set -euo pipefail

script=$(cd "$(dirname "$0")" && pwd)/release.sh
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
git init -q --bare "$tmp/origin.git"
git init -q "$tmp/work"
cd "$tmp/work"
git config user.name test
git config user.email test@example.com
git commit -q --allow-empty -m initial
git remote add origin "$tmp/origin.git"

fails() {
	if "$@" >"$tmp/output" 2>&1; then
		echo "Expected failure: $*" >&2
		exit 1
	fi
}
released() {
	git ls-remote --exit-code --tags --refs origin "refs/tags/$1" >/dev/null
}

fails bash "$script" invalid
fails bash "$script" beta
git tag v0.1.0-beta.9
git push -q origin refs/tags/v0.1.0-beta.9
bash "$script" beta
released v0.1.0-beta.10
bash "$script" stable
released v0.1.0
fails bash "$script" stable
bash "$script" beta
released v0.1.1-beta.1
bash "$script" stable
released v0.1.1

# The newest version wins even when an older series has a larger beta number.
git tag v0.2.0-beta.1
git push -q origin refs/tags/v0.2.0-beta.1
bash "$script" beta
released v0.2.0-beta.2

git tag v0.2.0-beta.3
fails bash "$script" beta
git tag -d v0.2.0-beta.3 >/dev/null
git remote set-url --push origin "$tmp/missing.git"
fails bash "$script" beta
git show-ref --verify --quiet refs/tags/v0.2.0-beta.3
grep -q 'local tag v0.2.0-beta.3 still exists' "$tmp/output"

git remote set-url origin "$tmp/missing.git"
fails bash "$script" beta
echo "release tests passed"
