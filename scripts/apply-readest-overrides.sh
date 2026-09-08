#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
source="$root/readest"
build="$root/.build/readest"
buildroot="$root/.build"
overrides="$root/frontend-overrides/readest-app"

if [[ ! -d "$source/apps/readest-app" ]]; then
  echo "Readest submodule is not initialized. Run: git submodule update --init --recursive" >&2
  exit 1
fi

override_files=()
while IFS= read -r file; do
  override_files+=("$file")
done < <(cd "$overrides" && find . -type f -print | sort | sed 's#^\./##')

# PREPARE_CLEAN=1 restores the from-scratch behavior. Use it when `.build/` is
# suspected stale, or after moving the pinned Readest revision far enough that
# orphaned dependencies could linger in the reused `node_modules`.
if [[ "${PREPARE_CLEAN:-0}" == 1 ]]; then
  rm -rf "$buildroot"
fi

mkdir -p "$build"

if command -v rsync >/dev/null 2>&1; then
  # Mirror `readest/` incrementally rather than deleting and re-copying it. The
  # excluded trees are generated here, not upstream: `node_modules` is installed
  # below, `.next`/`out` belong to Next.js, and `public/vendor` is filled in at
  # the end of this script. Copying `node_modules` only to delete it again cost
  # ~85s per run and forced a cold `bun install` on every build.
  rsync_args=(
    --archive
    --delete
    --exclude 'node_modules/'
    --exclude '/.git/'
    --exclude '/.gitmodules'
    --exclude '/apps/readest-app/.next/'
    --exclude '/apps/readest-app/out/'
    --exclude '/apps/readest-app/public/vendor/'
  )
  # Files the overlay owns: never mirror or delete them, so the overlay below can
  # leave unchanged ones untouched and keep the Next.js build cache valid.
  for file in "${override_files[@]}"; do
    rsync_args+=(--exclude "/apps/readest-app/$file")
  done
  rsync "${rsync_args[@]}" "$source/" "$build/"
else
  # No rsync (the slim Bun builder image in `Dockerfile`): fall back to a
  # from-scratch copy. Nothing carries over between image builds anyway, and the
  # Docker context already excludes `node_modules` via `.dockerignore`.
  rm -rf "$build"
  mkdir -p "$buildroot"
  cp -a "$source" "$buildroot/"
  rm -rf "$build/.git" "$build/.gitmodules" \
    "$build/node_modules" \
    "$build/apps/readest-app/node_modules" \
    "$build/packages/foliate-js/node_modules" \
    "$build/packages/js-mdict/node_modules" \
    "$build/packages/simplecc-wasm/node_modules"
fi

# Overlay the personal changes, preserving timestamps and skipping files that
# already match so an unchanged override does not invalidate the build cache.
for file in "${override_files[@]}"; do
  target="$build/apps/readest-app/$file"
  if ! cmp -s "$overrides/$file" "$target"; then
    mkdir -p "$(dirname "$target")"
    cp -p "$overrides/$file" "$target"
  fi
done

bun install --cwd "$build" --no-save

mkdir -p "$build/apps/readest-app/public/vendor/pdfjs" "$build/apps/readest-app/public/vendor/simplecc"
cp -p "$build/packages/foliate-js/node_modules/pdfjs-dist/legacy/build/pdf.min.mjs" \
  "$build/packages/foliate-js/node_modules/pdfjs-dist/legacy/build/pdf.worker.min.mjs" \
  "$build/apps/readest-app/public/vendor/pdfjs/"
cp -p "$build/packages/foliate-js/node_modules/pdfjs-dist/wasm/"* "$build/apps/readest-app/public/vendor/pdfjs/"
cp -p "$build/packages/foliate-js/vendor/pdfjs/"*.css "$build/apps/readest-app/public/vendor/pdfjs/"
cp -p "$build/packages/simplecc-wasm/dist/web/"* "$build/apps/readest-app/public/vendor/simplecc/"
