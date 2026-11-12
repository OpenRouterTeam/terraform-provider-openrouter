#!/usr/bin/env bash
# Tests for .github/scripts/check_generated_pairing.py.
#
# The guard has two clauses and they are not interchangeable. A file listed in
# `.speakeasy/gen.lock` is generated, so an edit to it anywhere needs a generator
# input that observes the edit. A guarded *prefix* exists only because the head's
# lock file cannot list a file the pull request just added. Applying the prefix
# clause to an edit of a hand-written file that was already there fails a pull
# request that changes nothing generated (#672, an edit to
# internal/provider/guardrail_model_ids_test.go with no generator input), and
# forces a declaration asserting something untrue.
#
# Runs against throwaway repositories; no network and no credentials.
# usage: bash .github/scripts/tests/check-generated-pairing-test.sh

set -uo pipefail

SCRIPT="$(cd "$(dirname "$0")/.." && pwd)/check_generated_pairing.py"
failures=0
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT

pass() { printf '  ok   %s\n' "$1"; }
fail() { printf '  FAIL %s\n' "$1"; failures=$((failures + 1)); }

# A repository whose lock file tracks one generated path, with one hand-written
# file already sitting beside it. `edit` runs in the work tree and commits.
new_fixture() {
  local name="$1"
  local base="$scratch/$name"
  mkdir -p "$base"
  git -C "$base" init -q
  git -C "$base" config user.name "Test"
  git -C "$base" config user.email "test@example.com"
  mkdir -p "$base/.speakeasy" "$base/internal/provider" "$base/internal/sdk" "$base/handwritten" "$base/docs"
  cat > "$base/.speakeasy/gen.lock" <<'LOCK'
lockVersion: 2.0.0
trackedFiles:
  internal/provider/thing.go:
  internal/sdk/openrouter.go:
  main.go:
  docs/readme.md:
examples:
  internal/provider/thing.go: examples/resources/thing/resource.tf
LOCK
  cat > "$base/.speakeasy/gen.yaml" <<'YAML'
generation:
  baseVersion: 0.1.0
YAML
  echo 'package provider' > "$base/internal/provider/thing.go"
  echo 'package sdk'      > "$base/internal/sdk/openrouter.go"
  echo 'package main'     > "$base/main.go"
  # Hand-written and NOT in the lock file: an additional resource.
  echo 'package provider' > "$base/internal/provider/extra_resource.go"
  echo 'package handwritten' > "$base/handwritten/new_resource.go"
  echo '# docs'           > "$base/docs/readme.md"
  git -C "$base" add -A
  git -C "$base" commit -q -m "base"
  echo "$base"
}

# Runs the script over the single commit the fixture's edit produced.
check() {
  local base="$1" label="$2" expected="$3"
  local parent head out status
  parent=$(git -C "$base" rev-parse HEAD~1)
  head=$(git -C "$base" rev-parse HEAD)
  out=$(cd "$base" && python3 "$SCRIPT" "$parent" "$head" 2>&1)
  status=$?
  if [ "$status" -eq "$expected" ]; then
    pass "$label (exit $status)"
  else
    fail "$label: expected exit $expected, got $status"
    printf '%s\n' "$out" | sed 's/^/       /'
  fi
}

commit() { git -C "$1" commit -q -am "$2"; }

echo "1. an edit to a hand-written file under a guarded prefix passes"
base=$(new_fixture edit-handwritten)
echo 'package provider // tweak' > "$base/internal/provider/extra_resource.go"
commit "$base" "tweak hand-written"
check "$base" "modified internal/provider/extra_resource.go, not in gen.lock" 0

echo "2. a newly added file under a guarded prefix still fails"
base=$(new_fixture add-under-prefix)
echo 'package provider' > "$base/internal/provider/brand_new.go"
git -C "$base" add -A
git -C "$base" commit -q -m "add"
check "$base" "added internal/provider/brand_new.go" 1

echo "3. an added file outside the guarded prefixes passes"
base=$(new_fixture add-outside)
echo 'package handwritten' > "$base/handwritten/another.go"
git -C "$base" add -A
git -C "$base" commit -q -m "add"
check "$base" "added handwritten/another.go" 0

echo "4. an edit to a tracked generated file still fails"
base=$(new_fixture edit-tracked)
echo 'package provider // tweak' > "$base/internal/provider/thing.go"
commit "$base" "tweak generated"
check "$base" "modified tracked internal/provider/thing.go" 1

echo "5. the same edit passes once a generator input changes too"
base=$(new_fixture edit-tracked-with-input)
echo 'package provider // tweak' > "$base/internal/provider/thing.go"
printf 'overlay:\n  - note\n' >> "$base/.speakeasy/gen.yaml"
commit "$base" "tweak generated + input"
check "$base" "modified tracked file with .speakeasy/gen.yaml" 0

echo "6. a tracked generated file outside the prefixes is still guarded"
base=$(new_fixture edit-tracked-elsewhere)
echo 'package main // tweak' > "$base/main.go"
commit "$base" "tweak main"
check "$base" "modified tracked main.go" 1

echo "7. a rename that lands a hand-written file in a guarded prefix fails"
base=$(new_fixture rename-into-prefix)
git -C "$base" mv handwritten/new_resource.go internal/provider/moved_resource.go
commit "$base" "move into internal/provider/"
check "$base" "renamed handwritten/new_resource.go into internal/provider/" 1

echo "8. a prose-only change passes"
base=$(new_fixture docs-only)
echo '# docs, edited' > "$base/docs/readme.md"
commit "$base" "docs"
check "$base" "modified docs/readme.md" 0

if [ "$failures" -ne 0 ]; then
  printf '\n%d check(s) failed\n' "$failures"
  exit 1
fi
echo
echo "all generated-pairing checks passed"
