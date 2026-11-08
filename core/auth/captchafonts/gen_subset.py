# 点选验证码字体子集生成脚本（assets provenance）。
#
# 源字体：Noto Sans SC（思源黑体的 Google 发行版），SIL Open Font License 1.1——
# 允许商用、允许嵌入与子集化（改名才受限，本目录不保留 RFN 命名），替代
# go-captcha-assets 内嵌的方正系字体（方正字体商用需向方正购买授权，卖产品不能带）。
#
# 下载源字体（可变 TTF，~10MB，不入仓库）：
#   https://github.com/google/fonts/raw/main/ofl/notosanssc/NotoSansSC%5Bwght%5D.ttf
# 再生成（pip install fonttools，在仓库外跑也行，产物放回本目录）：
#   python gen_subset.py <NotoSansSC[wght].ttf 路径>
# 产出：clickfont.ttf —— 实例化到 Medium(500) 字重（照片花底上比 Regular 更耐看），
#       只保留 charset.txt 里的字形，几十 KB。
#
# 字符池扩充时：先改 charset.txt，重跑本脚本，字形覆盖测试会兜住漏网字符。

import pathlib
import sys

from fontTools import subset
from fontTools.ttLib import TTFont
from fontTools.varLib.instancer import instantiateVariableFont

HERE = pathlib.Path(__file__).parent


def load_charset() -> str:
    chars, seen = [], set()
    for line in (HERE / "charset.txt").read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        for ch in line:
            if ch not in seen:
                seen.add(ch)
                chars.append(ch)
    return "".join(chars)


def main():
    if len(sys.argv) != 2:
        sys.exit("用法: python gen_subset.py <NotoSansSC[wght].ttf 路径>")
    src = pathlib.Path(sys.argv[1])
    if not src.is_file():
        sys.exit(f"源字体不存在: {src}")

    font = TTFont(str(src))
    if "fvar" in font:
        # Medium(500)：验证码正文在花底上要压得住，Regular 偏细
        instantiateVariableFont(font, {"wght": 500}, inplace=True)

    text = load_charset()
    opts = subset.Options()
    opts.drop_tables += ["GSUB", "GPOS", "GDEF"]  # freetype/captcha 渲染用不到排版特性
    opts.layout_features = []
    opts.notdef_outline = True
    opts.recalc_bounds = True
    opts.drop_names = True

    ss = subset.Subsetter(opts)
    ss.populate(text=text)
    ss.subset(font)

    out = HERE / "clickfont.ttf"
    font.save(str(out))
    print(f"charset {len(text)} 字，输出 {out.name} {out.stat().st_size} bytes")


if __name__ == "__main__":
    main()
