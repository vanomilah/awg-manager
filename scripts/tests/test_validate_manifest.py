#!/usr/bin/env python3
import json
import os
import subprocess
import sys
import tempfile
import unittest

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "../.."))
VALIDATOR_SCRIPT = os.path.join(REPO_ROOT, "scripts", "validate_manifest.py")


def run_validator(manifest_path: str, env_extra: dict = None):
    env = os.environ.copy()
    if env_extra:
        env.update(env_extra)
    res = subprocess.run(
        [sys.executable, VALIDATOR_SCRIPT, manifest_path],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        env=env,
    )
    return res.returncode, res.stdout, res.stderr.decode("utf-8", errors="replace")


def make_valid_manifest_dict():
    return {
        "version": "v1.19.29",
        "binary": {
            "assetName": "mihomo-linux-amd64.gz",
            "url": "https://example.com/mihomo.gz",
            "compressedSha256": "a" * 64,
            "uncompressedSha256": "b" * 64,
        },
        "geodata": {
            "geoip.dat": {
                "url": "https://example.com/geoip.dat",
                "sha256": "c" * 64,
            },
            "geosite.dat": {
                "url": "https://example.com/geosite.dat",
                "sha256": "d" * 64,
            },
            "ASN.mmdb": {
                "url": "https://example.com/ASN.mmdb",
                "sha256": "e" * 64,
            },
        },
    }


class TestValidateManifest(unittest.TestCase):
    def setUp(self):
        self.tmp_dir = tempfile.TemporaryDirectory()

    def tearDown(self):
        self.tmp_dir.cleanup()

    def write_json(self, data):
        path = os.path.join(self.tmp_dir.name, "manifest.json")
        with open(path, "w", encoding="utf-8") as f:
            json.dump(data, f)
        return path

    def write_raw(self, content: str):
        path = os.path.join(self.tmp_dir.name, "manifest.raw")
        with open(path, "w", encoding="utf-8") as f:
            f.write(content)
        return path

    def test_valid_standard_manifest(self):
        m = make_valid_manifest_dict()
        path = self.write_json(m)
        code, stdout, stderr = run_validator(path)
        self.assertEqual(code, 0, f"Expected 0, got {code}. stderr: {stderr}")
        fields = stdout.split(b"\0")
        # stdout ends with \0, so split gives 12 items with last empty
        self.assertEqual(len(fields), 12)
        self.assertEqual(fields[-1], b"")
        self.assertEqual(fields[0].decode(), "v1.19.29")
        self.assertEqual(fields[1].decode(), "mihomo-linux-amd64.gz")
        self.assertEqual(fields[2].decode(), "https://example.com/mihomo.gz")
        self.assertEqual(fields[3].decode(), "a" * 64)
        self.assertEqual(fields[4].decode(), "b" * 64)
        self.assertEqual(fields[5].decode(), "https://example.com/geoip.dat")
        self.assertEqual(fields[6].decode(), "c" * 64)
        self.assertEqual(fields[7].decode(), "https://example.com/geosite.dat")
        self.assertEqual(fields[8].decode(), "d" * 64)
        self.assertEqual(fields[9].decode(), "https://example.com/ASN.mmdb")
        self.assertEqual(fields[10].decode(), "e" * 64)

    def test_valid_no_v_prefix(self):
        m = make_valid_manifest_dict()
        m["version"] = "1.19.29"
        path = self.write_json(m)
        code, stdout, stderr = run_validator(path)
        self.assertEqual(code, 0, stderr)
        self.assertTrue(stdout.startswith(b"1.19.29\0"))

    def test_valid_prerelease(self):
        m = make_valid_manifest_dict()
        m["version"] = "v1.19.29-beta.1"
        path = self.write_json(m)
        code, stdout, stderr = run_validator(path)
        self.assertEqual(code, 0, stderr)
        self.assertTrue(stdout.startswith(b"v1.19.29-beta.1\0"))

    def test_valid_zero_version(self):
        m = make_valid_manifest_dict()
        m["version"] = "v0.1.0"
        path = self.write_json(m)
        code, stdout, stderr = run_validator(path)
        self.assertEqual(code, 0, stderr)

    def test_valid_http_with_flag(self):
        m = make_valid_manifest_dict()
        m["binary"]["url"] = "http://127.0.0.1:8080/mihomo.gz"
        path = self.write_json(m)
        code, stdout, stderr = run_validator(path, env_extra={"ACCEPTANCE_ALLOW_HTTP": "1"})
        self.assertEqual(code, 0, stderr)

    def test_invalid_http_without_flag(self):
        m = make_valid_manifest_dict()
        m["binary"]["url"] = "http://127.0.0.1:8080/mihomo.gz"
        path = self.write_json(m)
        code, stdout, stderr = run_validator(path, env_extra={"ACCEPTANCE_ALLOW_HTTP": "0"})
        self.assertEqual(code, 1)
        self.assertIn("binary.url", stderr)
        self.assertIn("URL scheme must be https", stderr)

    def test_invalid_root_types(self):
        for bad in [[], "string", 123, None, True]:
            path = self.write_json(bad)
            code, _, stderr = run_validator(path)
            self.assertEqual(code, 1, f"Failed on {bad}")
            self.assertIn("root", stderr)

    def test_invalid_json_syntax(self):
        path = self.write_raw("{not valid json")
        code, _, stderr = run_validator(path)
        self.assertEqual(code, 1)
        self.assertIn("root", stderr)

    def test_invalid_versions(self):
        bad_versions = [
            ("", "empty or whitespace"),
            ("   ", "empty or whitespace"),
            (123, "expected string"),
            (None, "expected string"),
            ("v01.19.29", "does not match awgm-version format"),
            ("v1.019.29", "does not match awgm-version format"),
            ("v1.19.029", "does not match awgm-version format"),
            ("v1.19.29+build1", "does not match awgm-version format"),
            ("v1.19/29", "does not match awgm-version format"),
            ("v1.19\\29", "does not match awgm-version format"),
            ("v1.19..29", "does not match awgm-version format"),
            ("../../etc", "does not match awgm-version format"),
        ]
        for v, expected_msg in bad_versions:
            m = make_valid_manifest_dict()
            m["version"] = v
            path = self.write_json(m)
            code, _, stderr = run_validator(path)
            self.assertEqual(code, 1, f"Failed on version {v!r}")
            self.assertIn("version", stderr)
            self.assertIn(expected_msg, stderr)

    def test_invalid_asset_names(self):
        bad_assets = [
            ".",
            "..",
            "foo/bar",
            "foo\\bar",
            "/bar",
            "bar/",
            "foo\x00bar",
            "foo\x01bar",
            "",
            "   ",
        ]
        for asset in bad_assets:
            m = make_valid_manifest_dict()
            m["binary"]["assetName"] = asset
            path = self.write_json(m)
            code, _, stderr = run_validator(path)
            self.assertEqual(code, 1, f"Failed on assetName {asset!r}")
            self.assertIn("binary.assetName", stderr)

    def test_invalid_urls(self):
        bad_urls = [
            ("file:///etc/passwd", "scheme must be https"),
            ("ftp://example.com/file", "scheme must be https"),
            ("https://", "URL host cannot be empty"),
            ("https://user:pass@example.com/bin", "URL credentials are forbidden"),
            ("https://example.com/bin#fragment", "URL fragments are forbidden"),
            ("https://example.com:not-a-port/bin", "invalid port in URL"),
            ("https://example.com:70000/bin", "invalid port in URL"),
            ("https://example.com/bin with space", "URL contains whitespace or control characters"),
            ("https://example.com/bin\nnewline", "URL contains whitespace or control characters"),
            ("https://[::1/bin", "Invalid IPv6 URL"),
            ("https://[]:8080/bin", "does not appear to be an IPv4 or IPv6 address"),
            (" https://example.com/bin", "URL cannot have leading or trailing whitespace"),
            ("https://example.com/bin ", "URL cannot have leading or trailing whitespace"),
            ("  https://example.com/bin  ", "URL cannot have leading or trailing whitespace"),
        ]
        for url, expected_msg in bad_urls:
            m = make_valid_manifest_dict()
            m["binary"]["url"] = url
            path = self.write_json(m)
            code, _, stderr = run_validator(path)
            self.assertEqual(code, 1, f"Failed on url {url!r}")
            self.assertIn("binary.url", stderr)
            self.assertIn(expected_msg, stderr)

    def test_invalid_sha(self):
        bad_shas = [
            "a" * 63,  # too short
            "a" * 65,  # too long
            "g" * 64,  # non hex
            "",        # empty
            "   ",     # whitespace
        ]
        for sha in bad_shas:
            m = make_valid_manifest_dict()
            m["binary"]["compressedSha256"] = sha
            path = self.write_json(m)
            code, _, stderr = run_validator(path)
            self.assertEqual(code, 1, f"Failed on sha {sha!r}")
            self.assertIn("binary.compressedSha256", stderr)

    def test_invalid_geodata(self):
        m = make_valid_manifest_dict()
        del m["geodata"]["geoip.dat"]
        path = self.write_json(m)
        code, _, stderr = run_validator(path)
        self.assertEqual(code, 1)
        self.assertIn("geodata.geoip.dat", stderr)

        m = make_valid_manifest_dict()
        m["geodata"]["ASN.mmdb"] = "not a dict"
        path = self.write_json(m)
        code, _, stderr = run_validator(path)
        self.assertEqual(code, 1)
        self.assertIn("geodata.ASN.mmdb", stderr)


if __name__ == "__main__":
    unittest.main()
