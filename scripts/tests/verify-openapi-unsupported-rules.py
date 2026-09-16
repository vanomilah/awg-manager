#!/usr/bin/env python3
import os
import sys
import yaml


def main():
    repo_root = os.path.abspath(os.path.join(os.path.dirname(__file__), "../.."))
    swagger_path = os.path.join(repo_root, "internal", "openapi", "swagger.yaml")

    if not os.path.exists(swagger_path):
        sys.stderr.write(f"ERROR: swagger.yaml not found at {swagger_path}\n")
        return 1

    with open(swagger_path, "r", encoding="utf-8") as f:
        spec = yaml.safe_load(f)

    paths = spec.get("paths", {})
    list_path = paths.get("/mihomo/native/rules/unsupported")
    if not list_path or "get" not in list_path:
        sys.stderr.write("ERROR: GET /mihomo/native/rules/unsupported not found in paths\n")
        return 1

    del_path = paths.get("/mihomo/native/rules/unsupported/delete")
    if not del_path or "post" not in del_path:
        sys.stderr.write("ERROR: POST /mihomo/native/rules/unsupported/delete not found in paths\n")
        return 1

    definitions = spec.get("definitions", {})
    req_schema = definitions.get("api.NativeDeleteUnsupportedRulesRequest")
    if not req_schema:
        sys.stderr.write("ERROR: api.NativeDeleteUnsupportedRulesRequest not found in definitions\n")
        return 1

    required = req_schema.get("required", [])
    if "ids" not in required:
        sys.stderr.write("ERROR: 'ids' is not in required fields of NativeDeleteUnsupportedRulesRequest\n")
        return 1
    if "revision" not in required:
        sys.stderr.write("ERROR: 'revision' is not in required fields of NativeDeleteUnsupportedRulesRequest\n")
        return 1

    props = req_schema.get("properties", {})
    ids_prop = props.get("ids", {})
    if ids_prop.get("type") != "array":
        sys.stderr.write(f"ERROR: ids type is {ids_prop.get('type')}, expected 'array'\n")
        return 1

    if ids_prop.get("minItems") != 1:
        sys.stderr.write(f"ERROR: ids minItems is {ids_prop.get('minItems')}, expected 1\n")
        return 1

    if ids_prop.get("uniqueItems") is not True:
        sys.stderr.write(f"ERROR: ids uniqueItems is {ids_prop.get('uniqueItems')}, expected True\n")
        return 1

    items = ids_prop.get("items", {})
    if items.get("type") != "string":
        sys.stderr.write(f"ERROR: ids items type is {items.get('type')}, expected 'string'\n")
        return 1

    if items.get("minLength") != 1:
        sys.stderr.write(f"ERROR: ids items minLength is {items.get('minLength')}, expected 1\n")
        return 1

    revision_prop = props.get("revision", {})
    if revision_prop.get("type") != "string":
        sys.stderr.write(f"ERROR: revision type is {revision_prop.get('type')}, expected 'string'\n")
        return 1

    print("SUCCESS: OpenAPI schema for unsupported rules verified (endpoints, required, minItems: 1, uniqueItems: true, items.minLength: 1).")
    return 0


if __name__ == "__main__":
    sys.exit(main())
