# captchafonts

`clickfont.ttf` 是 [Noto Sans SC](https://github.com/google/fonts/tree/main/ofl/notosanssc)
（思源黑体的 Google 发行版，**SIL Open Font License 1.1**——允许商用、允许嵌入与子集化，
仅改名受限、本目录不保留其保留字体名）的**子集**：按 `charset.txt` 的字符池裁剪并实例化
到 Medium(500) 字重，约 48KB。

替换原因：go-captcha-assets 内嵌的是方正系字体（`fzshengsksjw_cu`）——方正字体商用
需向方正购买授权，随商业产品分发构成侵权。

再生成：下载源字体（可变 TTF，不入仓库）
`https://github.com/google/fonts/raw/main/ofl/notosanssc/NotoSansSC%5Bwght%5D.ttf`
（GitHub 直连慢时走 `https://cdn.jsdelivr.net/gh/google/fonts@main/...` 同路径），
然后 `pip install fonttools && python gen_subset.py <字体路径>`。
扩字符池：先改 `charset.txt`，重跑子集，字形覆盖测试（captcha_click_font_test.go）会兜住漏网字符。
