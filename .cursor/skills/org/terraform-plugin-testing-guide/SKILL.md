---
name: terraform-plugin-testing-guide
description: Apply HashiCorp Terraform provider testing guidance to all resources/datasources.
metadata:
  surfaces:
    - ide
---

When the user asks to “add tests”, “set up testing”, “ensure test coverage”, or “apply the testing guide to all resources/datasources” in this repo:

1. Follow the HashiCorp Terraform Plugin Testing split:
   - **Unit tests**: isolated, no network connections, run with `go test ./...` (the current repo state).
   - **Acceptance tests**: real plan/apply/refresh/destroy via `terraform-plugin-testing` (optional; requires `TF_ACC=1` + `ORCHESTRA_API_KEY`).

2. Apply the workflow to **every current Terraform resource and data source** in this provider:
   - Resources (create/update/delete Terraform resources):
     - `internal/resources/pipeline/pipeline_resource.go`
   - Data sources (read-only data sources):
     - `internal/datasources/assets/assets_data_source.go`
     - `internal/datasources/azure_federated_credentials/azure_federated_credentials_data_source.go`
     - `internal/datasources/operations/operations_data_source.go`
     - `internal/datasources/pipeline_run_status/pipeline_run_status_data_source.go`
     - `internal/datasources/pipeline_runs/pipeline_runs_data_source.go`
     - `internal/datasources/pipelines/pipelines_data_source.go`
     - `internal/datasources/task_run_artifacts/task_run_artifacts_data_source.go`
     - `internal/datasources/task_run_logs/task_run_logs_data_source.go`
     - `internal/datasources/task_runs/task_runs_data_source.go`

3. Ensure the repo stays aligned with codegen:
   - If schema/type tests are missing for a package, regenerate provider code + framework-generated tests using the existing OpenAPI/codegen sync flow.
   - Then verify via `go test ./...`.

## Implementation

### 0) Preconditions / signals
- This repo currently runs **unit tests only** (no acceptance tests yet).
- Provider tests should run locally with **no network access**.

### 1) Unit test verification (always)
Run:
```bash
go test ./...
```

Optional: run a single package while iterating:
```bash
go test ./internal/datasources/pipelines/...
```

### 2) Ensure unit tests exist for every resource/data source
For each package listed in “When the user asks …”:

1. Check whether that package has generated test coverage (look for `*_gen_test.go` under the same directory).
2. If missing or incomplete:
   - Regenerate generated code + any framework-generated unit tests by invoking the existing “OpenAPI codegen sync” workflow:
     - regenerate `generator_config.yml`
     - regenerate `provider_code_spec.json`
     - run `tfplugingen-framework generate all`
     - sync the generated output into `internal/...` via `scripts/sync_codegen_internal_layout.py`
3. Re-run `go test`:
   - first for the specific package
   - then for the whole repo (`go test ./...`)

### 3) Acceptance tests (optional, only if explicitly requested)
If the user explicitly asks for acceptance tests (real plan/apply against Orchestra’s API):

1. Require:
   - `TF_ACC=1`
   - `ORCHESTRA_API_KEY` set
2. Add a `PreCheck` that fails fast when `ORCHESTRA_API_KEY` is empty:
```go
PreCheck: func() {
  if os.Getenv("ORCHESTRA_API_KEY") == "" {
    t.Fatal("ORCHESTRA_API_KEY must be set for acceptance tests")
  }
},
```
3. Create `*_acc_test.go` tests using the `terraform-plugin-testing` framework and the conventional `TestAcc<Resource>_<scenario>` naming.
4. Implement scenarios per package:
   - For resources: create/update/read/verify attributes.
   - For data sources: configure the provider + query the data source + verify state values.
5. Verify by running only acceptance tests:
```bash
TF_ACC=1 ORCHESTRA_API_KEY=... go test ./... -run TestAcc -count=1
```

## Notes
- The HashiCorp testing guide emphasizes unit tests for isolated helpers (schema flatten/expand), and acceptance tests for end-to-end Terraform behavior.
- This repo’s current README describes it as **unit tests only**; acceptance tests should be introduced only when requested and after you confirm the correct credentials setup.

