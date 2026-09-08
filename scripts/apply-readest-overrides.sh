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

rm -rf "$buildroot"
mkdir -p "$buildroot"
cp -a "$source" "$buildroot/"
rm -rf "$build/.git" "$build/.gitmodules" \
  "$build/node_modules" \
  "$build/apps/readest-app/node_modules" \
  "$build/packages/foliate-js/node_modules" \
  "$build/packages/js-mdict/node_modules" \
  "$build/packages/simplecc-wasm/node_modules"

while IFS= read -r file; do
  target="$build/apps/readest-app/$file"
  mkdir -p "$(dirname "$target")"
  cp "$overrides/$file" "$target"
done < <(cd "$overrides" && find . -type f -print | sort | sed 's#^\./##')

mkdir -p "$build/apps/readest-app/public/vendor/pdfjs" "$build/apps/readest-app/public/vendor/simplecc"
bun install --cwd "$build" --no-save
cp "$build/packages/foliate-js/node_modules/pdfjs-dist/legacy/build/pdf.min.mjs" \
  "$build/packages/foliate-js/node_modules/pdfjs-dist/legacy/build/pdf.worker.min.mjs" \
  "$build/apps/readest-app/public/vendor/pdfjs/"
cp "$build/packages/foliate-js/node_modules/pdfjs-dist/wasm/"* "$build/apps/readest-app/public/vendor/pdfjs/"
cp "$build/packages/foliate-js/vendor/pdfjs/"*.css "$build/apps/readest-app/public/vendor/pdfjs/"
cp "$build/packages/simplecc-wasm/dist/web/"* "$build/apps/readest-app/public/vendor/simplecc/"
