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

# Runs the script over one commit the fixture's edit produced. `base_rev` and
# `head_rev` default to that commit's parent and the commit itself; a fixture that
# diverges passes the two branch tips, so the guard's own merge-base resolution is
# what is under test.
check() {
  local dir="$1" label="$2" expected="$3" base_rev="${4:-HEAD~1}" head_rev="${5:-HEAD}"
  local parent head out status
  parent=$(git -C "$dir" rev-parse "$base_rev")
  head=$(git -C "$dir" rev-parse "$head_rev")
  out=$(cd "$dir" && python3 "$SCRIPT" "$parent" "$head" 2>&1)
  status=$?
  if [ "$status" -eq "$expected" ]; then
    pass "$label (exit $status)"
  else
    fail "$label: expected exit $expected, got $status"
    printf '%s\n' "$out" | sed 's/^/       /'
  fi
}

commit() { git -C "$1" commit -q -am "$2"; }

# Removes one path's entry from a fixture's lock file, the way a regeneration or a
# declassification does.
drop_lock_entry() {
  python3 - "$1" "$2" <<'PY'
import sys
path, entry = sys.argv[1] + "/.speakeasy/gen.lock", sys.argv[2]
text = open(path, encoding="utf-8").read()
open(path, "w", encoding="utf-8").write(text.replace("  " + entry + ":\n", "", 1))
PY
}

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

echo "9. deleting a tracked generated file, and its lock entry, fails"
base=$(new_fixture delete-tracked)
git -C "$base" rm -q internal/provider/thing.go
python3 - "$base" <<'PY'
import sys
path = sys.argv[1] + "/.speakeasy/gen.lock"
text = open(path, encoding="utf-8").read()
open(path, "w", encoding="utf-8").write(
    text.replace("  internal/provider/thing.go:\n", "")
)
PY
git -C "$base" add -A
git -C "$base" commit -q -m "delete generated"
check "$base" "deleted tracked internal/provider/thing.go" 1

echo "10. deleting a hand-written file under a guarded prefix passes"
base=$(new_fixture delete-handwritten)
git -C "$base" rm -q internal/provider/extra_resource.go
commit "$base" "delete hand-written"
check "$base" "deleted internal/provider/extra_resource.go, not in gen.lock" 0

echo "11. an added file under internal/sdk/ fails like one under internal/provider/"
base=$(new_fixture add-under-sdk)
echo 'package sdk' > "$base/internal/sdk/client_extra.go"
git -C "$base" add -A
git -C "$base" commit -q -m "add sdk"
check "$base" "added internal/sdk/client_extra.go" 1

echo "12. a deletion is still caught when main advanced past the merge base"
base=$(new_fixture diverged-delete)
git -C "$base" checkout -q -b main-advance
drop_lock_entry "$base" internal/provider/thing.go
git -C "$base" commit -q -am "main stops tracking the file"
git -C "$base" checkout -q -b topic main-advance~1
git -C "$base" rm -q internal/provider/thing.go
drop_lock_entry "$base" internal/provider/thing.go
git -C "$base" commit -q -am "delete a generated file"
check "$base" "deleted generated file, entry gone from both tips" 1 \
  refs/heads/main-advance refs/heads/topic

echo "13. an edit to a file main has only just started tracking passes"
base=$(new_fixture diverged-track)
git -C "$base" checkout -q -b main-advance
python3 - "$base" <<'PY'
import sys
path = sys.argv[1] + "/.speakeasy/gen.lock"
text = open(path, encoding="utf-8").read()
open(path, "w", encoding="utf-8").write(
    text.replace("trackedFiles:\n", "trackedFiles:\n  internal/provider/extra_resource.go:\n", 1)
)
PY
git -C "$base" commit -q -am "main starts tracking the path"
git -C "$base" checkout -q -b topic main-advance~1
echo 'package provider // tweak' > "$base/internal/provider/extra_resource.go"
commit "$base" "tweak hand-written"
check "$base" "edit at the merge base, generated only at the base tip" 0 \
  refs/heads/main-advance refs/heads/topic

echo "14. an edit after a file leaves gen.lock passes, so a declassification is not guarded"
base=$(new_fixture declassified)
drop_lock_entry "$base" internal/provider/thing.go
echo 'package provider // now hand-written' > "$base/internal/provider/thing.go"
commit "$base" "declassify and edit"
check "$base" "edit after leaving gen.lock" 0

echo "15. deleting a tracked file outside the guarded prefixes passes, as it did before"
base=$(new_fixture delete-tracked-elsewhere)
git -C "$base" rm -q main.go
drop_lock_entry "$base" main.go
git -C "$base" commit -q -am "delete tracked main.go"
check "$base" "deleted tracked main.go, outside the prefixes" 0

if [ "$failures" -ne 0 ]; then
  printf '\n%d check(s) failed\n' "$failures"
  exit 1
fi
echo
echo "all generated-pairing checks passed"
