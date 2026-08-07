#!/bin/sh
set -eu

image=$1
limit=$((30 * 1024 * 1024))
base=${image%@*}
index=$(docker buildx imagetools inspect "$image" --raw)

for architecture in amd64 arm64; do
  digest=$(printf '%s' "$index" | jq -r --arg arch "$architecture" '.manifests[] | select(.platform.architecture == $arch and .platform.os == "linux") | .digest' | head -n 1)
  test -n "$digest"
  manifest=$(docker buildx imagetools inspect "$base@$digest" --raw)
  bytes=$(printf '%s' "$manifest" | jq '[.config.size, (.layers[]?.size)] | add')
  printf '%s compressed bytes: %s\n' "$architecture" "$bytes"
  test "$bytes" -lt "$limit"
done
