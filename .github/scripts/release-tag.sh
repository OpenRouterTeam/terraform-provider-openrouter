#!/usr/bin/env bash
# Resolve and publish the release tag.
#
# GoReleaser keys the whole build off the tag, so the tag has to exist before
# staging. Pushing it before the staged archives pass, though, leaves a tag
# pinned to a commit whose code failed the scan: a later dispatch reuses that
# tag, checks out its old tree and rebuilds the vulnerable revision, while the
# acceptance gate grades the newer commit. So the tag is created locally, the
# archives are staged and scanned, and only then is the tag pushed.
#
# usage: release-tag.sh <tag> [resolve|push]
#
#   resolve  Create the tag locally if it is absent. Refuse to continue when a
#            tag already points at a commit other than HEAD.
#   push     Push the tag after the staged artifacts pass. Refuse to overwrite
#            a remote tag that points at a different commit.
#
# Prints one of created|exists|pushed for the workflow log. Tags are annotated,
# so the remote ref holds a tag object: always compare through the peeled ref.

set -euo pipefail

TAG="${1:?usage: release-tag.sh <tag> [resolve|push]}"
MODE="${2:-resolve}"
REMOTE="${REMOTE:-origin}"

# The tag reaches git as a refspec pattern, so allow only characters a version
# can contain and nothing a glob would interpret.
if ! printf '%s' "$TAG" | grep -qE '^v[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.]+)?$'; then
  echo "::error::refusing to use '$TAG' as a release tag" >&2
  exit 2
fi

head_sha=$(git rev-parse --verify 'HEAD^{commit}')

# Commit a tag points at, or empty when the tag is absent. An annotated tag's
# ref holds the tag object's sha, so ask for the peeled commit and fall back to
# the ref itself for a lightweight tag. Retried, because a transient network
# failure here would otherwise abort the release.
remote_tag_commit() {
  local out attempt peeled plain
  for attempt in 1 2 3; do
    if out=$(git ls-remote --tags "$REMOTE" "refs/tags/$TAG" "refs/tags/$TAG^{}" 2>/dev/null); then
      peeled=$(printf '%s\n' "$out" | awk -v ref="refs/tags/$TAG^{}" '$2 == ref {print $1}')
      plain=$(printf '%s\n' "$out" | grep -v '\^{}' | awk -v ref="refs/tags/$TAG" '$2 == ref {print $1}')
      printf '%s' "${peeled:-$plain}"
      return 0
    fi
    sleep 2
  done
  echo "::error::could not read tags from $REMOTE after three attempts" >&2
  return 1
}

ensure_identity() {
  git config user.name "github-actions[bot]"
  git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
}

bump_advice() {
  cat >&2 <<EOF
::error::Tag $TAG is at $1 but this run is releasing $head_sha. Reusing it rebuilds a
commit the scan rejected, which is why the tag is no longer pushed before the
gate. Bump releaseVersion in .speakeasy/gen.lock (a regeneration does that on
its own once main moves) and dispatch Publish again -- or, if this tag never
produced a release and main has not moved, dispatch again on the same commit. Do
not re-tag a published version, and do not delete a tag users may have installed.
EOF
}

if ! remote_sha=$(remote_tag_commit); then
  exit 1
fi

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
      echo "exists"
      exit 0
    fi
    # resolve creates the tag earlier in this job. Reaching push without one
    # means the prepare step was skipped or reordered, so fail rather than tag
    # after the scan.
    if ! git rev-parse -q --verify "refs/tags/$TAG" > /dev/null; then
      echo "::error::Local tag $TAG is missing at the push step; prepare it before staging" >&2
      exit 1
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
