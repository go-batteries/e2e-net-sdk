#!/usr/bin/env python3
"""Apply a declarative patch file to a raw E2E OpenAPI spec.

E2E's published specs have real authoring bugs (undeclared path params,
integer-typed schemas with string enum values, a self-referential oneOf
shape) that break codegen. Rather than hand-editing JSON or hardcoding
fixes in throwaway scripts, every known bug is recorded in
scripts/patches/*.json with a bug description and a fix type. This script
is the only thing that applies them, so re-running it against a refreshed
spec download is a one-line command, and every fix is auditable in git.

Usage:
    python3 scripts/fix_spec.py specs/e2e.openapi.json scripts/patches/myaccount.json specs/e2e.openapi.fixed.json
"""
import json
import sys


def apply_patch(spec, patch):
    op = spec["paths"][patch["path"]][patch["method"]]

    fix = patch["fix"]

    if fix == "reclassify_to_path":
        for p in op["parameters"]:
            if p["name"] == patch["param"]:
                p["in"] = "path"
                return
        raise ValueError(f"param {patch['param']} not found on {patch['method']} {patch['path']}")

    if fix == "reclassify_to_query":
        for p in op["parameters"]:
            if p["name"] == patch["param"]:
                p["in"] = "query"
                return
        raise ValueError(f"param {patch['param']} not found on {patch['method']} {patch['path']}")

    if fix == "add_path_param":
        op.setdefault("parameters", [])
        if any(p["name"] == patch["param"] for p in op["parameters"]):
            return  # already present, nothing to do
        op["parameters"].insert(0, {
            "name": patch["param"],
            "in": "path",
            "required": True,
            "description": f"{patch['param']} (added by fix_spec.py: {patch['bug']})",
            "schema": patch["schema"],
        })
        return

    if fix == "retype_schema":
        for p in op["parameters"]:
            if p["name"] == patch["param"]:
                p["schema"]["type"] = patch["new_type"]
                return
        raise ValueError(f"param {patch['param']} not found on {patch['method']} {patch['path']}")

    if fix == "retype_schema_by_index":
        op["parameters"][patch["param_index"]]["schema"]["type"] = patch["new_type"]
        return

    if fix == "simplify_body_array_enum":
        body_schema = op["requestBody"]["content"]["application/json"]["schema"]
        body_schema["properties"][patch["param"]] = patch["new_schema"]
        return

    raise ValueError(f"unknown fix type: {fix}")


def main():
    if len(sys.argv) != 4:
        print(__doc__)
        sys.exit(1)

    src_path, patch_path, out_path = sys.argv[1:4]

    spec = json.load(open(src_path, encoding="utf-8"))
    patches = json.load(open(patch_path, encoding="utf-8"))

    for patch in patches:
        apply_patch(spec, patch)

    json.dump(spec, open(out_path, "w", encoding="utf-8"))
    print(f"applied {len(patches)} patches from {patch_path} -> {out_path}")


if __name__ == "__main__":
    main()
