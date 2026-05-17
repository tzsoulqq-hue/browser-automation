#!/usr/bin/env sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
CONTRACTS_ROOT="$ROOT/../contracts"
PATH="$(go env GOPATH)/bin:$PATH"

if [ -f "$CONTRACTS_ROOT/scripts/generate-proto.sh" ]; then
  (cd "$CONTRACTS_ROOT" && sh scripts/generate-proto.sh)
fi

mkdir -p "$ROOT/gen/go"

protoc -I "$ROOT/proto" -I "$CONTRACTS_ROOT/browserautomation/proto" \
  --go_out="$ROOT/gen/go" \
  --go_opt=paths=source_relative \
  --go-grpc_out="$ROOT/gen/go" \
  --go-grpc_opt=paths=source_relative \
  "$ROOT/proto/byte/v/forge/browserautomation/internal/v1/browser_automation_internal.proto"
