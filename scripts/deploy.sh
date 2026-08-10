#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
project_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)

cd "$project_dir"

if ! command -v docker >/dev/null 2>&1; then
  echo "error: docker is not installed or not on PATH" >&2
  exit 1
fi

echo "Pulling the latest Bookshelf image..."
docker compose pull bookshelf

echo "Starting Bookshelf..."
docker compose up -d bookshelf

container_id=$(docker compose ps -q bookshelf)
image_ref=$(docker inspect --format '{{.Config.Image}}' "$container_id")
image_id=$(docker inspect --format '{{.Image}}' "$container_id")
image_digest=$(docker image inspect --format '{{join .RepoDigests "\n"}}' "$image_id" | sed -n '1p')

echo "Deployment complete."
running_version=$(docker compose run --rm --no-deps bookshelf version)
echo "Image: $image_ref"
echo "Version: $running_version"
echo "Image ID: $image_id"
if [ -n "$image_digest" ]; then
  echo "Image digest: $image_digest"
fi
