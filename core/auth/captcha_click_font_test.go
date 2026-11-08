package auth

import (
	"testing"

	"github.com/golang/freetype/truetype"
)

// 字符池与内嵌子集字体的一致性：池里每个字都必须在字体里有字形，否则渲染出豆腐块；
// 池必须足够大（大于单张挑战的字符数几个量级），猜字空间才有意义。扩池时先改
// charset.txt 再跑 captchafonts/gen_subset.py，本测试兜住漏网字符。
func TestClickCharsetGlyphCoverage(t *testing.T) {
	font, err := truetype.Parse(clickFontTTF)
	if err != nil {
		t.Fatalf("内嵌字体解析失败: %v", err)
	}
	pool := clickCharset()
	if len(pool) < 32 {
		t.Fatalf("字符池过小（%d 个），不足以支撑随机出题", len(pool))
	}
	seen := make(map[string]bool, len(pool))
	for _, s := range pool {
		rs := []rune(s)
		if len(rs) != 1 {
			t.Errorf("字符 %q 不是单个汉字", s)
			continue
		}
		if seen[s] {
			t.Errorf("字符 %q 在池中重复", s)
		}
		seen[s] = true
		if font.Index(rs[0]) == 0 {
			t.Errorf("子集字体缺少字形: %q（扩池后要重跑 gen_subset.py）", s)
		}
	}
}
