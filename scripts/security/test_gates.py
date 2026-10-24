import copy
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import licenses


class LicenseGateTests(unittest.TestCase):
    def setUp(self):
        self.policy = licenses.load_policy(
            licenses.ROOT / "scripts/security/license-policy.json")

    def test_spdx(self):
        allowed = set(self.policy["allowed"])
        for value in ["MIT", "MIT AND ISC", "(MPL-2.0 OR Apache-2.0)"]:
            self.assertTrue(licenses.expression_allowed(value, allowed), value)
        for value in ["UNKNOWN", "GPL-3.0-only", "MIT AND GPL-3.0-only",
                      "MIT OR", "MIT OR (GPL-3.0-only", "MIT WITH unknown",
                      "MIT arbitrary", "MIT*"]:
            self.assertFalse(licenses.expression_allowed(value, allowed), value)

    def test_negative_records(self):
        for value in [None, "", "UNKNOWN", "NOASSERTION", "UNLICENSED", "GPL-3.0-only"]:
            with self.subTest(license=value), self.assertRaises(ValueError):
                licenses.check([("npm", "fixture", "1.0.0", value)], self.policy)
        with self.assertRaises(ValueError):
            licenses.check([], self.policy)

    def test_exception_is_exact_and_requires_reason(self):
        item = self.policy["exceptions"][0]
        record = tuple(item[k] for k in ("ecosystem", "name", "version", "license"))
        licenses.check([record], self.policy)
        for index, value in [(0, "go"), (1, "other"), (2, "99.0.0"), (3, "UNKNOWN")]:
            changed = list(record)
            changed[index] = value
            with self.assertRaises(ValueError):
                licenses.check([tuple(changed)], self.policy)
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "policy.json"
            for key, value in [("reason", ""), ("name", "*"), ("version", "*"),
                               ("license", "UNKNOWN")]:
                invalid = copy.deepcopy(self.policy)
                invalid["exceptions"][0][key] = value
                path.write_text(json.dumps(invalid))
                with self.assertRaises(ValueError):
                    licenses.load_policy(path)

    def test_locked_and_downloaded_npm(self):
        with tempfile.TemporaryDirectory() as directory:
            web = Path(directory)
            package = web / "node_modules/fixture"
            package.mkdir(parents=True)
            lock = {"lockfileVersion": 3, "packages": {
                "": {}, "node_modules/fixture": {"version": "1.0.0"}}}
            (web / "package-lock.json").write_text(json.dumps(lock))
            (package / "package.json").write_text(json.dumps(
                {"name": "fixture", "version": "1.0.0", "license": "MIT"}))
            licenses.check(licenses.npm_records(web, True), self.policy)
            (package / "package.json").unlink()
            with self.assertRaises(ValueError):
                licenses.check(licenses.npm_records(web, True), self.policy)
            # Optional packages cannot silently disappear from the lock inventory.
            lock["packages"]["node_modules/fixture"]["optional"] = True
            (web / "package-lock.json").write_text(json.dumps(lock))
            with self.assertRaises(ValueError):
                licenses.check(licenses.npm_records(web, True), self.policy)
            (package / "package.json").write_text(json.dumps(
                {"name": "fixture", "version": "2.0.0", "license": "MIT"}))
            with self.assertRaises(ValueError):
                licenses.npm_records(web, True)

    def test_registry_failure_is_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            web = Path(directory)
            (web / "package-lock.json").write_text(json.dumps({
                "lockfileVersion": 3, "packages": {
                    "": {}, "node_modules/fixture": {"version": "1.0.0"}}}))
            with patch("urllib.request.urlopen", side_effect=OSError("network fixture")):
                with self.assertRaises(OSError):
                    licenses.npm_records(web)

    def test_go_report_coverage_and_unknown(self):
        inventory = json.dumps({"ImportPath": "example.com/fixture/pkg",
                                "Module": {"Path": "example.com/fixture",
                                           "Version": "v1.0.0"}})
        for report in ["", "malformed\n", "example.com/fixture,url,Unknown\n"]:
            with patch.object(licenses, "run", side_effect=[inventory, report]):
                with self.assertRaises(ValueError):
                    licenses.check(licenses.go_records(licenses.ROOT), self.policy)
        with patch.object(licenses, "run", side_effect=[
                inventory, "example.com/fixture,url,MIT\n"]):
            licenses.check(licenses.go_records(licenses.ROOT), self.policy)
        with patch.object(licenses, "run", side_effect=ValueError("tool failed")):
            with self.assertRaises(ValueError):
                licenses.go_records(licenses.ROOT)


class GitleaksTests(unittest.TestCase):
    def test_worktree_negative_fixtures(self):
        binary = shutil.which("gitleaks")
        if not binary:
            if os.environ.get("CI") == "true":
                self.fail("CI must install the pinned gitleaks binary")
            self.skipTest("gitleaks unavailable locally; not a successful scan")
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            target = root / "core/storage/sigv4_test.go"
            target.parent.mkdir(parents=True)
            # Assemble fixtures at runtime so the gate's own source is clean.
            known = "AKIA" + "W0AZ82CEIYN8O9EZ"
            fresh = "AKIA" + "Q7M2N9P4R6S8T3V5"
            config = licenses.ROOT / ".gitleaks.toml"

            def scan(expected):
                result = subprocess.run([
                    binary, "dir", ".", "--config", str(config), "--redact",
                    "--ignore-gitleaks-allow", "--gitleaks-ignore-path", "/dev/null",
                    "--exit-code", "1"], cwd=str(root), text=True,
                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30)
                self.assertEqual(result.returncode, expected, result.stderr)

            target.write_text('aws_access_key_id = "' + known + '"\n')
            scan(0)
            target.write_text('aws_access_key_id = "' + fresh + '"\n')
            scan(1)  # New secret in the allowlisted test path still blocks.
            target.write_text("")
            (root / "another_test.go").write_text(
                'aws_access_key_id = "' + known + '"\n')
            scan(1)  # Known vector outside the exact path still blocks.
            (root / "another_test.go").write_text("")
            # A different secret in a test directory must also block.
            tests = root / "tests"
            tests.mkdir()
            key = subprocess.run(["openssl", "genrsa", "-traditional", "2048"],
                                 capture_output=True, text=True, timeout=15, check=True)
            (tests / "private.pem").write_text(key.stdout)
            scan(1)


if __name__ == "__main__":
    unittest.main()
