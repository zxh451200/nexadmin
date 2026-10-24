"""Require a version change when reviewed public-contract goldens change."""
import argparse
import json
from pathlib import Path
import subprocess

VERSION = ".nexadmin.manifest.json"
PREFIX = "core/testdata/contracts/"

def check(changed, before, after):
    goldens = any(p.startswith(PREFIX) and p.endswith(".golden") for p in changed)
    if goldens and before == after:
        raise ValueError("public contract golden changed without framework_version update")

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--base", required=True)
    args = parser.parse_args()
    base = subprocess.check_output(["git", "merge-base", args.base, "HEAD"], text=True).strip()
    changed = subprocess.check_output(["git", "diff", "--name-only", base, "HEAD"], text=True).splitlines()
    before = json.loads(subprocess.check_output(["git", "show", base + ":" + VERSION], text=True))["framework_version"]
    after = json.loads(Path(VERSION).read_text())["framework_version"]
    check(changed, before, after)

if __name__ == "__main__":
    main()
