#!/usr/bin/env python3
import json
import os
import re
import sys
import urllib.parse

VERSION_RE = re.compile(r"^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$")
SHA256_RE = re.compile(r"^[0-9a-fA-F]{64}$")


def fail(path: str, msg: str):
    sys.stderr.write(f"ERROR: Invalid manifest at '{path}': {msg}\n")
    sys.exit(1)


def validate_str(val, path: str) -> str:
    if not isinstance(val, str):
        fail(path, f"expected string, got {type(val).__name__}")
    s = val.strip()
    if not s:
        fail(path, "string cannot be empty or whitespace-only")
    return s


def validate_version(val, path: str) -> str:
    s = validate_str(val, path)
    if not VERSION_RE.match(s):
        fail(path, f"version '{s}' does not match awgm-version format")
    return s


def validate_asset_name(val, path: str) -> str:
    s = validate_str(val, path)
    if s in (".", "..") or "/" in s or "\\" in s or "\x00" in s or any(ord(c) < 32 for c in s):
        fail(path, f"assetName '{s}' contains invalid characters or is not a clean basename")
    return s


def validate_sha(val, path: str) -> str:
    s = validate_str(val, path)
    if not SHA256_RE.match(s):
        fail(path, f"invalid sha256 checksum '{s}'")
    return s.lower()


def validate_url(val, path: str, allow_http: bool) -> str:
    if not isinstance(val, str):
        fail(path, f"expected string, got {type(val).__name__}")
    if val != val.strip():
        fail(path, "URL cannot have leading or trailing whitespace")
    s = val
    if not s:
        fail(path, "string cannot be empty or whitespace-only")
    if any(c.isspace() or ord(c) < 32 or ord(c) == 127 for c in s):
        fail(path, "URL contains whitespace or control characters")
    try:
        p = urllib.parse.urlparse(s)
    except Exception as e:
        fail(path, f"failed to parse URL: {e}")
    if allow_http and p.scheme.lower() == "http":
        pass
    elif p.scheme.lower() != "https":
        fail(path, f"URL scheme must be https (got '{p.scheme}')")
    if not p.netloc:
        fail(path, "URL host cannot be empty")
    try:
        _ = p.port
    except ValueError as e:
        fail(path, f"invalid port in URL: {e}")
    if not p.hostname:
        fail(path, "URL host cannot be empty")
    if p.username or p.password:
        fail(path, "URL credentials are forbidden")
    if p.fragment:
        fail(path, "URL fragments are forbidden")
    if "[" in p.netloc or "]" in p.netloc:
        if not (p.netloc.startswith("[") and "]" in p.netloc):
            fail(path, "malformed IPv6 literal in URL authority")
        bracket_content = p.netloc[1 : p.netloc.index("]")]
        if not bracket_content:
            fail(path, "empty IPv6 literal in URL authority")
    return s


def main():
    if len(sys.argv) < 2:
        sys.stderr.write("Usage: validate_manifest.py <manifest.json>\n")
        sys.exit(1)

    allow_http = os.environ.get("ACCEPTANCE_ALLOW_HTTP") == "1"

    try:
        with open(sys.argv[1], "rb") as f:
            data = json.load(f)
    except Exception as e:
        fail("root", f"cannot read/parse JSON: {e}")

    if not isinstance(data, dict):
        fail("root", "manifest root must be a JSON object")

    version = validate_version(data.get("version"), "version")

    bin_obj = data.get("binary")
    if not isinstance(bin_obj, dict):
        fail("binary", "must be a JSON object")
    bin_asset = validate_asset_name(bin_obj.get("assetName"), "binary.assetName")
    bin_url = validate_url(bin_obj.get("url"), "binary.url", allow_http)
    bin_c_sha = validate_sha(bin_obj.get("compressedSha256"), "binary.compressedSha256")
    bin_u_sha = validate_sha(bin_obj.get("uncompressedSha256"), "binary.uncompressedSha256")

    geo_obj = data.get("geodata")
    if not isinstance(geo_obj, dict):
        fail("geodata", "must be a JSON object")

    geo_fields = []
    for item in ("geoip.dat", "geosite.dat", "ASN.mmdb"):
        entry = geo_obj.get(item)
        if not isinstance(entry, dict):
            fail(f"geodata.{item}", "must be a JSON object")
        u = validate_url(entry.get("url"), f"geodata.{item}.url", allow_http)
        s = validate_sha(entry.get("sha256"), f"geodata.{item}.sha256")
        geo_fields.extend([u, s])

    fields = [version, bin_asset, bin_url, bin_c_sha, bin_u_sha] + geo_fields
    for f in fields:
        sys.stdout.buffer.write(f.encode("utf-8") + b"\0")
    sys.stdout.buffer.flush()


if __name__ == "__main__":
    main()
