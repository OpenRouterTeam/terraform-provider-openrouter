#!/usr/bin/env bash
# Resolve and publish the release tag.
#
# GoReleaser keys the whole build off the tag, so the tag has to exist before
# staging. Pushing it before the staged archives pass, though, leaves a tag
# pinned to a commit whose code failed the scan: a later dispatch reuses that
# tag, checks out its old tree and rebuilds the vulnerable revision, while the
# acceptance gate grades the newer commit. So the tag is created locally,
# the archives are staged and scanned, and only then is the tag pushed.
#
# usage: release-tag.sh <tag> [resolve|push]
#
#   resolve  Create the tag locally if it is absent. Refuse to continue when a
#            tag already points at a commit other than HEAD.
#   push     Push the tag after the staged artifacts pass. Refuse to overwrite
#            a remote tag that points at a different commit.
#
# Prints one of created|exists|pushed for the workflow log.

set -euo pipefail

TAG="${1:?usage: release-tag.sh <tag> [resolve|push]}"
MODE="${2:-resolve}"
REMOTE="${REMOTE:-origin}"

head_sha=$(git rev-parse --verify 'HEAD^{commit}')
remote_sha=$(git ls-remote --tags --refs "$REMOTE" "refs/tags/$TAG" | awk '{print $1}')

ensure_identity() {
  git config user.name "github-actions[bot]"
  git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
}

bump_advice() {
  cat >&2 <<EOF
::error::Tag $TAG is at $1 but this run is releasing $head_sha. Reusing it rebuilds a
commit whose artifacts failed the scan, which is why the tag is no longer pushed
before the gate. Bump releaseVersion in .speakeasy/gen.lock (a regeneration does
that on its own after main moves) and dispatch Publish again. Do not re-tag a
published version, and do not delete a tag users may already have installed.
EOF
}

case "$MODE" in
  resolve)
    if [ -n "$remote_sha" ]; then
      if [ "$remote_sha" != "$head_sha" ]; then
        bump_advice "$remote_sha"
        exit 1
      fi
      echo "exists"
      exit 0
    fi
    if git rev-parse -q --verify "refs/tags/$TAG" > /dev/null; then
      local_sha=$(git rev-parse --verify "refs/tags/$TAG^{commit}")
      if [ "$local_sha" != "$head_sha" ]; then
        echo "::error::Local tag $TAG points at $local_sha, not at $head_sha" >&2
        exit 1
      fi
      echo "exists"
      exit 0
    fi
    ensure_identity
    git tag -a "$TAG" -m "Release $TAG"
    echo "created"
    ;;
  push)
    if [ -n "$remote_sha" ]; then
      if [ "$remote_sha" != "$head_sha" ]; then
        bump_advice "$remote_sha"
        exit 1
      fi
      echo "pushed"
      exit 0
    fi
    if ! git rev-parse -q --verify "refs/tags/$TAG" > /dev/null; then
      ensure_identity
      git tag -a "$TAG" -m "Release $TAG"
    fi
    # Keep stdout machine-readable for the caller.
    git push "$REMOTE" "refs/tags/$TAG" >&2
    echo "pushed"
    ;;
  *)
    echo "::error::unknown mode '$MODE' (expected resolve or push)" >&2
    exit 2
    ;;
esac
