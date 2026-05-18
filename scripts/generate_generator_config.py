import argparse
import os
import re
from dataclasses import dataclass
from typing import Any, Dict, Optional, Tuple, List, Set


def to_terraform_identifier(name: str) -> str:
    # Mirrors hashicorp/terraform-plugin-codegen-openapi identifier normalization:
    # 1. Remove all characters that are NOT alphanumeric or underscore
    # 2. Remove all leading numbers
    # 3. Insert underscore between lowercase letter followed by uppercase letter
    # 4. Lowercase
    s = re.sub(r"[^0-9A-Za-z_]", "", name)
    s = re.sub(r"^[0-9]+", "", s)
    s = re.sub(r"([a-z])([A-Z])", r"\1_\2", s)
    return s.lower()


def camel_to_snake(s: str) -> str:
    # Handles already-snake-ish strings too.
    s = to_terraform_identifier(s)
    return s


@dataclass(frozen=True)
class OperationRef:
    operation_id: str
    method: str
    path: str
    request_body_schema: Optional[dict]
    response_schema: Optional[dict]
    path_params: List[str]


def get_response_schema(operation: dict) -> Optional[dict]:
    responses = operation.get("responses") or {}
    # Try 200/201 first, then first available 2xx (lexicographic).
    candidates: List[Tuple[str, dict]] = []
    for code in ("200", "201"):
        if code in responses:
            candidates.append((code, responses[code]))
    if not candidates:
        for code in sorted(responses.keys()):
            if re.match(r"^2\d\d$", str(code)):
                candidates.append((code, responses[code]))
                if candidates:
                    break
    if not candidates:
        return None

    _code, resp = candidates[0]
    content = (resp or {}).get("content") or {}
    # Prefer application/json, otherwise first content-type with schema.
    if "application/json" in content:
        schema = (((content or {}).get("application/json") or {}).get("schema")) or None
        return schema
    for _ct, media in content.items():
        schema = (media or {}).get("schema")
        if schema is not None:
            return schema
    return None


def get_request_body_schema(operation: dict) -> Optional[dict]:
    rb = operation.get("requestBody")
    if not rb:
        return None
    content = rb.get("content") or {}
    if "application/json" in content:
        return (((content.get("application/json") or {}).get("schema")) or None)
    for _ct, media in content.items():
        schema = (media or {}).get("schema")
        if schema is not None:
            return schema
    return None


def resolve_ref(schema: Any, components: dict) -> Optional[dict]:
    if not isinstance(schema, dict):
        return None
    ref = schema.get("$ref")
    if not ref or not isinstance(ref, str):
        return schema if isinstance(schema, dict) else None
    # Support only local refs we expect in this project.
    if not ref.startswith("#/components/schemas/"):
        return None
    name = ref.split("/")[-1]
    return (components.get("schemas") or {}).get(name)


def extract_object_properties(schema: Optional[dict], components: dict) -> Set[str]:
    # Returns terraform-identiferized property names found at the root of an object schema.
    if schema is None:
        return set()

    schema = resolve_ref(schema, components) or schema
    if not isinstance(schema, dict):
        return set()

    # Handle wrappers.
    if "allOf" in schema and isinstance(schema["allOf"], list):
        props: Set[str] = set()
        for s in schema["allOf"]:
            props |= extract_object_properties(s, components)
        return props

    if "anyOf" in schema and isinstance(schema["anyOf"], list):
        props = set()
        for s in schema["anyOf"]:
            # Union all possible root properties.
            props |= extract_object_properties(s, components)
        return props

    # Arrays: inspect items.
    if schema.get("type") == "array":
        return extract_object_properties(schema.get("items"), components)

    if schema.get("type") != "object":
        # Sometimes schemas omit `type` but define properties.
        if "properties" not in schema:
            return set()

    properties = schema.get("properties") or {}
    out: Set[str] = set()
    for k in properties.keys():
        out.add(to_terraform_identifier(k))
    return out


def has_pagination_properties(schema: Optional[dict], components: dict) -> bool:
    if schema is None:
        return False
    schema = resolve_ref(schema, components) or schema
    if not isinstance(schema, dict):
        return False
    if "allOf" in schema and isinstance(schema["allOf"], list):
        # If any subschema looks like it, treat as paginated.
        return any(has_pagination_properties(s, components) for s in schema["allOf"])
    if "anyOf" in schema and isinstance(schema["anyOf"], list):
        return any(has_pagination_properties(s, components) for s in schema["anyOf"])
    if schema.get("type") == "array":
        return has_pagination_properties(schema.get("items"), components)
    if schema.get("type") != "object" and "properties" not in schema:
        return False
    props = schema.get("properties") or {}
    keys = set(props.keys())
    # OAS may use pageSize/camelCase; generator_config uses page_size.
    return ("page" in keys) or ("pageSize" in keys) or ("total" in keys)


def pagination_ignore_list(schema: Optional[dict], components: dict) -> List[str]:
    if not has_pagination_properties(schema, components):
        return []
    schema = resolve_ref(schema, components) or schema
    if not isinstance(schema, dict):
        return []
    if schema.get("type") == "array":
        return pagination_ignore_list(schema.get("items"), components)
    props = schema.get("properties") or {}
    ignore: List[str] = []
    # Convert keys to terraform identifiers.
    for k in props.keys():
        if to_terraform_identifier(k) in ("page", "page_size", "total"):
            ignore.append(to_terraform_identifier(k))
    # Stable order matching current hand-written file.
    ordered = []
    for x in ("page", "page_size", "total"):
        if x in ignore:
            ordered.append(x)
    # Fall back to whatever we found.
    return ordered or ignore


def extract_path_params(path_item: dict, path_template: str) -> List[str]:
    # Only path templated params count: {alias}, {pipeline_id}, etc.
    return re.findall(r"\{([^}]+)\}", path_template)


def collect_operations(openapi: dict) -> Dict[str, List[OperationRef]]:
    components = openapi.get("components") or {}
    schemas = components.get("schemas") or {}
    _ = schemas  # keep lint happy

    ops: Dict[str, List[OperationRef]] = {}
    for path, methods in (openapi.get("paths") or {}).items():
        if not isinstance(methods, dict):
            continue
        for method, operation in methods.items():
            if method.lower() not in ("get", "post", "put", "patch", "delete"):
                continue
            if not isinstance(operation, dict):
                continue
            operation_id = operation.get("operationId")
            if not operation_id:
                continue

            request_body_schema = get_request_body_schema(operation)
            response_schema = get_response_schema(operation)
            path_params = extract_path_params(methods, path)
            ref = OperationRef(
                operation_id=str(operation_id),
                method=method.upper(),
                path=path,
                request_body_schema=request_body_schema,
                response_schema=response_schema,
                path_params=path_params,
            )
            ops.setdefault(str(operation_id), []).append(ref)
    return ops


def choose_single_operation(ops_by_id: Dict[str, List[OperationRef]], operation_id: str) -> Optional[OperationRef]:
    lst = ops_by_id.get(operation_id) or []
    if not lst:
        return None
    # Prefer the first (there should normally be exactly one).
    return lst[0]


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--openapi", default="openapi.yaml")
    parser.add_argument("--out", default="generator_config.yml")
    parser.add_argument(
        "--preserve-existing",
        action="store_true",
        default=True,
        help="Preserve manual schema ignores/aliases already present in --out.",
    )
    parser.add_argument(
        "--no-preserve-existing",
        dest="preserve_existing",
        action="store_false",
        help="Do not preserve manual schema ignores/aliases from existing --out.",
    )
    args = parser.parse_args()

    try:
        import yaml  # type: ignore
    except Exception as e:
        raise SystemExit(
            "Missing dependency 'pyyaml'. Install with: pip install pyyaml"
        ) from e

    with open(args.openapi, "r", encoding="utf-8") as f:
        openapi = yaml.safe_load(f)

    components = openapi.get("components") or {}

    # Provider name:
    # Prefer to use `info.title` (e.g. "Orchestra API") -> "orchestra".
    info = openapi.get("info") or {}
    title = (info.get("title") or "provider").strip()
    provider_name = camel_to_snake(title)
    if provider_name.endswith("_api"):
        provider_name = provider_name[: -len("_api")]

    ops_by_id = collect_operations(openapi)

    resources: Dict[str, Dict[str, Any]] = {}
    data_sources: Dict[str, Dict[str, Any]] = {}

    def add_resource(name: str, create: Optional[OperationRef], read: Optional[OperationRef], update: Optional[OperationRef], delete: Optional[OperationRef], import_variant: bool) -> None:
        # Build schema overrides.
        schema: Dict[str, Any] = {}
        # For this provider, schema ignores are optional and are commonly used to
        # drop non-settable response-only fields. Those are intentionally omitted
        # here to keep generation robust/stable across spec evolutions.
        #
        # We still include pagination ignores when they are clearly represented
        # (page/page_size/total), because those are generally safe and useful.
        if read is not None:
            pagination_ignores = pagination_ignore_list(read.response_schema, components)
            if pagination_ignores:
                schema["ignores"] = pagination_ignores

        # Aliases: map any path-param identifiers used in update/delete to `id`.
        aliases: Dict[str, str] = {}
        for op in [update, delete]:
            if op is None:
                continue
            for p in op.path_params:
                aliases[p] = "id"
        if aliases:
            schema.setdefault("attributes", {})["aliases"] = aliases

        res_entry: Dict[str, Any] = {}
        if create is not None:
            res_entry["create"] = {"path": create.path, "method": create.method}
        if read is not None:
            res_entry["read"] = {"path": read.path, "method": read.method}
        if update is not None:
            res_entry["update"] = {"path": update.path, "method": update.method}
        if delete is not None:
            res_entry["delete"] = {"path": delete.path, "method": delete.method}
        if schema:
            res_entry["schema"] = schema

        resources[name] = res_entry

    # Plain resources: create/update/delete all exist.
    # Repo-specific convention:
    # Only the "Pipelines" domain is represented as Terraform resources in this provider.
    pipeline_create = choose_single_operation(ops_by_id, "createPipeline")
    pipeline_read = choose_single_operation(ops_by_id, "listPipelines")
    pipeline_update = choose_single_operation(ops_by_id, "updatePipeline")
    pipeline_delete = choose_single_operation(ops_by_id, "deletePipeline")
    if pipeline_create and pipeline_read and pipeline_update and pipeline_delete:
        add_resource("pipeline", pipeline_create, pipeline_read, pipeline_update, pipeline_delete, import_variant=False)

    # Data sources follow the OpenAPI operation IDs directly.
    # (These map to list/get operations but are not managed as Terraform resources.)
    def add_data_source(name: str, op: Optional[OperationRef]) -> None:
        if op is None:
            return
        entry: Dict[str, Any] = {"read": {"path": op.path, "method": op.method}}
        schema: Dict[str, Any] = {}

        ignores = pagination_ignore_list(op.response_schema, components)
        if ignores:
            schema["ignores"] = ignores

        # Alias path params to `id` only when there is a single identifier param.
        # Endpoints like `/.../{pipeline_run_id}/.../{task_run_id}/...` would
        # otherwise generate duplicate `id` aliases.
        if op.path_params and len(op.path_params) == 1:
            schema.setdefault("attributes", {})["aliases"] = {op.path_params[0]: "id"}

        if schema:
            entry["schema"] = schema
        data_sources[name] = entry

    add_data_source("pipeline_runs", choose_single_operation(ops_by_id, "listPipelineRuns"))
    add_data_source("pipeline_run_status", choose_single_operation(ops_by_id, "getPipelineRunStatus"))
    add_data_source("task_runs", choose_single_operation(ops_by_id, "listTaskRuns"))
    add_data_source("task_run_artifacts", choose_single_operation(ops_by_id, "listTaskRunArtifacts"))
    add_data_source("task_run_logs", choose_single_operation(ops_by_id, "listTaskRunLogs"))
    add_data_source("operations", choose_single_operation(ops_by_id, "listOperations"))
    add_data_source("assets", choose_single_operation(ops_by_id, "listAssets"))
    add_data_source("pipelines", choose_single_operation(ops_by_id, "listPipelines"))
    add_data_source("azure_federated_credentials", choose_single_operation(ops_by_id, "getAzureFederatedCredentials"))

    # Preserve hand-tuned overrides from the current generator_config.yml so
    # we don't accidentally remove schema.ignores / schema.attributes.aliases.
    if args.preserve_existing and os.path.exists(args.out):
        try:
            with open(args.out, "r", encoding="utf-8") as f:
                existing_cfg = yaml.safe_load(f) or {}
        except Exception:
            existing_cfg = {}

        existing_resources = existing_cfg.get("resources") or {}
        existing_data_sources = existing_cfg.get("data_sources") or {}

        for rname, r_entry in resources.items():
            if rname not in existing_resources:
                continue
            existing_schema = (existing_resources.get(rname) or {}).get("schema") or {}
            if not existing_schema:
                continue
            # Keep any existing manual schema tuning.
            r_entry.setdefault("schema", {})
            if "ignores" in existing_schema:
                r_entry["schema"]["ignores"] = existing_schema["ignores"]
            if "attributes" in existing_schema:
                r_entry["schema"]["attributes"] = existing_schema["attributes"]

        for ds_name, ds_entry in data_sources.items():
            if ds_name not in existing_data_sources:
                continue
            existing_schema = (existing_data_sources.get(ds_name) or {}).get("schema") or {}
            if not existing_schema:
                continue
            ds_entry.setdefault("schema", {})
            if "ignores" in existing_schema:
                ds_entry["schema"]["ignores"] = existing_schema["ignores"]
            if "attributes" in existing_schema:
                ds_entry["schema"]["attributes"] = existing_schema["attributes"]

    # Ensure stable output order: provider/resources/data_sources sorted by key.
    def render_mapping(d: Dict[str, Any], indent: int = 0) -> str:
        lines: List[str] = []
        pad = " " * indent
        for key in sorted(d.keys()):
            val = d[key]
            if isinstance(val, dict):
                lines.append(f"{pad}{key}:")
                lines.append(render_mapping(val, indent + 2))
            elif isinstance(val, list):
                lines.append(f"{pad}{key}:")
                for item in val:
                    lines.append(f"{pad}  - {item}")
            else:
                # Strings/numbers/bools
                if isinstance(val, bool):
                    sval = "true" if val else "false"
                else:
                    sval = str(val)
                lines.append(f"{pad}{key}: {sval}")
        return "\n".join(lines)

    # Manual YAML rendering (to keep it simple and deterministic).
    lines: List[str] = []
    lines.append("provider:")
    lines.append(f"  name: {provider_name}")
    lines.append("resources:")
    for rname in sorted(resources.keys()):
        r = resources[rname]
        lines.append(f"  {rname}:")
        for section in ["create", "read", "update", "delete", "schema"]:
            if section not in r:
                continue
            if section == "schema":
                lines.append(f"    schema:")
                # schema keys: ignores + attributes
                if "ignores" in r["schema"]:
                    ignores = r["schema"]["ignores"]
                    lines.append(f"      ignores:")
                    for item in ignores:
                        lines.append(f"        - {item}")
                if "attributes" in r["schema"]:
                    lines.append("      attributes:")
                    aliases = (r["schema"]["attributes"] or {}).get("aliases") or {}
                    if aliases:
                        lines.append("        aliases:")
                        for a_k in sorted(aliases.keys()):
                            lines.append(f"          {a_k}: {aliases[a_k]}")
            else:
                op = r[section]
                lines.append(f"    {section}:")
                lines.append(f"      path: {op['path']}")
                lines.append(f"      method: {op['method']}")

    lines.append("data_sources:")
    for ds_name in sorted(data_sources.keys()):
        ds = data_sources[ds_name]
        lines.append(f"  {ds_name}:")
        read = ds["read"]
        lines.append("    read:")
        lines.append(f"      path: {read['path']}")
        lines.append(f"      method: {read['method']}")
        if "schema" in ds:
            lines.append("    schema:")
            schema = ds["schema"]
            if "ignores" in schema:
                lines.append("      ignores:")
                for item in schema["ignores"]:
                    lines.append(f"        - {item}")
            if "attributes" in schema:
                lines.append("      attributes:")
                aliases = (schema["attributes"] or {}).get("aliases") or {}
                if aliases:
                    lines.append("        aliases:")
                    for a_k in sorted(aliases.keys()):
                        lines.append(f"          {a_k}: {aliases[a_k]}")

    out = "\n".join(lines) + "\n"
    with open(args.out, "w", encoding="utf-8") as f:
        f.write(out)


if __name__ == "__main__":
    main()

