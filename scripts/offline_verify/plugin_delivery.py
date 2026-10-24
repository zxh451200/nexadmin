#!/usr/bin/env python3
"""Verify delivered plugin code in an isolated framework copy, without downloads."""
import argparse
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
DELIVERY = ROOT.parent / "nexadmin-plugins"
IDENTITY = "config.go ldap_auth.go ldap_conn.go ldap_ops.go service.go test_ops.go cas.go ldap_client_test.go store_test.go ldap_adversarial_test.go".split()

def verify_backend(plugin):
    source = ROOT / "plugins" / plugin
    delivered = DELIVERY / plugin / "backend"
    for name in (IDENTITY if plugin == "identity" else ["render.go", "tpl/module.go.tmpl", "codegen_test.go"]):
        if (source / name).read_bytes() != (delivered / name).read_bytes():
            raise RuntimeError("delivery differs: " + plugin + "/" + name)
    # Explicitly select existing tests from local fake-protocol and memory-store files.
    files = [p for p in delivered.glob("*_test.go") if "env" not in p.name]
    names = []
    for path in files:
        text = path.read_text()
        if any(marker in text for marker in ["os.Getenv(", "os.LookupEnv("]):
            continue
        names.extend(re.findall(r"^func (Test\w+)\(t \*testing.T\)", text, re.M))
    if not names:
        raise RuntimeError("no offline tests selected")
    with tempfile.TemporaryDirectory(prefix="nexadmin-delivery-") as tmp:
        framework = Path(tmp) / "framework"
        shutil.copytree(ROOT, framework, ignore=shutil.ignore_patterns(".git", "node_modules", "bin", "dist", "data", "logs", "__pycache__"))
        target = framework / "plugins" / plugin
        shutil.rmtree(target)
        shutil.copytree(delivered, target)
        # Plugin metadata lives one level above the delivered backend.
        metadata = DELIVERY / plugin / "plugin.yaml"
        if metadata.exists():
            shutil.copy2(metadata, target / "plugin.yaml")
        env = dict(os.environ, GOPROXY="off", GOSUMDB="off", GOTOOLCHAIN="local", GOWORK="off")
        for key in list(env):
            if key.startswith(("IDENTITY_", "LDAP_", "CAS_", "NEXADMIN_TEST_")):
                del env[key]
        subprocess.run(["go", "test", "./plugins/" + plugin, "-run", "^(" + "|".join(sorted(set(names))) + ")$", "-count=1", "-timeout=120s"], cwd=framework, env=env, check=True)
        if plugin == "codegen":
            subprocess.run(["go", "test", "./cmd/nexadmin", "-run", "^TestCodegenOwnership", "-count=1", "-timeout=90s"], cwd=framework, env=env, check=True)

FRONTEND = [("audit", "audit.mock.ts"), ("cron", "cron.mock.ts")] + [("formbuilder", name) for name in ["formbuilder.mock.ts", "FormbuilderPage.tsx", "OptionRules.tsx", "mockEngine.ts"]]

def verify_frontend():
    for plugin, name in FRONTEND:
        if (ROOT / "web/src/plugins" / plugin / name).read_bytes() != (DELIVERY / plugin / "web" / name).read_bytes():
            raise RuntimeError(f"delivery differs: {plugin}/web/{name}")
    dependencies = ROOT / "web/node_modules"
    for executable in ["typescript/bin/tsc", "eslint/bin/eslint.js"]:
        if not (dependencies / executable).is_file():
            raise RuntimeError(f"offline tool unavailable: {dependencies / executable}; installation disabled")
    with tempfile.TemporaryDirectory(prefix="nexadmin-frontend-delivery-") as tmp:
        web = Path(tmp) / "web"
        shutil.copytree(ROOT / "web", web, ignore=shutil.ignore_patterns("node_modules", "dist", ".git"))
        (web / "node_modules").symlink_to(dependencies, target_is_directory=True)
        for plugin, name in FRONTEND:
            shutil.copy2(DELIVERY / plugin / "web" / name, web / "src/plugins" / plugin / name)
        env = dict(os.environ, CI="1", npm_config_offline="true")
        for command in [["node", "node_modules/typescript/bin/tsc", "--noEmit"], ["node", "node_modules/eslint/bin/eslint.js", "src"]]:
            subprocess.run(command, cwd=web, env=env, check=True, timeout=120)

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("plugin", choices=["identity", "codegen", "frontend"])
    args = parser.parse_args()
    verify_frontend() if args.plugin == "frontend" else verify_backend(args.plugin)
