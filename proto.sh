#!/bin/bash

set -eou pipefail

shopt -s globstar

find_all_proto_files() {
  find "${PROTO_DIR}" -name "*.proto" -type f
}

PROTO_DIR="${1:-pkg/proto}"
OUT_DIR="${2:-pkg/pb}"

# Clean previously generated files.
rm -rf "${OUT_DIR:?}"/* && \
  mkdir -p "${OUT_DIR:?}"

# Step 1: Generate Go code for ALL proto files
protoc \
  -I "${PROTO_DIR}" \
  --go_out="${OUT_DIR}" \
  --go_opt=paths=source_relative \
  --go-grpc_out="${OUT_DIR}" \
  --go-grpc_opt=paths=source_relative,require_unimplemented_servers=false \
  $(find_all_proto_files)
