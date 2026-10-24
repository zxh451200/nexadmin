#!/usr/bin/env python3
"""Fail-closed license gate. Python standard library only; no package installation."""
import argparse
import csv
import io
import json
from pathlib import Path
import re
import subprocess
import sys
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
UNKNOWN = {"UNKNOWN", "NOASSERTION", "UNLICENSED", "NONE", ""}


def run(args, cwd):
    result = subprocess.run(args, cwd=str(cwd), text=True, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, timeout=600)
    if result.stderr:
        print(result.stderr, file=sys.stderr, end="")
    if result.returncode:
        raise ValueError("{} exited {}".format(args[0], result.returncode))
    return result.stdout


def json_stream(text):
    decoder = json.JSONDecoder()
    while text.strip():
        value, end = decoder.raw_decode(text.lstrip())
        yield value
        text = text.lstrip()[end:]


def expression_allowed(expression, allowed):
    """Small strict SPDX AND/OR parser; WITH and unrecognized syntax fail closed."""
    tokens = re.findall(r"[A-Za-z0-9.+-]+|\(|\)", expression)
    if "".join(tokens) != re.sub(r"\s+", "", expression) or not tokens:
        return False
    position = 0

    def atom():
        nonlocal position
        if position >= len(tokens):
            raise ValueError("incomplete SPDX expression")
        token = tokens[position]
        position += 1
        if token == "(":
            result = disjunction()
            if position >= len(tokens) or tokens[position] != ")":
                raise ValueError("unclosed SPDX expression")
            position += 1
            return result
        if token in {"AND", "OR", "WITH", ")"}:
            raise ValueError("invalid SPDX atom")
        return token in allowed

    def conjunction():
        nonlocal position
        result = atom()
        while position < len(tokens) and tokens[position] == "AND":
            position += 1
            right = atom()
            result = result and right
        return result

    def disjunction():
        nonlocal position
        result = conjunction()
        while position < len(tokens) and tokens[position] == "OR":
            position += 1
            right = conjunction()
            result = result or right
        return result

    try:
        result = disjunction()
        return result and position == len(tokens)
    except ValueError:
        return False


def load_policy(path):
    policy = json.loads(path.read_text())
    allowed = policy["allowed"]
    if not allowed or any(not isinstance(x, str) or x.upper() in UNKNOWN for x in allowed):
        raise ValueError("invalid allowlist")
    seen = set()
    for item in policy["exceptions"]:
        key = (item["ecosystem"], item["name"], item["version"], item["license"])
        if (key in seen or key[0] not in {"go", "npm"}
                or any(not isinstance(x, str) or not x.strip() or "*" in x for x in key)
                or key[3].upper() in UNKNOWN or not item["reason"].strip()):
            raise ValueError("exceptions require exact dependency, version, license and reason")
        seen.add(key)
    return policy


def check(records, policy):
    if not records:
        raise ValueError("empty dependency inventory")
    failures = []
    for ecosystem, name, version, license_name in records:
        label = "{}:{}@{}".format(ecosystem, name, version)
        if not isinstance(license_name, str) or license_name.upper() in UNKNOWN:
            failures.append(label + ": missing/unknown license")
            continue
        if expression_allowed(license_name, set(policy["allowed"])):
            continue
        exception = next((x for x in policy["exceptions"]
                          if (x["ecosystem"], x["name"], x["version"], x["license"])
                          == (ecosystem, name, version, license_name)), None)
        if exception:
            print("EXCEPTION {} [{}]: {}".format(label, license_name, exception["reason"]))
        else:
            failures.append("{}: disallowed license {}".format(label, license_name))
    if failures:
        raise ValueError("\n".join(failures))
    print("License gate passed: {} dependency records".format(len(records)))


def npm_records(web, offline=False):
    lock = json.loads((web / "package-lock.json").read_text())
    if lock.get("lockfileVersion") not in (2, 3) or not lock.get("packages"):
        raise ValueError("npm requires a nonempty v2/v3 package-lock.json")
    records = []
    for location, package in lock["packages"].items():
        if not location:
            continue
        # All locked packages, including dev, optional and other-platform binaries.
        if (package.get("link") or not location.startswith("node_modules/")
                or ".." in Path(location).parts):
            raise ValueError("unsupported npm lock entry: " + location)
        name = package.get("name") or location.rsplit("node_modules/", 1)[1]
        version = package.get("version")
        if not version or not re.fullmatch(r"\d+\.\d+\.\d+(?:[-+][A-Za-z0-9.+-]+)?", version):
            raise ValueError("npm dependency has no exact version: " + location)
        license_name = package.get("license")
        manifest = web / location / "package.json"
        if manifest.exists():
            installed = json.loads(manifest.read_text())
            if installed.get("name") != name or installed.get("version") != version:
                raise ValueError("installed npm dependency differs from lock: " + location)
            installed_license = installed.get("license")
            if license_name and installed_license and license_name != installed_license:
                raise ValueError("conflicting npm license metadata: " + location)
            license_name = installed_license or license_name
        if not license_name and not offline:
            url = "https://registry.npmjs.org/{}/{}".format(
                urllib.parse.quote(name, safe=""), urllib.parse.quote(version, safe=""))
            with urllib.request.urlopen(url, timeout=20) as response:
                metadata = json.load(response)
            if metadata.get("name") != name or metadata.get("version") != version:
                raise ValueError("registry identity mismatch: " + name)
            license_name = metadata.get("license")
        records.append(("npm", name, version, license_name))
    return records


def go_records(root):
    # Resolve packages from downloaded modules, including test-only dependencies.
    packages = list(json_stream(run(
        ["go", "list", "-mod=readonly", "-deps", "-test", "-json", "./..."], root)))
    dependencies = {}
    imports = set()
    for package in packages:
        if package.get("Error") or package.get("DepsErrors"):
            raise ValueError("incomplete Go package inventory")
        module = package.get("Module")
        if not module or module.get("Main"):
            continue
        if module.get("Replace"):
            raise ValueError("Go replacement requires explicit license review: " + module["Path"])
        dependencies[module["Path"]] = module["Version"]
        if not package.get("ForTest"):
            imports.add(package["ImportPath"])
    if not dependencies or not imports:
        raise ValueError("empty Go dependency inventory")
    # go-licenses handles Go source/module license discovery; never apply npm tools to Go.
    report = run(["go-licenses", "report"] + sorted(imports), root)
    records = []
    covered = set()
    for row in csv.reader(io.StringIO(report)):
        if len(row) != 3:
            raise ValueError("malformed go-licenses report")
        path, _, license_name = row
        owners = [m for m in dependencies if path == m or path.startswith(m + "/")]
        if not owners:
            raise ValueError("unresolved Go license owner: " + path)
        owner = max(owners, key=len)
        covered.add(owner)
        records.append(("go", owner, dependencies[owner], license_name))
    missing = set(dependencies) - covered
    if missing:
        raise ValueError("Go modules absent from license report: " + ", ".join(sorted(missing)))
    return records


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("ecosystem", choices=["go", "npm"])
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--offline", action="store_true",
                        help="npm: missing local metadata fails, never skips dependencies")
    args = parser.parse_args()
    policy = load_policy(ROOT / "scripts/security/license-policy.json")
    records = (go_records(args.root) if args.ecosystem == "go"
               else npm_records(args.root / "web", args.offline))
    check(records, policy)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, TypeError, OSError, subprocess.SubprocessError) as error:
        print("LICENSE GATE FAILED: " + str(error), file=sys.stderr)
        sys.exit(1)
