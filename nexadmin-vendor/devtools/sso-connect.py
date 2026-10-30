#!/usr/bin/env python3
"""把各 NexAdmin 派生项目接入统一身份中心（OIDC）。

用法：
  python sso_connect.py --center http://127.0.0.1:19000 --token <超管token> \
      --root D:/home/work/data/xxx [--only nexTools] [--dry-run]

行为：为每个项目在身份中心注册应用（redirect_uris=本机回调），把
auth.providers.oidc 写入项目 config/config.yaml（保留注释，先备份 .bak-sso-*）。
"""
import argparse, json, os, shutil, sys, time, urllib.request, urllib.error
import yaml

DEFAULT_PROJECTS = "nexEmail nexForum nexGrain nexHotel nexOrder nexRag nexTools nexTwin nexWms nexMall nexBI".split()


def api(url, method="GET", body=None, token=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    if data:
        req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=15) as r:
            return r.status, json.loads(r.read() or b"{}")
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read() or b"{}")


def project_port(root, name):
    p = os.path.join(root, name)
    for f in ("config/config.yaml", "config/default.yaml"):
        fp = os.path.join(p, f)
        if not os.path.exists(fp):
            continue
        try:
            c = yaml.safe_load(open(fp, encoding="utf-8")) or {}
            port = (c.get("server") or {}).get("port")
            if port:
                return int(port)
        except Exception:
            pass
    return 8080


def oidc_block(issuer, client_id, client_secret, redirect_uri):
    return [
        "    oidc:",
        "      name: 统一身份中心",
        f"      issuer: {issuer}",
        f"      client_id: {client_id}",
        f"      client_secret: {client_secret}",
        "      scopes: [openid, email, profile]",
        f'      redirect_uris: ["{redirect_uri}"]',
        "      match_by: email",
        "      auto_create: true",
    ]


def upsert_oidc(text, block):
    lines = text.splitlines()
    # 找 auth: / providers: / oidc:
    auth_i = providers_i = oidc_i = None
    for i, l in enumerate(lines):
        s = l.rstrip()
        if s == "auth:":
            auth_i = i
            continue
        if auth_i is not None and s == "  providers:":
            providers_i = i
            continue
        if providers_i is not None and s == "    oidc:":
            oidc_i = i
            break
    if oidc_i is not None:
        # 替换到下一个缩进 <=4 的非空行
        end = len(lines)
        for j in range(oidc_i + 1, len(lines)):
            l = lines[j]
            if not l.strip():
                continue
            indent = len(l) - len(l.lstrip())
            if indent <= 4:
                end = j
                break
        return "\n".join(lines[:oidc_i] + block + lines[end:]) + ("\n" if text.endswith("\n") else "")
    if providers_i is not None:
        return "\n".join(lines[:providers_i + 1] + block + lines[providers_i + 1:]) + "\n"
    if auth_i is not None:
        ins = ["  providers:"] + block
        return "\n".join(lines[:auth_i + 1] + ins + lines[auth_i + 1:]) + "\n"
    tail = "" if text.endswith("\n") or not text else "\n"
    return text + tail + "auth:\n  providers:\n" + "\n".join(block) + "\n"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--center", required=True)
    ap.add_argument("--token", required=True)
    ap.add_argument("--root", required=True)
    ap.add_argument("--only", default="")
    ap.add_argument("--issuer-path", default="/api/v1/oauth", help="身份中心 OIDC issuer 路径（与 discovery 的 issuer 一致）")
    ap.add_argument("--dry-run", action="store_true")
    a = ap.parse_args()
    names = a.only.split() if a.only else DEFAULT_PROJECTS
    stamp = time.strftime("%Y%m%d-%H%M%S")
    rows = []
    for n in names:
        p = os.path.join(a.root, n)
        if not os.path.isdir(p):
            rows.append((n, "-", "-", "missing"))
            continue
        port = project_port(a.root, n)
        redirect = f"http://127.0.0.1:{port}/login/oauth/oidc"
        if a.dry_run:
            rows.append((n, port, "-", "dry-run(no register)"))
            continue
        code, out = api(a.center + "/api/v1/sso/apps", "POST", {"name": n, "redirectUris": [redirect]}, a.token)
        if code != 200:
            rows.append((n, port, "-", f"register-failed {code} {out.get('message')}"))
            continue
        d = out.get("data") or {}
        cid, sec = d.get("clientId"), d.get("clientSecret")
        if not cid or not sec:
            rows.append((n, port, "-", "register-no-credentials"))
            continue
        cfg = os.path.join(p, "config", "config.yaml")
        text = open(cfg, encoding="utf-8").read() if os.path.exists(cfg) else ""
        issuer = a.center.rstrip("/") + a.issuer_path
        new = upsert_oidc(text, oidc_block(issuer, cid, sec, redirect))
        if os.path.exists(cfg):
            shutil.copy2(cfg, cfg + f".bak-sso-{stamp}")
        os.makedirs(os.path.dirname(cfg), exist_ok=True)
        open(cfg, "w", encoding="utf-8", newline="\n").write(new)
        rows.append((n, port, cid, "written"))

    print(f"{'project':10} {'port':6} {'clientId':28} status")
    for r in rows:
        print(f"{r[0]:10} {str(r[1]):6} {str(r[2]):28} {r[3]}")
    if any(r[3] != "written" for r in rows) and not a.dry_run:
        print("\n有项目未写入，见上表；已写入的配置备份为 config.yaml.bak-sso-*")


if __name__ == "__main__":
    main()
