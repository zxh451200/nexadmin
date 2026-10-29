#!/usr/bin/env python3
"""迁移三方言一致性检查：对比 mysql / mariadb / postgres 迁移目录解析出的表与列集合。

不执行 SQL、不连数据库——纯文本解析 CREATE TABLE 语句（够发现"漏写一个引擎/漏一列"这类漂移）。
类型表达式不做严格比对（各引擎类型名本来就不同），只比对表名、列名与列数。

用法：python scripts/ci/migration_parity.py [--root DIR]
退出码：发现漂移返回 1。
"""

import argparse
import re
import sys
from pathlib import Path

ENGINES = ("mysql", "mariadb", "postgres")
CREATE_RE = re.compile(r"CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?[`\"(\[]?\s*(\w+)", re.IGNORECASE)
# 列定义行：列名（反引号/引号/裸标识符）+ 类型；排除约束关键字开头
COLUMN_RE = re.compile(r"^\s*[`\"(\[]?\s*(\w+)\s+[A-Za-z]\w*", re.IGNORECASE)
CONSTRAINTS = ("PRIMARY", "UNIQUE", "KEY", "INDEX", "CONSTRAINT", "FOREIGN", "CHECK", "EXCLUDE")


def parse_tables(sql: str) -> dict:
    """解析一份 SQL 里的 表名 -> 列名列表（有序）。"""
    tables = {}
    for m in CREATE_RE.finditer(sql):
        name = m.group(1).lower()
        # 取 CREATE TABLE 到闭括号的片段（简化：到下一个 CREATE 或文件尾）
        tail = sql[m.end():]
        nxt = CREATE_RE.search(tail)
        seg = tail[: nxt.start()] if nxt else tail
        cols = []
        depth = 0
        for line in seg.splitlines():
            if "(" in line:
                depth += line.count("(") - line.count(")")
            stripped = line.strip()
            if not stripped or stripped.startswith("--"):
                continue
            cm = COLUMN_RE.match(line)
            if cm and cm.group(1).upper() not in CONSTRAINTS:
                cols.append(cm.group(1).lower())
            if depth <= 0 and ")" in line:
                break
        tables[name] = cols
    return tables


def load_engine(mig_dir: Path, engine: str) -> dict:
    d = mig_dir / engine
    if not d.is_dir():
        # MariaDB 允许回退到 mysql 目录（与运行时规则一致）
        if engine == "mariadb" and (mig_dir / "mysql").is_dir():
            d = mig_dir / "mysql"
        else:
            return {}
    tables = {}
    for f in sorted(d.glob("*.sql")):
        for name, cols in parse_tables(f.read_text(encoding="utf-8", errors="replace")).items():
            tables.setdefault(name, cols)
    return tables


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", default=".", help="仓库根目录")
    args = ap.parse_args()
    root = Path(args.root)
    mig = root / "migrations" / "framework"
    if not mig.is_dir():
        print(f"migration parity: {mig} 不存在", file=sys.stderr)
        return 2

    per_engine = {e: load_engine(mig, e) for e in ENGINES}
    drift = []

    all_tables = sorted({t for tabs in per_engine.values() for t in tabs})
    for t in all_tables:
        for e in ENGINES:
            if t not in per_engine[e]:
                drift.append(f"表 {t}: {e} 缺失")
                continue
        have = [e for e in ENGINES if t in per_engine[e]]
        if len(have) < 2:
            continue
        base = have[0]
        for e in have[1:]:
            a, b = per_engine[base][t], per_engine[e][t]
            only_a = [c for c in a if c not in b]
            only_b = [c for c in b if c not in a]
            if only_a:
                drift.append(f"表 {t}: {base} 有而 {e} 缺列 {only_a}")
            if only_b:
                drift.append(f"表 {t}: {e} 有而 {base} 缺列 {only_b}")

    if drift:
        print(f"migration parity: 发现 {len(drift)} 处方言漂移")
        for d in drift:
            print("  " + d)
        return 1
    n = sum(len(v) for v in per_engine.values())
    print(f"migration parity: 三方言一致（{len(all_tables)} 张表，共 {n} 份解析）")
    return 0


if __name__ == "__main__":
    sys.exit(main())
