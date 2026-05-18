#!/usr/bin/env sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
INTERNAL_CONTRACTS_ROOT="$ROOT/../internal-contracts"
PATH="$(go env GOPATH)/bin:$PATH"

rm -rf "$ROOT/gen/go"
mkdir -p "$ROOT/gen/go"

protoc -I "$ROOT/proto" -I "$INTERNAL_CONTRACTS_ROOT/browserautomation/proto" \
  --go_out="$ROOT/gen/go" \
  --go_opt=paths=source_relative \
  --go-grpc_out="$ROOT/gen/go" \
  --go-grpc_opt=paths=source_relative \
  "$ROOT/proto/byte/v/forge/browserautomation/internal/v1/browser_automation_internal.proto"
