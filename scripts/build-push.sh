#!/usr/bin/env sh
set -eu

PREFIX="${IMAGE_PREFIX:-stylle}"
IMAGE="${PREFIX}/today-random-movie-web"
VERSION="$(git rev-parse --short HEAD)"
PLATFORMS="${PLATFORMS:-linux/amd64,linux/arm64}"

echo "Building ${IMAGE}:${VERSION} for ${PLATFORMS}"
docker buildx build \
  --platform "${PLATFORMS}" \
  --tag "${IMAGE}:${VERSION}" \
  --tag "${IMAGE}:latest" \
  --provenance=mode=max \
  --sbom=true \
  --push .

echo "Published ${IMAGE}:${VERSION} and ${IMAGE}:latest"
