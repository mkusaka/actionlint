#!/bin/bash

set -e -o pipefail

if [ ! -d .git ]; then
    echo 'This script must be run from repository root' 1>&2
    echo 'Usage: bash ./scripts/bump-version.bash 1.2.3' 1>&2
    exit 1
fi

if ! git diff --quiet; then
    echo 'Working tree is dirty! Ensure all changes are committed and working tree is clean' >&2
    exit 1
fi

if ! git diff --cached --quiet; then
    echo 'Git index is dirty! Ensure all changes are committed and Git index is clean' >&2
    exit 1
fi

if [[ "$(git rev-parse --abbrev-ref HEAD)" != main ]]; then
    echo "This script must be run on 'main' branch" >&2
    exit 1
fi

version="$1"

if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "The first argument must match to '^\d+\.\d+\.\d+$': ${version}" 1>&2
    echo 'Usage: bash ./scripts/bump-version.bash 1.2.3' 1>&2
    exit 1
fi

function sed_() {
    case "$OSTYPE" in
        darwin*)
            /usr/bin/sed -i '' -E "$@"
            ;;
        *)
            sed -i -E "$@"
            ;;
    esac
}

usage_doc='./docs/usage.md'
download_script='./scripts/download-yactionlint.bash'
tag="v${version}"
job_url='https://github.com/mkusaka/yactionlint/actions/workflows/release.yaml'

echo "Bumping up version to ${version} (tag: ${tag})"

echo "Updating $download_script"
sed_ "s/version=\"[0-9]+\\.[0-9]+\\.[0-9]+\"/version=\"${version}\"/" "$download_script"

echo "Updating $usage_doc"
sed_ "\
    s/    rev: v[0-9]+\.[0-9]+\.[0-9]+/    rev: v${version}/; \
    s/ yactionlint@[0-9]+\.[0-9]+\.[0-9]+/ yactionlint@${version}/g; \
    " "$usage_doc"

echo 'Creating a version bump commit and a version tag'
git add "$download_script" "$usage_doc"
git commit -m "bump up version to ${tag}"
git tag "$tag"

echo "Pushing bump commit to main"
git push origin main

echo "Pushing the version tag '${tag}'"
git push origin "$tag"

echo "Open release job page to check release progress ${job_url}"
if [[ "$OSTYPE" == darwin* ]]; then
    open "$job_url"
fi

echo "Update version bump timestamp"
touch .bumptimestamp

echo 'Done.'
