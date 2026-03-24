#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUNDLE_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
IMAGES_DIR="${BUNDLE_ROOT}/images"

if [[ ! -d "${IMAGES_DIR}" ]]; then
  echo "Images directory not found: ${IMAGES_DIR}" >&2
  exit 1
fi

shopt -s nullglob
image_files=("${IMAGES_DIR}"/*.tar)
shopt -u nullglob

if [[ ${#image_files[@]} -eq 0 ]]; then
  echo "No image tar files found in ${IMAGES_DIR}" >&2
  exit 1
fi

for image_file in "${image_files[@]}"; do
  echo "Loading ${image_file}"
  docker load -i "${image_file}"
done

echo "All images loaded."
