"""Conservative PR selection from merge-base and Go imports (including tests)."""
import argparse
import json
import os
from pathlib import Path, PurePosixPath
import subprocess

DEPENDENCIES = {"go.mod", "go.sum", "go.work", "go.work.sum", "web/package.json", "web/package-lock.json", "web/npm-shrinkwrap.json", "web/pnpm-lock.yaml", "web/yarn.lock", "package.json", "package-lock.json", "npm-shrinkwrap.json", "pnpm-lock.yaml", "yarn.lock"}
SHARED = {"go.mod", "go.sum", "go.work", "go.work.sum", ".nexadmin.manifest.json", "Makefile", "Dockerfile"}

def select(changes, graph, root="."):
    paths = [p for status, names in changes for p in names]
    license_change = any(p in DEPENDENCIES or p.startswith("scripts/security/") or "license" in p.lower() or p.startswith("scripts/ci/") or p == ".github/workflows/security.yml" for p in paths)
    shared = any(p in SHARED or p.startswith((".github/", "scripts/ci/", "scripts/build", "config/")) for p in paths)
    frontend = shared or any(p.startswith("web/") or p in DEPENDENCIES or p == ".npmrc" for p in paths)
    backend_paths = [p for p in paths if not p.startswith("web/") and not (p.startswith("docs/") or ("/" not in p and p.endswith(".md")))]
    result = {"frontend": frontend, "licenses": license_change, "packages": [], "fallback": ""}
    if not backend_paths:
        return result
    def full(reason):
        result.update(packages=["./..."], fallback=reason)
        return result
    if shared:
        return full("shared configuration changed")
    if graph is None:
        return full("dependency graph unavailable")
    owners = {}
    for pkg in graph:
        if pkg.get("Error") or pkg.get("DepsErrors"):
            return full("dependency graph incomplete")
        directory = os.path.relpath(pkg["Dir"], os.path.abspath(root)).replace(os.sep, "/")
        owners[directory] = pkg["ImportPath"]
    selected = set()
    for status, names in changes:
        for p in names:
            if p not in backend_paths:
                continue
            directory = str(PurePosixPath(p).parent)
            while directory not in owners and directory != ".":
                directory = str(PurePosixPath(directory).parent)
            if directory not in owners:
                return full("changed resource has no package owner")
            if status.startswith(("D", "R")) and p.endswith(".go"):
                return full("deleted or renamed Go source/package")
            selected.add(owners[directory])
    while True:
        previous = set(selected)
        for pkg in graph:
            deps = pkg.get("Imports", []) + pkg.get("TestImports", []) + pkg.get("XTestImports", [])
            if selected.intersection(deps):
                selected.add(pkg["ImportPath"])
        if previous == selected:
            break
    result["packages"] = sorted(selected)
    return result

def parse_diff(raw):
    fields = raw.decode().split("\0")
    result = []
    i = 0
    while i < len(fields) and fields[i]:
        status = fields[i]
        count = 2 if status.startswith(("R", "C")) else 1
        names = fields[i + 1:i + 1 + count]
        if len(names) != count or any(not name for name in names):
            raise ValueError("truncated git diff")
        result.append((status, names))
        i += count + 1
    return result

def read_graph(raw):
    decoder = json.JSONDecoder()
    result = []
    while raw.strip():
        obj, end = decoder.raw_decode(raw.lstrip())
        result.append(obj)
        raw = raw.lstrip()[end:]
    if not result:
        raise ValueError("empty graph")
    return result

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--base")
    parser.add_argument("--output")
    parser.add_argument("--licenses-only", action="store_true")
    args = parser.parse_args()
    if not args.base:
        result = {"frontend": True, "licenses": True, "packages": ["./..."], "fallback": "full entry point"}
    else:
        try:
            base = subprocess.check_output(["git", "merge-base", args.base, "HEAD"], text=True).strip()
            changes = parse_diff(subprocess.check_output(["git", "diff", "--name-status", "-z", "-M", base, "HEAD"]))
        except (subprocess.SubprocessError, ValueError, OSError):
            result = {"frontend": True, "licenses": True, "packages": ["./..."], "fallback": "merge-base unavailable"}
        else:
            try:
                graph = [] if args.licenses_only else read_graph(subprocess.check_output(["go", "list", "-json", "./..."], text=True, timeout=35))
            except (subprocess.SubprocessError, ValueError, OSError):
                graph = None
            try:
                result = select(changes, graph)
            except (KeyError, TypeError, ValueError):
                result = {"frontend": True, "licenses": True, "packages": ["./..."], "fallback": "invalid dependency graph"}
    if args.output:
        with open(args.output, "a") as out:
            for key in ("frontend", "licenses"):
                out.write(f"{key}={str(result[key]).lower()}\n")
            out.write("backend=" + str(bool(result["packages"])).lower() + "\n")
            out.write("packages=" + json.dumps(result["packages"]) + "\n")
    print(json.dumps(result))

if __name__ == "__main__":
    main()
