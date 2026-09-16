#!/usr/bin/env python3
import os
import re
import sys
import tempfile
import yaml


def main():
    if len(sys.argv) < 2:
        sys.stderr.write("Usage: patch-openapi.py <swagger.yaml>\n")
        return 1

    target = os.path.abspath(sys.argv[1])
    if not os.path.exists(target):
        sys.stderr.write(f"ERROR: Target file not found: {target}\n")
        return 1

    with open(target, "r", encoding="utf-8") as f:
        content = f.read()

    # Step 1: Validate AST with PyYAML
    try:
        spec = yaml.safe_load(content)
    except Exception as e:
        sys.stderr.write(f"ERROR: Failed to parse YAML at {target}: {e}\n")
        return 1

    if not isinstance(spec, dict):
        sys.stderr.write(f"ERROR: Root of {target} is not a dict\n")
        return 1

    try:
        defs = spec.get("definitions", {})
        req = defs.get("api.NativeDeleteUnsupportedRulesRequest", {})
        props = req.get("properties", {})
        ids_prop = props.get("ids", {})
        items = ids_prop.get("items", {})
        if not isinstance(items, dict):
            raise KeyError("items is not a dict")
        if items.get("type") != "string":
            raise KeyError("items.type is not string")
    except Exception as e:
        sys.stderr.write(f"ERROR: Target schema not found in {target}: {e}\n")
        return 1

    # Idempotent: check if already patched
    if items.get("minLength") == 1:
        print(f"SUCCESS: {target} already has items.minLength: 1")
        return 0

    # Step 2: Surgical regex edit to preserve all swag formatting and quotes across the 500 KB file
    pattern = re.compile(
        r"(  api\.NativeDeleteUnsupportedRulesRequest:\n"
        r"    properties:\n"
        r"      ids:\n"
        r"        items:\n"
        r"          type: string\n)"
    )
    replacement = (
        r"  api.NativeDeleteUnsupportedRulesRequest:\n"
        r"    properties:\n"
        r"      ids:\n"
        r"        items:\n"
        r"          minLength: 1\n"
        r"          type: string\n"
    )

    new_content, count = pattern.subn(replacement, content, count=1)
    if count != 1:
        sys.stderr.write("ERROR: Pattern for api.NativeDeleteUnsupportedRulesRequest not found for text replacement\n")
        return 1

    # Step 3: Re-verify modified AST with PyYAML
    try:
        verify_spec = yaml.safe_load(new_content)
        v_items = verify_spec["definitions"]["api.NativeDeleteUnsupportedRulesRequest"]["properties"]["ids"]["items"]
        if v_items.get("minLength") != 1:
            sys.stderr.write("ERROR: Post-patch AST verification failed: minLength is not 1\n")
            return 1
    except Exception as e:
        sys.stderr.write(f"ERROR: Post-patch YAML verification failed: {e}\n")
        return 1

    # Step 4: Atomic write via tempfile in target directory
    tmp_dir = os.path.dirname(target)
    tmp_name = None
    try:
        with tempfile.NamedTemporaryFile("w", encoding="utf-8", dir=tmp_dir, delete=False) as tmp:
            tmp_name = tmp.name
            tmp.write(new_content)
            tmp.flush()
            os.fsync(tmp.fileno())

        os.replace(tmp_name, target)
        tmp_name = None
    except Exception as e:
        if tmp_name and os.path.exists(tmp_name):
            try:
                os.unlink(tmp_name)
            except Exception:
                pass
        sys.stderr.write(f"ERROR: Failed atomic replace on {target}: {e}\n")
        return 1

    print(f"SUCCESS: Surgically patched {target} with items.minLength: 1")
    return 0


if __name__ == "__main__":
    sys.exit(main())
