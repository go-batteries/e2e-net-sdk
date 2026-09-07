#!/usr/bin/env bash
# Regenerate every Go module in this repo from the raw specs in specs/.
#
# Pipeline per API (myaccount, tir):
#   1. fix_spec.py   patches known spec bugs, declaratively, from scripts/patches/*.json
#   2. split_spec.py splits the fixed spec into one openapi.json per module,
#                     using the classifier in scripts/classify_service.py
#   3. oapi-codegen   generates client.gen.go for every module that doesn't
#                     already have a go.mod (first run only -- go.mod is
#                     hand-owned once created, never regenerated)
#   4. gofmt / go build / go vet each module
#
# Run this after downloading a fresh spec from E2E:
#   cp ~/Downloads/e2e.openapi.json specs/e2e.openapi.json
#   cp ~/Downloads/openapi.json     specs/e2e-tir.openapi.json
#   ./scripts/generate.sh
set -euo pipefail
cd "$(dirname "$0")/.."

export PATH="$HOME/.local/go/bin:$HOME/go/bin:$PATH"
if ! command -v oapi-codegen >/dev/null 2>&1; then
  echo "oapi-codegen not found. Install it with:"
  echo "  go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest"
  exit 1
fi

generate_one() {
  local kind="$1" raw="$2" patches="$3" fixed="$4" outdir="$5"

  echo "== $kind: patching $raw =="
  python3 scripts/fix_spec.py "$raw" "$patches" "$fixed"

  echo "== $kind: splitting into modules =="
  python3 scripts/split_spec.py "$kind" "$fixed" "$outdir"

  for dir in "$outdir"/*/; do
    bucket=$(basename "$dir")
    spec="$dir/openapi.json"
    [ -f "$spec" ] || continue

    if [ ! -f "$dir/go.mod" ]; then
      echo "== $kind/$bucket: new module, writing go.mod =="
      cat > "$dir/go.mod" <<EOF
module github.com/go-batteries/e2e-net-sdk/$outdir$bucket

go 1.24.0

require github.com/oapi-codegen/runtime v1.7.0
EOF
    fi

    echo "== $kind/$bucket: generating client =="
    oapi-codegen -generate types,client -package "$bucket" -o "$dir/client.gen.go" "$spec"
    gofmt -l -w "$dir/client.gen.go"

    (cd "$dir" && go mod tidy && go build ./... && go vet ./...)
  done
}

generate_one myaccount specs/e2e.openapi.json scripts/patches/myaccount.json specs/e2e.openapi.fixed.json myaccount/
generate_one tir specs/e2e-tir.openapi.json scripts/patches/tir.json specs/e2e-tir.openapi.fixed.json tir/

echo "done."
