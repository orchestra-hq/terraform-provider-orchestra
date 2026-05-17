#!/usr/bin/env python3

"""
Sync Terraform codegen output into a conventional internal/ layout.

This repo is generated-code heavy (tfplugingen-framework). The framework generator
emits into top-level folders like `datasource_pipelines/` and `resource_pipeline/`.

CI/workflows should generate into a temporary directory, then call this script to:
1) move generated directories into `internal/...`
2) rewrite Go `package ...` declarations to match the new package names
3) rewrite `_test.go` imports to point at the new internal package paths
"""

from __future__ import annotations

import argparse
import re
import shutil
from pathlib import Path


DEFAULT_MODULE_PATH = "github.com/orchestra-hq/terraform-provider-orchestra"


def read_module_path(repo_root: Path) -> str:
    go_mod = repo_root / "go.mod"
    m = re.search(r"^module\s+(.+?)\s*$", go_mod.read_text(encoding="utf-8"), flags=re.M)
    return (m.group(1).strip() if m else DEFAULT_MODULE_PATH)


# Mapping: codegen output dir (old) -> internal target dir + Go package name.
#
# Note: Go package name does not have to match the directory name, but we keep it
# consistent/idiomatic by aligning with the mapping `new_package`.
MAPPINGS: list[dict[str, str]] = [
    {"old_dir": "provider_orchestra", "new_dir": "internal/provider", "new_package": "provider"},
    {"old_dir": "resource_pipeline", "new_dir": "internal/resources/pipeline", "new_package": "pipeline"},
    {
        "old_dir": "resource_imported_pipeline",
        "new_dir": "internal/resources/imported_pipeline",
        "new_package": "imported_pipeline",
    },
    # Datasources
    {"old_dir": "datasource_assets", "new_dir": "internal/datasources/assets", "new_package": "assets"},
    {
        "old_dir": "datasource_azure_federated_credentials",
        "new_dir": "internal/datasources/azure_federated_credentials",
        "new_package": "azure_federated_credentials",
    },
    {"old_dir": "datasource_operations", "new_dir": "internal/datasources/operations", "new_package": "operations"},
    {
        "old_dir": "datasource_pipeline_run_status",
        "new_dir": "internal/datasources/pipeline_run_status",
        "new_package": "pipeline_run_status",
    },
    {
        "old_dir": "datasource_pipeline_runs",
        "new_dir": "internal/datasources/pipeline_runs",
        "new_package": "pipeline_runs",
    },
    {"old_dir": "datasource_pipelines", "new_dir": "internal/datasources/pipelines", "new_package": "pipelines"},
    {
        "old_dir": "datasource_task_run_artifacts",
        "new_dir": "internal/datasources/task_run_artifacts",
        "new_package": "task_run_artifacts",
    },
    {
        "old_dir": "datasource_task_run_logs",
        "new_dir": "internal/datasources/task_run_logs",
        "new_package": "task_run_logs",
    },
    {"old_dir": "datasource_task_runs", "new_dir": "internal/datasources/task_runs", "new_package": "task_runs"},
]


def update_go_files(
    target_dir: Path,
    *,
    old_package: str,
    new_package: str,
    old_import: str,
    new_import: str,
) -> None:
    # Regex to replace `package xyz` (Go `package` clause appears once per file).
    package_re = re.compile(r"^package\s+([a-zA-Z0-9_]+)\s*$", flags=re.M)

    for go_file in target_dir.rglob("*.go"):
        text = go_file.read_text(encoding="utf-8")

        # Update package clause depending on test/non-test.
        if go_file.name.endswith("_test.go"):
            desired_pkg = f"{new_package}_test"
            # Current test package is `<old_dir>_test` (per existing generated files).
            # Replace whatever package clause exists in that file, but only if it
            # matches the expected old test package.
            text = re.sub(
                rf"^package\s+{re.escape(old_package)}_test\s*$",
                f"package {desired_pkg}",
                text,
                flags=re.M,
            )
        else:
            text = re.sub(
                rf"^package\s+{re.escape(old_package)}\s*$",
                f"package {new_package}",
                text,
                flags=re.M,
            )

        # Update test imports so the types resolve to internal packages.
        # These imports only exist in `_test.go` files in this repo today, but
        # the replacement is safe if it appears elsewhere.
        if old_import in text:
            text = text.replace(old_import, new_import)

        # Rewrite references in tests to match the imported package identifier.
        # When Go imports a package without an explicit alias, the identifier in
        # code is the imported package's declared name (the `package ...`
        # clause). Since we also rewrite `package` clauses in moved code,
        # test files must be updated to call the new package name.
        if go_file.name.endswith("_test.go") and f"{old_package}." in text:
            text = text.replace(f"{old_package}.", f"{new_package}.")

        # Update any remaining `package <old_package>_test` or `package <old_package>` occurrences
        # if we missed the exact regex above (idempotency/future regen safety).
        # Do not update unrelated packages.
        if f"package {old_package}_test" in text and go_file.name.endswith("_test.go"):
            text = text.replace(f"package {old_package}_test", f"package {new_package}_test")
        if f"package {old_package}\n" in text and not go_file.name.endswith("_test.go"):
            text = text.replace(f"package {old_package}\n", f"package {new_package}\n")

        # If package declarations still mismatch, surface it with a helpful message.
        if go_file.name.endswith("_test.go") and f"package {new_package}_test" not in text:
            # Avoid noisy failures: only error when an old package clause is still present.
            if f"package {old_package}_test" in text:
                raise RuntimeError(f"Failed to rewrite test package in {go_file}")
        if not go_file.name.endswith("_test.go") and f"package {new_package}" not in text:
            if f"package {old_package}" in text:
                raise RuntimeError(f"Failed to rewrite package in {go_file}")

        go_file.write_text(text, encoding="utf-8")


def sync(src_root: Path, repo_root: Path) -> None:
    module_path = read_module_path(repo_root)

    for m in MAPPINGS:
        old_dir = m["old_dir"]
        new_dir = m["new_dir"]
        new_package = m["new_package"]

        src_pkg_dir = src_root / old_dir
        target_dir = repo_root / new_dir
        target_dir_parent = target_dir.parent
        target_dir_parent.mkdir(parents=True, exist_ok=True)

        # If source codegen output still exists at `src_root/<old_dir>`, move it.
        # Otherwise, if `target_dir` already exists, just rewrite files in-place.
        if src_pkg_dir.exists():
            # Clean target to keep diffs deterministic and prevent stale files.
            if target_dir.exists():
                shutil.rmtree(target_dir)

            shutil.move(str(src_pkg_dir), str(target_dir))
        else:
            if not target_dir.exists():
                # In regen runs, not every mapping may be present if generator output changes.
                continue

        old_package = old_dir
        old_import = f"{module_path}/{old_dir}"
        new_import = f"{module_path}/{new_dir}"

        update_go_files(
            target_dir,
            old_package=old_package,
            new_package=new_package,
            old_import=old_import,
            new_import=new_import,
        )


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--src",
        default=".",
        help="Directory containing the codegen output folders (e.g. '.' or '.tmp-...').",
    )
    parser.add_argument(
        "--repo-root",
        default=".",
        help="Repo root containing go.mod and the destination internal/ directory.",
    )
    args = parser.parse_args()

    src_root = Path(args.src).resolve()
    repo_root = Path(args.repo_root).resolve()

    sync(src_root, repo_root)


if __name__ == "__main__":
    main()

