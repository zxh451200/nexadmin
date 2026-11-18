#!/usr/bin/env python3
"""生成官网客服「收货地址」卡片的城市级联数据：plugins/website/static/cities/<ISO2>.json

数据源：GeoNames dump
  - cities1000.txt           https://download.geonames.org/export/dump/cities1000.zip
  - admin1CodesASCII.txt     https://download.geonames.org/export/dump/admin1CodesASCII.txt
  - admin2Codes.txt          https://download.geonames.org/export/dump/admin2Codes.txt
（CC BY 4.0，许可是提供者的声明）

州/省名以 plugins/website/static/country-region.json 为准（来自 country-region-data）。
GeoNames 的州名与它不是一一对应（拼写差异、带 Province/Region 前后缀、行政层级不同），
所以按三级匹配：归一化后精确相等 → 互相包含 → 相似度阈值；admin1 优先于 admin2，
避免同名 admin2（如美国各州的 Washington County）把别的州的城市串进来。

输出格式与线上一致：{"<州名>": ["城市1", "城市2", ...]}，州名保持 country-region.json 原文，
城市按人口降序、同名去重、每州封顶（--cap）。没有匹配到城市的州不写键，前端据此退回手写输入。

用法：
    python scripts/gen_cities/gen_cities.py --src /path/to/geonames
"""
import argparse
import collections
import difflib
import json
import os
import re
import sys
import unicodedata

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
CR_PATH = os.path.join(ROOT, 'plugins/website/static/country-region.json')
OUT_DIR = os.path.join(ROOT, 'plugins/website/static/cities')

# 行政层级词：两边都削掉，才不会「Province of Abra」对不上「Abra」
PREFIXES = [
    'province of', 'region of', 'governorate of', 'prefecture of', 'city of',
    'municipality of', 'district of', 'republic of', 'state of',
    'tinh', 'thanh pho', 'thu do', 'gorod', 'gmina', 'opstina', 'obshtina', 'rajon',
]
SUFFIXES = [
    'province', 'provincia', 'province', 'region', 'regione', 'governorate', 'muhafazat',
    'prefecture', 'district', 'county', 'state', 'oblast', 'voblast', 'voblasc', 'horad',
    'gorod', 'zupanija', 'upanija', 'canton', 'department', 'departamento', 'division',
    'territory', 'municipality', 'municipio', 'comune', 'gmina', 'powiat', 'raion', 'rayon',
    'okrug', 'krai', 'kray', 'autonomous', 'republic',
]
STOPWORDS = {
    'de', 'del', 'della', 'do', 'da', 'dos', 'das', 'la', 'le', 'el', 'los', 'las',
    'il', 'lo', 'di', 'of', 'the', 'and',
}

# 拼写差异大到相似度也救不回来的，手工兜一下（只收真正必要的）
ALIASES = {
    ('AF', 'Nimruz'): 'Nimroz',
    ('AF', 'Urozgan'): 'Oruzgan',
    ('BY', 'Homiel voblast'): 'Homyel',
    ('BY', 'Hrodna voblast'): 'Hrodna',
    ('BY', 'Mahilyow voblast'): 'Mahilyow',
    ('BY', 'Vitsebsk voblast'): 'Vitebsk',
    ('BY', 'Brest voblast'): 'Brest',
    ('BY', 'Minsk voblast'): 'Minsk',
}


def norm(name):
    s = unicodedata.normalize('NFKD', name)
    s = ''.join(ch for ch in s if not unicodedata.combining(ch))
    s = s.lower().replace('&', ' and ').replace("'", ' ').replace('\u2019', ' ')
    tokens = re.sub(r'[^a-z0-9]+', ' ', s).split()
    changed = True
    while changed and tokens:
        changed = False
        for p in PREFIXES:
            pt = p.split()
            if len(tokens) > len(pt) and tokens[:len(pt)] == pt:
                tokens = tokens[len(pt):]
                changed = True
                break
        if len(tokens) > 1 and tokens[-1] in SUFFIXES:
            tokens = tokens[:-1]
            changed = True
        if len(tokens) > 1 and tokens[0] in SUFFIXES:
            tokens = tokens[1:]
            changed = True
    tokens = [t for t in tokens if t not in STOPWORDS]
    return ''.join(tokens)


def load_admin1(path):
    out = {}
    with open(path, encoding='utf-8') as f:
        for line in f:
            p = line.rstrip('\n').split('\t')
            if len(p) >= 3:
                out[p[0]] = p[1]
    return out


def load_admin2(path):
    out = {}
    with open(path, encoding='utf-8') as f:
        for line in f:
            p = line.rstrip('\n').split('\t')
            if len(p) >= 3:
                out[p[0]] = p[1]
    return out


def load_cities(path):
    """(国家, 层级, 行政码) -> {城市名: 人口}；只收 feature class P（有人居住的地方）"""
    groups = collections.defaultdict(dict)
    with open(path, encoding='utf-8') as f:
        for line in f:
            p = line.rstrip('\n').split('\t')
            if len(p) < 15 or p[6] != 'P':
                continue
            cc, adm1, adm2 = p[8], p[10], p[11]
            name, pop = p[1], int(p[14] or 0)
            if not name:
                continue
            if adm1:
                g = groups[(cc, 1, adm1)]
                if pop > g.get(name, -1):
                    g[name] = pop
            if adm2:
                g = groups[(cc, 2, adm1 + '.' + adm2)]
                if pop > g.get(name, -1):
                    g[name] = pop
    return groups


def pick(level_names, state_norm, threshold):
    """在一个层级里挑与该州名对应的行政码键：精确优先，其次包含，最后相似度"""
    hits = [k for k, nm in level_names.items() if nm == state_norm]
    if hits:
        return hits
    if len(state_norm) >= 5:
        hits = [k for k, nm in level_names.items()
                if (state_norm in nm or nm in state_norm) and min(len(nm), len(state_norm)) >= 5]
        if hits:
            return hits
    best, best_k = 0.0, None
    for k, nm in level_names.items():
        r = difflib.SequenceMatcher(None, state_norm, nm).ratio()
        if r > best:
            best, best_k = r, k
    return [best_k] if best_k and best >= threshold else []


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--src', required=True, help='GeoNames dump 解压目录')
    ap.add_argument('--out', default=OUT_DIR)
    ap.add_argument('--cap', type=int, default=400, help='每州最多保留多少个城市（按人口降序）')
    ap.add_argument('--threshold', type=float, default=0.86, help='州名相似度阈值')
    args = ap.parse_args()

    countries = json.load(open(CR_PATH, encoding='utf-8'))
    a1 = load_admin1(os.path.join(args.src, 'admin1CodesASCII.txt'))
    a2 = load_admin2(os.path.join(args.src, 'admin2Codes.txt'))
    cities = load_cities(os.path.join(args.src, 'cities1000.txt'))

    # 国家 -> 层级 -> {行政码: 归一化名}
    names = collections.defaultdict(lambda: {1: {}, 2: {}})
    for (cc, lvl, code) in cities:
        full = '%s.%s' % (cc, code)
        nm = a1.get(full) if lvl == 1 else a2.get(full)
        if nm:
            names[cc][lvl][code] = norm(nm)

    os.makedirs(args.out, exist_ok=True)
    stats = {'countries': 0, 'states': 0, 'covered': 0, 'cities': 0, 'bytes': 0}
    gaps = []
    for c in countries:
        cc = c['c']
        states = c.get('r') or []
        if not states or cc not in names:
            continue
        out = collections.OrderedDict()
        for st in states:
            sn = norm(ALIASES.get((cc, st), st))
            # admin1 优先：admin2 里同名的地方多（美国的 Washington County 遍布各州），
            # 拿 admin2 兜底时只认本国的那个行政码，不能跨州串城市。
            level, codes = 1, pick(names[cc][1], sn, args.threshold)
            if not codes:
                level, codes = 2, pick(names[cc][2], sn, args.threshold)
            bucket = {}
            for code in codes:
                for nm, pop in cities.get((cc, level, code), {}).items():
                    if pop > bucket.get(nm, -1):
                        bucket[nm] = pop
            stats['states'] += 1
            if not bucket:
                gaps.append((cc, st))
                continue
            stats['covered'] += 1
            ordered = sorted(bucket.items(), key=lambda kv: (-kv[1], kv[0]))
            out[st] = [nm for nm, _ in ordered[:args.cap]]
            stats['cities'] += len(out[st])
        if not out:
            continue
        stats['countries'] += 1
        path = os.path.join(args.out, cc + '.json')
        body = json.dumps(out, ensure_ascii=False, separators=(',', ':'))
        with open(path, 'w', encoding='utf-8', newline='\n') as f:
            f.write(body)
        stats['bytes'] += len(body.encode('utf-8'))

    print('国家 %d，州省 %d，有城市 %d（%.1f%%），城市 %d，数据 %.0f KB'
          % (stats['countries'], stats['states'], stats['covered'],
             100.0 * stats['covered'] / max(1, stats['states']),
             stats['cities'], stats['bytes'] / 1024))
    print('仍无城市数据的州省 %d 个，示例：%s' % (len(gaps), gaps[:10]))


if __name__ == '__main__':
    sys.exit(main())
