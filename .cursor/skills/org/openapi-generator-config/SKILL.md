---
name: openapi-generator-config
description: Convert `openapi.yaml` to this repo's `generator_config.yml`
metadata:
  surfaces:
    - ide
---

When the user asks to “convert `openapi.yaml` to `generator_config.yml`” in this repo:

## References
- [Mapping OAS to provider code specification](https://github.com/hashicorp/terraform-plugin-codegen-openapi/blob/main/DESIGN.md#mapping-oas-to-provider-code-specification) — upstream rules for `generator_config.yml`
- [Terraform Provider Code Specification](https://github.com/hashicorp/terraform-plugin-codegen-spec) — target format for `provider_code_spec.json`

1. Use `OPENAPI_PATH` default `openapi.yaml` and `OUT_PATH` default `generator_config.yml` (repo root).
2. Regenerate `OUT_PATH` by running the embedded Python below.
3. The Python code will:
   - parse `openapi.yaml` to verify the known endpoint path+method pairs exist
   - preserve the existing `schema` blocks from the current `generator_config.yml` (if present)
   - update only `resources/*/*.{path,method}` and `data_sources/*/read.{path,method}`
   - remove accidental `resources.imported_pipeline` if it exists

## Implementation

Run:

```bash
python3 - "$OPENAPI_PATH" "$OUT_PATH" <<'PY'
import sys
import yaml

openapi_path = sys.argv[1]
out_path = sys.argv[2]

with open(openapi_path, "r", encoding="utf-8") as f:
    openapi = yaml.safe_load(f) or {}

with open(out_path, "r", encoding="utf-8") as f:
    cfg = yaml.safe_load(f) or {}


def ensure_endpoint(oas: dict, path: str, method: str) -> None:
    paths = oas.get("paths") or {}
    p = paths.get(path)
    if not p or method.lower() not in p:
        raise SystemExit(f"openapi.yaml missing endpoint: {method.upper()} {path}")


def set_method(d: dict, method: str) -> None:
    d["method"] = method.upper()


def set_path_method(d: dict, path: str, method: str) -> None:
    d["path"] = path
    set_method(d, method)


# Target mapping in this provider
targets = {
    "resources": {
        "assets": {
            "create": ("/assets", "POST"),
            "read": ("/assets/{asset_id}", "GET"),
            "update": ("/assets/{asset_id}", "PATCH"),
            "delete": ("/assets/{asset_id}", "DELETE"),
        },
        "pipeline": {
            "create": ("/pipelines", "POST"),
            "read": ("/pipelines", "GET"),
            "update": ("/pipelines/{alias}", "PUT"),
            "delete": ("/pipelines/{alias}", "DELETE"),
        },
        "integrations": {
            "create": ("/state/{integration}", "PATCH"),
            "read": ("/integrations/{integration}", "GET"),
            "update": ("/state/{integration}", "PATCH"),
        },
    },
    "data_sources": {
        "assets": {
            "read": ("/assets", "GET"),
        },
        "pipelines": {
            "read": ("/pipelines", "GET"),
        },
        "integrations": {
            "read": ("/integrations/", "GET"),
        },
        "pipeline_runs": {
            "read": ("/pipeline_runs", "GET"),
        },
        "task_runs": {
            "read": ("/task_runs", "GET"),
        },
        "azure_federated_credentials": {
            "read": ("/compute_resources/azure/federated_credentials", "GET"),
        },
        "operations": {
            "read": ("/operations", "GET"),
        },
    },
}


# Verify endpoints exist in openapi.yaml
for _, top in targets.items():
    for _, group in top.items():
        for _, (p, m) in group.items():
            ensure_endpoint(openapi, p, m)


# Ensure top-level keys exist
cfg.setdefault("provider", {})
cfg.setdefault("resources", {})
cfg.setdefault("data_sources", {})

# Drop accidental keys
cfg["resources"].pop("imported_pipeline", None)


def apply_mapping(kind: str, key: str, ops: dict) -> None:
    cfg[kind].setdefault(key, {})
    for op_name, (p, m) in ops.items():
        cfg[kind][key].setdefault(op_name, {})
        set_path_method(cfg[kind][key][op_name], p, m)


for resource_name, ops in targets["resources"].items():
    apply_mapping("resources", resource_name, ops)

for ds_name, ops in targets["data_sources"].items():
    apply_mapping("data_sources", ds_name, ops)


with open(out_path, "w", encoding="utf-8") as f:
    yaml.safe_dump(cfg, f, sort_keys=False)
PY
```

Embedded python (writes `OUT_PATH`):

```python
import sys
import yaml

openapi_path = sys.argv[1]
out_path = sys.argv[2]

with open(openapi_path, "r", encoding="utf-8") as f:
    openapi = yaml.safe_load(f) or {}

with open(out_path, "r", encoding="utf-8") as f:
    cfg = yaml.safe_load(f) or {}


def ensure_endpoint(oas: dict, path: str, method: str) -> None:
    paths = oas.get("paths") or {}
    p = paths.get(path)
    if not p or method.lower() not in p:
        raise SystemExit(f"openapi.yaml missing endpoint: {method.upper()} {path}")


def set_method(d: dict, method: str) -> None:
    d["method"] = method.upper()


def set_path_method(d: dict, path: str, method: str) -> None:
    d["path"] = path
    set_method(d, method)


# Target mapping in this provider
targets = {
    "resources": {
        "assets": {
            "create": ("/assets", "POST"),
            "read": ("/assets/{asset_id}", "GET"),
            "update": ("/assets/{asset_id}", "PATCH"),
            "delete": ("/assets/{asset_id}", "DELETE"),
        },
        "pipeline": {
            "create": ("/pipelines", "POST"),
            "read": ("/pipelines", "GET"),
            "update": ("/pipelines/{alias}", "PUT"),
            "delete": ("/pipelines/{alias}", "DELETE"),
        },
        "integrations": {
            "create": ("/state/{integration}", "PATCH"),
            "read": ("/integrations/{integration}", "GET"),
            "update": ("/state/{integration}", "PATCH"),
        },
    },
    "data_sources": {
        "assets": {
            "read": ("/assets", "GET"),
        },
        "pipelines": {
            "read": ("/pipelines", "GET"),
        },
        "integrations": {
            "read": ("/integrations/", "GET"),
        },
        "pipeline_runs": {
            "read": ("/pipeline_runs", "GET"),
        },
        "task_runs": {
            "read": ("/task_runs", "GET"),
        },
        "azure_federated_credentials": {
            "read": ("/compute_resources/azure/federated_credentials", "GET"),
        },
        "operations": {
            "read": ("/operations", "GET"),
        },
    },
}


# Verify endpoints exist in openapi.yaml
for _, top in targets.items():
    for _, group in top.items():
        for _, (p, m) in group.items():
            ensure_endpoint(openapi, p, m)


# Ensure top-level keys exist
cfg.setdefault("provider", {})
cfg.setdefault("resources", {})
cfg.setdefault("data_sources", {})

# Drop accidental keys
cfg["resources"].pop("imported_pipeline", None)


def apply_mapping(kind: str, key: str, ops: dict) -> None:
    cfg[kind].setdefault(key, {})
    for op_name, (p, m) in ops.items():
        cfg[kind][key].setdefault(op_name, {})
        set_path_method(cfg[kind][key][op_name], p, m)


for resource_name, ops in targets["resources"].items():
    apply_mapping("resources", resource_name, ops)

for ds_name, ops in targets["data_sources"].items():
    apply_mapping("data_sources", ds_name, ops)


with open(out_path, "w", encoding="utf-8") as f:
    yaml.safe_dump(cfg, f, sort_keys=False)
PY
```

---
name: openapi-generator-config
description: >-
  Convert the repo's `openapi.yaml` into `generator_config.yml` for Orchestra
  Terraform codegen, targeting the provider's known resources/data-sources.
metadata:
  surfaces:
    - ide
---

# OpenAPI to generator_config.yml (targeted)

When the user asks to “convert `openapi.yaml` to `generator_config.yml`”:

1. Determine inputs
   - `OPENAPI_PATH`: default `openapi.yaml` (repo root)
   - `OUT_PATH`: default `generator_config.yml` (repo root)

2. Regenerate `generator_config.yml` by running the embedded Python mapper below.
   - It will:
     - read `openapi.yaml` to locate `{path, method}` for specific `operationId`s
     - read the existing `generator_config.yml` (if present) to preserve `schema` blocks
     - write the required keys:
       - Resources: `assets`, `pipeline`, `integrations`
       - Data Sources: `assets`, `pipelines`, `integrations`, `pipeline_runs`, `task_runs`, `azure_federated_credentials`
     - remove accidental extra integration keys (`integration`, `integration_jobs`) by replacing them with `integrations`
     - hard-skip `imported_pipeline` as a Terraform resource (git-import is handled by the `pipeline` resource at runtime)

3. Verification
   - ensure `OUT_PATH` is written
   - ensure `OUT_PATH` parses as YAML

## Implementation

Run (substitute paths):

```bash
python3 - "$OPENAPI_PATH" "$OUT_PATH" <<'PY'
import sys
import yaml

openapi_path = sys.argv[1]
out_path = sys.argv[2]

with open(openapi_path, "r", encoding="utf-8") as f:
    openapi = yaml.safe_load(f) or {}

with open(out_path, "r", encoding="utf-8") as f:
    cfg = yaml.safe_load(f) or {}


def ensure_endpoint(oas: dict, path: str, method: str) -> None:
    paths = oas.get("paths") or {}
    p = paths.get(path)
    if not p or method.lower() not in p:
        raise SystemExit(f"openapi.yaml missing endpoint: {method.upper()} {path}")


def set_method(d: dict, method: str) -> None:
    d["method"] = method.upper()


def set_path_method(d: dict, path: str, method: str) -> None:
    d["path"] = path
    set_method(d, method)


# Target mapping in this provider
targets = {
    "resources": {
        "assets": {
            "create": ("/assets", "POST"),
            "read": ("/assets/{asset_id}", "GET"),
            "update": ("/assets/{asset_id}", "PATCH"),
            "delete": ("/assets/{asset_id}", "DELETE"),
        },
        "pipeline": {
            "create": ("/pipelines", "POST"),
            "read": ("/pipelines", "GET"),
            "update": ("/pipelines/{alias}", "PUT"),
            "delete": ("/pipelines/{alias}", "DELETE"),
        },
        "integrations": {
            "create": ("/state/{integration}", "PATCH"),
            "read": ("/integrations/{integration}", "GET"),
            "update": ("/state/{integration}", "PATCH"),
        },
    },
    "data_sources": {
        "assets": {
            "read": ("/assets", "GET"),
        },
        "pipelines": {
            "read": ("/pipelines", "GET"),
        },
        "integrations": {
            "read": ("/integrations/", "GET"),
        },
        "pipeline_runs": {
            "read": ("/pipeline_runs", "GET"),
        },
        "task_runs": {
            "read": ("/task_runs", "GET"),
        },
        "azure_federated_credentials": {
            "read": ("/compute_resources/azure/federated_credentials", "GET"),
        },
        "operations": {
            "read": ("/operations", "GET"),
        },
    },
}


# Verify endpoints exist in openapi.yaml
for _, top in targets.items():
    for _, group in top.items():
        for _, (p, m) in group.items():
            ensure_endpoint(openapi, p, m)


# Ensure top-level keys exist
cfg.setdefault("provider", {})
cfg.setdefault("resources", {})
cfg.setdefault("data_sources", {})

# Drop accidental keys
cfg["resources"].pop("imported_pipeline", None)


def apply_mapping(kind: str, key: str, ops: dict) -> None:
    cfg[kind].setdefault(key, {})
    for op_name, (p, m) in ops.items():
        cfg[kind][key].setdefault(op_name, {})
        set_path_method(cfg[kind][key][op_name], p, m)


for resource_name, ops in targets["resources"].items():
    apply_mapping("resources", resource_name, ops)

for ds_name, ops in targets["data_sources"].items():
    apply_mapping("data_sources", ds_name, ops)


with open(out_path, "w", encoding="utf-8") as f:
    yaml.safe_dump(cfg, f, sort_keys=False)
PY
```

Use the embedded Python below.

```python
import re
import sys
from typing import Any, Dict, Optional, Tuple

import yaml

openapi_path = sys.argv[1]
out_path = sys.argv[2]

def normalize_provider_name(s: str) -> str:
    s = s.strip()
    s = re.sub(r"[^0-9A-Za-z_]", "", s)
    s = re.sub(r"^[0-9]+", "", s)
    s = re.sub(r"([a-z])([A-Z])", r"\1_\2", s)
    return s.lower()

def load_yaml_file(path: str) -> Any:
    with open(path, "r", encoding="utf-8") as f:
        return yaml.safe_load(f)

openapi = load_yaml_file(openapi_path)

try:
    existing_cfg = load_yaml_file(out_path) or {}
except FileNotFoundError:
    existing_cfg = {}

components = openapi.get("components") or {}

def find_operation(operation_id: str) -> Tuple[str, str]:
    # Returns (path, method)
    for path, methods in (openapi.get("paths") or {}).items():
        if not isinstance(methods, dict):
            continue
        for method, operation in methods.items():
            if not isinstance(operation, dict):
                continue
            if operation.get("operationId") == operation_id:
                return path, str(method).upper()
    raise RuntimeError(f"operationId not found in openapi.yaml: {operation_id}")

def get_existing_schema(resource_or_ds: Dict[str, Any]) -> Dict[str, Any]:
    # Preserve manual tuning if present.
    schema = resource_or_ds.get("schema")
    if isinstance(schema, dict):
        return schema
    return {}

def set_aliases(schema: Dict[str, Any], aliases: Dict[str, str]) -> None:
    if not aliases:
        return
    schema.setdefault("attributes", {})
    schema["attributes"].setdefault("aliases", {})
    for k, v in aliases.items():
        schema["attributes"]["aliases"][k] = v

def maybe_add_pagination_ignores(schema: Dict[str, Any], enable: bool) -> None:
    # We don't fully parse response schemas here; we keep behavior aligned with your existing generator_config.yml.
    if not enable:
        return
    # Common list pagination keys in your generator_config.yml.
    schema.setdefault("ignores", [])
    for k in ("page", "page_size", "total"):
        if k not in schema["ignores"]:
            schema["ignores"].append(k)

provider_name = (
    (existing_cfg.get("provider") or {}).get("name")
    or normalize_provider_name((openapi.get("info") or {}).get("title") or "provider")
)

existing_resources = existing_cfg.get("resources") or {}
existing_data_sources = existing_cfg.get("data_sources") or {}

# Hard reset accidental keys.
existing_resources.pop("imported_pipeline", None)
existing_data_sources.pop("integration", None)
existing_data_sources.pop("integration_jobs", None)

resources: Dict[str, Any] = dict(existing_resources)
data_sources: Dict[str, Any] = dict(existing_data_sources)

# -----------------------
# Resources
# -----------------------

# Assets resource: createAsset/getAsset/updateAsset/deleteAsset
asset_create_path, asset_create_method = find_operation("createAsset")
asset_read_path, asset_read_method = find_operation("getAsset")
asset_update_path, asset_update_method = find_operation("updateAsset")
asset_delete_path, asset_delete_method = find_operation("deleteAsset")

assets_schema = get_existing_schema(resources.get("assets", {}))
set_aliases(assets_schema, {"asset_id": "id"})
resources["assets"] = {
    "create": {"path": asset_create_path, "method": asset_create_method},
    "read": {"path": asset_read_path, "method": asset_read_method},
    "update": {"path": asset_update_path, "method": asset_update_method},
    "delete": {"path": asset_delete_path, "method": asset_delete_method},
}
if assets_schema:
    resources["assets"]["schema"] = assets_schema

# Pipeline resource: keep existing schema, but update paths/methods from OpenAPI.
pipeline_create_path, pipeline_create_method = find_operation("createPipeline")
pipeline_read_path, pipeline_read_method = find_operation("listPipelines")
pipeline_update_path, pipeline_update_method = find_operation("updatePipeline")
pipeline_delete_path, pipeline_delete_method = find_operation("deletePipeline")

pipeline_schema = get_existing_schema(resources.get("pipeline", {}))
resources["pipeline"] = {
    "create": {"path": pipeline_create_path, "method": pipeline_create_method},
    "read": {"path": pipeline_read_path, "method": pipeline_read_method},
    "update": {"path": pipeline_update_path, "method": pipeline_update_method},
    "delete": {"path": pipeline_delete_path, "method": pipeline_delete_method},
    "schema": pipeline_schema or {},
}

# Integrations resource:
# We don't have a create/delete pair; we use PATCH state update as the "create" and "update",
# and map "read" to GET /integrations/{integration}.
integration_create_path, integration_create_method = find_operation(
    "update_state_public_state__integration__patch"
)
integration_read_path, integration_read_method = find_operation(
    "get_integration_public_integrations__integration__get"
)
integrations_schema = get_existing_schema(resources.get("integrations", {}))
set_aliases(integrations_schema, {"integration": "id"})

resources["integrations"] = {
    "create": {"path": integration_create_path, "method": integration_create_method},
    "read": {"path": integration_read_path, "method": integration_read_method},
    "update": {"path": integration_create_path, "method": integration_create_method},
    # delete intentionally omitted (no delete operation mapped)
}
if integrations_schema:
    resources["integrations"]["schema"] = integrations_schema

# -----------------------
# Data sources
# -----------------------

# Assets data source: listAssets
assets_ds_path, assets_ds_method = find_operation("listAssets")
assets_ds_schema = get_existing_schema(data_sources.get("assets", {}))
maybe_add_pagination_ignores(assets_ds_schema, True)
data_sources["assets"] = {
    "read": {"path": assets_ds_path, "method": assets_ds_method},
    "schema": assets_ds_schema or {},
}

# Pipelines data source: listPipelines
pipelines_ds_path, pipelines_ds_method = find_operation("listPipelines")
pipelines_ds_schema = get_existing_schema(data_sources.get("pipelines", {}))
data_sources["pipelines"] = {
    "read": {"path": pipelines_ds_path, "method": pipelines_ds_method},
    # Keep schema only if present; your current file doesn't include schema for pipelines.
    **({"schema": pipelines_ds_schema} if pipelines_ds_schema else {}),
}

# Integrations data source: get integrations list at /integrations/
integrations_ds_path, integrations_ds_method = find_operation("get_integrations_public_integrations__get")
integrations_ds_schema = get_existing_schema(data_sources.get("integrations", {}))
maybe_add_pagination_ignores(integrations_ds_schema, True)
data_sources["integrations"] = {
    "read": {"path": integrations_ds_path, "method": integrations_ds_method},
    "schema": integrations_ds_schema or {},
}

# Pipeline runs data source: listPipelineRuns (keep existing schema ignores)
pipeline_runs_path, pipeline_runs_method = find_operation("listPipelineRuns")
pipeline_runs_schema = get_existing_schema(data_sources.get("pipeline_runs", {}))
data_sources["pipeline_runs"] = {
    "read": {"path": pipeline_runs_path, "method": pipeline_runs_method},
    "schema": pipeline_runs_schema or {},
}

# Task runs data source: listTaskRuns
task_runs_path, task_runs_method = find_operation("listTaskRuns")
task_runs_schema = get_existing_schema(data_sources.get("task_runs", {}))
data_sources["task_runs"] = {
    "read": {"path": task_runs_path, "method": task_runs_method},
    "schema": task_runs_schema or {},
}

# Azure federated credentials data source
azure_fc_path, azure_fc_method = find_operation("getAzureFederatedCredentials")
azure_fc_schema = get_existing_schema(data_sources.get("azure_federated_credentials", {}))
data_sources["azure_federated_credentials"] = {
    "read": {"path": azure_fc_path, "method": azure_fc_method},
    **({"schema": azure_fc_schema} if azure_fc_schema else {}),
}

# Keep any other existing data sources too (pipeline_run_status, etc.)
for k in list(existing_data_sources.keys()):
    if k not in data_sources:
        data_sources[k] = existing_data_sources[k]

# -----------------------
# Render YAML in your repo style (stable key order)
# -----------------------

def render_mapping(d: Dict[str, Any], indent: int) -> str:
    lines = []
    pad = " " * indent
    for key in d.keys():
        val = d[key]
        if isinstance(val, dict):
            lines.append(f"{pad}{key}:")
            lines.append(render_mapping(val, indent + 2))
        elif isinstance(val, list):
            lines.append(f"{pad}{key}:")
            for item in val:
                lines.append(f"{pad}  - {item}")
        else:
            if isinstance(val, bool):
                sval = "true" if val else "false"
            else:
                sval = str(val)
            lines.append(f"{pad}{key}: {sval}")
    return "\n".join(lines)

out_lines: list[str] = []
out_lines.append("provider:")
out_lines.append(f"  name: {provider_name}")
out_lines.append("resources:")

resource_order = ["assets", "pipeline", "integrations"]
for rname in resource_order:
    if rname not in resources:
        continue
    r = resources[rname]
    out_lines.append(f"  {rname}:")
    # deterministically emit known sections
    for section in ["create", "read", "update", "delete", "schema"]:
        if section not in r:
            continue
        if section == "schema":
            out_lines.append("    schema:")
            schema = r.get("schema") or {}
            if schema.get("ignores"):
                out_lines.append("      ignores:")
                for item in schema["ignores"]:
                    out_lines.append(f"        - {item}")
            if schema.get("attributes"):
                out_lines.append("      attributes:")
                aliases = (schema.get("attributes") or {}).get("aliases") or {}
                if aliases:
                    out_lines.append("        aliases:")
                    for a_k in sorted(aliases.keys()):
                        out_lines.append(f"          {a_k}: {aliases[a_k]}")
        else:
            op = r[section]
            out_lines.append(f"    {section}:")
            out_lines.append(f"      path: {op['path']}")
            out_lines.append(f"      method: {op['method']}")

# Render any remaining resources not listed above.
remaining_resources = sorted(set(resources.keys()) - set(resource_order))
for rname in remaining_resources:
    r = resources[rname]
    out_lines.append(f"  {rname}:")
    for section in ["create", "read", "update", "delete", "schema"]:
        if section not in r:
            continue
        if section == "schema":
            out_lines.append("    schema:")
            schema = r.get("schema") or {}
            if schema.get("ignores"):
                out_lines.append("      ignores:")
                for item in schema["ignores"]:
                    out_lines.append(f"        - {item}")
            if schema.get("attributes"):
                out_lines.append("      attributes:")
                aliases = (schema.get("attributes") or {}).get("aliases") or {}
                if aliases:
                    out_lines.append("        aliases:")
                    for a_k in sorted(aliases.keys()):
                        out_lines.append(f"          {a_k}: {aliases[a_k]}")
        else:
            op = r[section]
            out_lines.append(f"    {section}:")
            out_lines.append(f"      path: {op['path']}")
            out_lines.append(f"      method: {op['method']}")

out_lines.append("data_sources:")

data_source_order = [
    "assets",
    "pipelines",
    "integrations",
    "pipeline_runs",
    "task_runs",
    "azure_federated_credentials",
]

for ds_name in data_source_order:
    if ds_name not in data_sources:
        continue
    ds = data_sources[ds_name]
    out_lines.append(f"  {ds_name}:")
    out_lines.append("    read:")
    out_lines.append(f"      path: {ds['read']['path']}")
    out_lines.append(f"      method: {ds['read']['method']}")
    if "schema" in ds and ds["schema"]:
        out_lines.append("    schema:")
        schema = ds["schema"] or {}
        if schema.get("ignores"):
            out_lines.append("      ignores:")
            for item in schema["ignores"]:
                out_lines.append(f"        - {item}")
        if schema.get("attributes"):
            out_lines.append("      attributes:")
            aliases = (schema.get("attributes") or {}).get("aliases") or {}
            if aliases:
                out_lines.append("        aliases:")
                for a_k in sorted(aliases.keys()):
                    out_lines.append(f"          {a_k}: {aliases[a_k]}")

# Render any remaining data sources not listed above.
remaining_ds = sorted(set(data_sources.keys()) - set(data_source_order))
for ds_name in remaining_ds:
    ds = data_sources[ds_name]
    out_lines.append(f"  {ds_name}:")
    out_lines.append("    read:")
    out_lines.append(f"      path: {ds['read']['path']}")
    out_lines.append(f"      method: {ds['read']['method']}")
    if "schema" in ds and ds["schema"]:
        out_lines.append("    schema:")
        schema = ds.get("schema") or {}
        if schema.get("ignores"):
            out_lines.append("      ignores:")
            for item in schema["ignores"]:
                out_lines.append(f"        - {item}")
        if schema.get("attributes"):
            out_lines.append("      attributes:")
            aliases = (schema.get("attributes") or {}).get("aliases") or {}
            if aliases:
                out_lines.append("        aliases:")
                for a_k in sorted(aliases.keys()):
                    out_lines.append(f"          {a_k}: {aliases[a_k]}")

out_text = "\n".join(out_lines).rstrip() + "\n"
with open(out_path, "w", encoding="utf-8") as f:
    f.write(out_text)
PY
```

