#!/usr/bin/env bash
# Tests for .github/scripts/release-tag.sh, including the recovery path the
# review asked for: the archive scan fails, a dependency fix lands, Publish is
# dispatched again. That retry must release the fixed commit. It must never
# reuse a tag pinned to the commit the scan rejected, because GoReleaser then
# rebuilds the old tree while the acceptance gate grades the new one.
#
# Runs against throwaway repositories; needs no network and no credentials.
# usage: bash .github/scripts/tests/release-tag-test.sh

set -uo pipefail

SCRIPT="$(cd "$(dirname "$0")/.." && pwd)/release-tag.sh"
TAG="v0.3.150"
failures=0
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT

pass() { printf '  ok   %s\n' "$1"; }
fail() { printf '  FAIL %s\n' "$1"; failures=$((failures + 1)); }

# A bare origin plus a working clone, so push behaviour is exercised for real.
new_fixture() {
  local name="$1"
  local base="$scratch/$name"
  git init -q --bare "$base/remote.git"
  git clone -q "$base/remote.git" "$base/work" 2>/dev/null
  git -C "$base/work" config user.name "Test"
  git -C "$base/work" config user.email "test@example.com"
  echo "$name" > "$base/work/file.txt"
  git -C "$base/work" add -A
  git -C "$base/work" commit -q -m "base $(basename "$name")"
  echo "$base"
}

# A fresh clone that fetches tags, like a new workflow run on a new runner.
fresh_clone() {
  local base="$1" name="$2"
  git clone -q "$base/remote.git" "$base/$name" 2>/dev/null
  git -C "$base/$name" config user.name "Test"
  git -C "$base/$name" config user.email "test@example.com"
  echo "$base/$name"
}

remote_tag_sha() {
  git -C "$1/remote.git" rev-parse --verify "refs/tags/$TAG^{commit}" 2>/dev/null
}

head_sha_of() { git -C "$1" rev-parse --verify 'HEAD^{commit}'; }

echo "1. resolve creates the tag locally and leaves the remote untouched"
base=$(new_fixture create-only)
out=$(cd "$base/work" && bash "$SCRIPT" "$TAG" resolve 2>&1)
[ "$out" = "created" ] && pass "reports created" || fail "expected 'created', got '$out'"
git -C "$base/work" rev-parse -q --verify "refs/tags/$TAG" > /dev/null \
  && pass "local tag exists" || fail "local tag missing"
[ -z "$(remote_tag_sha "$base")" ] \
  && pass "remote has no tag before the scan passes" || fail "remote tag was pushed too early"

echo "2. push publishes it once the staged artifacts pass"
out=$(cd "$base/work" && bash "$SCRIPT" "$TAG" push 2>/dev/null)
[ "$out" = "pushed" ] && pass "reports pushed" || fail "expected 'pushed', got '$out'"
[ "$(remote_tag_sha "$base")" = "$(head_sha_of "$base/work")" ] \
  && pass "remote tag points at the released commit" || fail "remote tag points elsewhere"

echo "3. scan fails, the fix lands, Publish retries: the retry releases the fix"
base=$(new_fixture retry)
# First run: the tag is created for staging, then the archive scan fails, so the
# step that pushes never runs.
(cd "$base/work" && bash "$SCRIPT" "$TAG" resolve > /dev/null 2>&1)
[ -z "$(remote_tag_sha "$base")" ] \
  && pass "failed scan left no remote tag" || fail "a failed scan left a remote tag behind"
# The fix lands on main, and a new run starts from a fresh clone.
git -C "$base/work" commit -q --allow-empty -m "fix: bump the vulnerable module"
git -C "$base/work" push -q origin HEAD
retry=$(fresh_clone "$base" retry2)
out=$(cd "$retry" && bash "$SCRIPT" "$TAG" resolve 2>&1)
[ "$out" = "created" ] && pass "retry creates the tag at the new commit" || fail "retry output '$out'"
[ "$(git -C "$retry" rev-parse --verify "refs/tags/$TAG^{commit}")" = "$(head_sha_of "$retry")" ] \
  && pass "tag follows the fix, not the rejected commit" || fail "tag points at the rejected commit"
(cd "$retry" && bash "$SCRIPT" "$TAG" push > /dev/null 2>&1)
[ "$(remote_tag_sha "$base")" = "$(head_sha_of "$retry")" ] \
  && pass "published tag is the fixed commit" || fail "published tag is stale"

echo "4. a tag already pinned to the rejected commit stops the retry"
base=$(new_fixture pinned)
stale=$(head_sha_of "$base/work")
git -C "$base/work" tag -a "$TAG" -m "Release $TAG"
git -C "$base/work" push -q origin "refs/tags/$TAG"          # pretend an older workflow pushed early
git -C "$base/work" commit -q --allow-empty -m "fix: bump the vulnerable module"
git -C "$base/work" push -q origin HEAD
retry=$(fresh_clone "$base" pinned2)
out=$(cd "$retry" && bash "$SCRIPT" "$TAG" resolve 2>&1); status=$?
[ "$status" -ne 0 ] && pass "resolve exits non-zero" || fail "resolve reused the pinned tag"
case "$out" in
  *"Bump releaseVersion"*) pass "tells the maintainer to bump the version" ;;
  *) fail "missing the version-bump instruction: $out" ;;
esac
[ "$out" != *"created"* ] && pass "does not create a second tag" || fail "created a tag over the pinned one"
[ "$(remote_tag_sha "$base")" = "$stale" ] \
  && pass "remote tag is untouched" || fail "remote tag moved"

echo "5. push refuses to overwrite a remote tag at a different commit"
out=$(cd "$retry" && bash "$SCRIPT" "$TAG" push 2>&1); status=$?
[ "$status" -ne 0 ] && pass "push exits non-zero" || fail "push overwrote the pinned tag"
[ "$(remote_tag_sha "$base")" = "$stale" ] && pass "remote tag still at the pinned commit" || fail "remote tag moved"

echo "6. a tag that already matches the commit is reused, so re-runs are safe"
base=$(new_fixture idempotent)
(cd "$base/work" && bash "$SCRIPT" "$TAG" resolve > /dev/null 2>&1)
out=$(cd "$base/work" && bash "$SCRIPT" "$TAG" resolve 2>&1); status=$?
[ "$status" -eq 0 ] && [ "$out" = "exists" ] \
  && pass "matching tag reported as exists" || fail "expected 'exists', got '$out' (exit $status)"

echo "7. the publish workflow pushes the tag after the scan, not before"
WORKFLOW="$(cd "$(dirname "$0")/../.." && pwd)/workflows/sdk_publish.yaml"
line_of() { grep -n "$1" "$WORKFLOW" | head -1 | cut -d: -f1; }
prepare=$(line_of 'name: Prepare release tag')
stage=$(grep -n 'name: Stage GoReleaser artifacts' "$WORKFLOW" | head -1 | cut -d: -f1)
scan=$(line_of 'name: Scan linux_amd64')
push=$(line_of 'name: Push release tag')
release=$(line_of 'name: Run GoReleaser')
if [ -z "$prepare" ] || [ -z "$stage" ] || [ -z "$scan" ] || [ -z "$push" ] || [ -z "$release" ]; then
  fail "workflow steps not found (prepare=$prepare stage=$stage scan=$scan push=$push release=$release)"
else
  [ "$prepare" -lt "$stage" ] && pass "tag prepared before staging" || fail "prepare is not before staging"
  [ "$scan" -lt "$push" ] && pass "tag pushed after the scan" || fail "tag is pushed before the scan (the P1 regressed)"
  [ "$push" -lt "$release" ] && pass "tag pushed before the release" || fail "push is not before the release"
  grep -q 'release-tag.sh "$TAG" push' "$WORKFLOW" \
    && pass "push step goes through release-tag.sh" || fail "push step bypasses release-tag.sh"
  sed -n "${prepare},${stage}p" "$WORKFLOW" | grep -q 'release-tag.sh "$TAG" resolve' \
    && pass "prepare step goes through the script" || fail "prepare step does not use the script"
fi

if [ "$failures" -ne 0 ]; then
  printf '\n%d check(s) failed\n' "$failures"
  exit 1
fi
echo
echo "all release-tag checks passed"
