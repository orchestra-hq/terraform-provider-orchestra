---
name: openapi-codegen-sync
description: Regenerate all provider code from `openapi.yaml` spec.
metadata:
  surfaces:
    - ide
---

When the user asks to “update the actual generated code from `openapi.yaml`”,
"sync generated resources/datasources", or “add support for all generated resources/datasources”:

1. Regenerate `generator_config.yml` from `openapi.yaml`
2. Regenerate `provider_code_spec.json`
3. Run `tfplugingen-framework` to regenerate all Go code
4. Sync framework output into `internal/...` layout via `scripts/sync_codegen_internal_layout.py`
5. Run `go test ./...` to verify compilation/unit tests

## Implementation

### 0) Inputs (env vars, optional)
- `OPENAPI_PATH` (default: `openapi.yaml`)
- `GO_CODEGEN_TMP_DIR` (default: `.tmp-tfplugingen-framework-go`)

### 1) Regenerate `generator_config.yml`

Run:

```bash
python3 scripts/generate_generator_config.py --openapi "${OPENAPI_PATH:-openapi.yaml}" --out generator_config.yml
```

### 2) Generate `provider_code_spec.json`

Run:

```bash
go install github.com/hashicorp/terraform-plugin-codegen-openapi/cmd/tfplugingen-openapi@v0.3.0
$(go env GOPATH)/bin/tfplugingen-openapi generate \
  --config generator_config.yml \
  --output provider_code_spec.json \
  "${OPENAPI_PATH:-openapi.yaml}"
```

### 3) Generate Go code (framework)

Run:

```bash
go install github.com/hashicorp/terraform-plugin-codegen-framework/cmd/tfplugingen-framework@latest
rm -rf "${GO_CODEGEN_TMP_DIR:-.tmp-tfplugingen-framework-go}"
mkdir -p "${GO_CODEGEN_TMP_DIR:-.tmp-tfplugingen-framework-go}"
$(go env GOPATH)/bin/tfplugingen-framework generate all \
  --input provider_code_spec.json \
  --output "${GO_CODEGEN_TMP_DIR:-.tmp-tfplugingen-framework-go}"
python3 scripts/sync_codegen_internal_layout.py \
  --src "${GO_CODEGEN_TMP_DIR:-.tmp-tfplugingen-framework-go}" \
  --repo-root .
```

### 4) Verify

Run:

```bash
go test ./...
```

## Notes
- This operation overwrites generated code under `internal/...` and test files.
- It is designed to make `internal/datasources/*` and `internal/resources/*` match everything supported by `openapi.yaml` via the codegen pipeline.
