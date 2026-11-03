package idcard

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// 测试用区划码表：只放样本里出现过的码，其余一律认不出。
var stubRegions = map[string]string{
	"110000": "北京市",
	"110100": "市辖区",
	"110101": "东城区",
	"110105": "朝阳区",
	"410000": "河南省",
	"410100": "郑州市",
	"410181": "巩义市",
	"440000": "广东省",
	"440300": "深圳市",
	"440304": "福田区",
	"500000": "重庆市",
	"500100": "市辖区",
	"500154": "开州区",
	"610000": "陕西省",
	"610100": "西安市",
	"610104": "莲湖区",
	"810000": "香港特别行政区",
	"830000": "台湾地区",
}

// stubRegion 逐级回退解析，与插件里包 core/region 的实现同一口径。
func stubRegion(code string) ([]string, int, bool) { return walkStub(code, true) }

// stubRegionNoCounty 模拟只同步到地级粒度的区划快照。
func stubRegionNoCounty(code string) ([]string, int, bool) { return walkStub(code, false) }

func walkStub(code string, withCounty bool) ([]string, int, bool) {
	levels := []string{code[:2] + "0000", code[:4] + "00", code}
	var path []string
	level := 0
	for i, c := range levels {
		if i == 2 && !withCounty {
			break
		}
		if i > 0 && c == levels[i-1] {
			continue // 直辖市、省直辖县级：上级与本级代码同形
		}
		name, ok := stubRegions[c]
		if !ok {
			continue
		}
		path = append(path, name)
		level = i + 1
	}
	if level == 0 {
		return nil, 0, false
	}
	return path, level, true
}

func TestParseValid(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		number   string
		legacy   bool
		path     string
		level    int
		birthday string
		sex      string
	}{
		{"末位X", "11010519491231002X", "11010519491231002X", false, "北京市/市辖区/朝阳区", 3, "1949-12-31", SexFemale},
		{"小写x与空白", " 11010119900307803x ", "11010119900307803X", false, "北京市/市辖区/东城区", 3, "1990-03-07", SexMale},
		{"纯数字", "500154199301135886", "500154199301135886", false, "重庆市/市辖区/开州区", 3, "1993-01-13", SexFemale},
		{"闰日出生的号码", "440304198802291234", "440304198802291234", false, "广东省/深圳市/福田区", 3, "1988-02-29", SexMale},
		{"港澳居民居住证", "810000199408230021", "810000199408230021", false, "香港特别行政区", 1, "1994-08-23", SexFemale},
		{"台湾居民居住证", "830000199201300022", "830000199201300022", false, "台湾地区", 1, "1992-01-30", SexFemale},
		{"全角数字", "５００１５４１９９３０１１３５８８６", "500154199301135886", false, "重庆市/市辖区/开州区", 3, "1993-01-13", SexFemale},
		{"15位升级", "610104620927690", "610104196209276908", true, "陕西省/西安市/莲湖区", 3, "1962-09-27", SexFemale},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, err := Parse(tc.raw, stubRegion)
			if err != nil {
				t.Fatalf("Parse(%q) 报错: %v", tc.raw, err)
			}
			if info.Number != tc.number {
				t.Errorf("Number = %q, want %q", info.Number, tc.number)
			}
			if info.Legacy != tc.legacy {
				t.Errorf("Legacy = %v, want %v", info.Legacy, tc.legacy)
			}
			if got := strings.Join(info.RegionNames, "/"); got != tc.path {
				t.Errorf("RegionNames = %q, want %q", got, tc.path)
			}
			if info.RegionLevel != tc.level {
				t.Errorf("RegionLevel = %d, want %d", info.RegionLevel, tc.level)
			}
			if got := info.Birthday.Format("2006-01-02"); got != tc.birthday {
				t.Errorf("Birthday = %s, want %s", got, tc.birthday)
			}
			if info.Sex != tc.sex {
				t.Errorf("Sex = %q, want %q", info.Sex, tc.sex)
			}
			if info.CheckBit != tc.number[17:] {
				t.Errorf("CheckBit = %q, want %q", info.CheckBit, tc.number[17:])
			}
		})
	}
}

// 区划快照粒度不到县级时，合法号码仍应判合法，只把归属地停在能命中的层级。
// 这条来自真实反馈：410181（巩义市）在只到地级的小区划快照里查不到，被误判成"格式不通过"。
func TestCountyMissIsNotAnInvalidNumber(t *testing.T) {
	info, err := Parse("41018119890415451X", stubRegionNoCounty)
	if err != nil {
		t.Fatalf("县级查不到不该判非法，实际: %v", err)
	}
	if got := strings.Join(info.RegionNames, "/"); got != "河南省/郑州市" {
		t.Errorf("RegionNames = %q, want 河南省/郑州市", got)
	}
	if info.RegionLevel != 2 {
		t.Errorf("RegionLevel = %d, want 2", info.RegionLevel)
	}
	if info.Birthday.Format("2006-01-02") != "1989-04-15" || info.Sex != SexMale {
		t.Errorf("birthday/sex = %s/%s", info.Birthday.Format("2006-01-02"), info.Sex)
	}
	// 同一号码在完整数据下应能到县级。
	full, err := Parse("41018119890415451X", stubRegion)
	if err != nil {
		t.Fatal(err)
	}
	if full.RegionLevel != 3 || strings.Join(full.RegionNames, "/") != "河南省/郑州市/巩义市" {
		t.Errorf("完整数据下 = %q level %d", strings.Join(full.RegionNames, "/"), full.RegionLevel)
	}
}

func TestParseLengthWithoutResolver(t *testing.T) {
	// 不注入 resolver 时，区划码不参与判定。
	if _, err := Parse("999999199003077759", nil); err != nil {
		t.Fatalf("region=nil 应跳过区划码校验，实际: %v", err)
	}
	if _, err := Parse("999999199003077759", stubRegion); ReasonOf(err) != ReasonRegion {
		t.Fatalf("连省级都认不出应报 region，实际: %v", err)
	}
}

func TestParseReasons(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want Reason
	}{
		{"空串", "", ReasonEmpty},
		{"只有空白", "  \t　", ReasonEmpty},
		{"17位", "11010519491231002", ReasonLength},
		{"19位", "1101051949123100211", ReasonLength},
		{"中间夹字母", "1101011990030A803X", ReasonChars},
		{"15位含字母", "61010462092769X", ReasonChars},
		{"末位是Y", "11010119900307803Y", ReasonChars},
		{"13月", "110105194913310021", ReasonBirthday},
		{"2月30日", "110105194902300021", ReasonBirthday},
		{"1900年前", "110105189912310012", ReasonBirthday},
		{"未来出生", "110105299901010012", ReasonFuture},
		{"校验位不符", "110105194912310021", ReasonChecksum},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.raw, stubRegion)
			if ReasonOf(err) != tc.want {
				t.Fatalf("Parse(%q) reason = %q (err=%v), want %q", tc.raw, ReasonOf(err), err, tc.want)
			}
		})
	}
}

func TestReasonOfOtherError(t *testing.T) {
	if got := ReasonOf(errors.New("boom")); got != "" {
		t.Fatalf("非本包错误应返回空串，实际 %q", got)
	}
}

func TestChecksumMatchesStandard(t *testing.T) {
	// GB 11643-1999 加权取模，用已验证过的号码体逐条核对。
	cases := map[string]byte{
		"11010519491231002": 'X',
		"11010119900307803": 'X',
		"50015419930113588": '6',
		"44030419880229123": '4',
		"61010419620927690": '8',
		"41018119890415451": 'X',
	}
	for body, want := range cases {
		got, err := Checksum(body)
		if err != nil {
			t.Fatalf("Checksum(%q): %v", body, err)
		}
		if got != want {
			t.Errorf("Checksum(%q) = %q, want %q", body, got, want)
		}
	}
	if _, err := Checksum("1101051949123100"); ReasonOf(err) != ReasonLength {
		t.Errorf("16 位号码体应报 length，实际 %v", err)
	}
	if _, err := Checksum("1101051949123100A"); ReasonOf(err) != ReasonChars {
		t.Errorf("含字母的号码体应报 chars，实际 %v", err)
	}
}

func TestUpgrade(t *testing.T) {
	got, err := Upgrade("610104620927690")
	if err != nil || got != "610104196209276908" {
		t.Fatalf("Upgrade(15位) = %q, %v", got, err)
	}
	got, err = Upgrade(" 11010519491231002x ")
	if err != nil || got != "11010519491231002X" {
		t.Fatalf("Upgrade(18位) 应原样返回，实际 %q, %v", got, err)
	}
	if _, err := Upgrade("12345"); ReasonOf(err) != ReasonLength {
		t.Fatalf("过短应报 length，实际 %v", err)
	}
	// 15 位号码本身没有校验位，升级后的校验位是自己补的，必须能再解析回去。
	up, err := Upgrade("610104620927690")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(up, stubRegion); err != nil {
		t.Fatalf("升级后的号码应可解析: %v", err)
	}
}

func TestAgeAt(t *testing.T) {
	info, err := Parse("11010119900307803X", stubRegion)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		now  string
		want int
	}{
		{"2026-03-06", 35}, // 生日前一天未满 36
		{"2026-03-07", 36}, // 生日当天算已满
		{"2026-03-08", 36},
	}
	for _, tc := range cases {
		now, err := time.Parse("2006-01-02", tc.now)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.AgeAt(now); got != tc.want {
			t.Errorf("AgeAt(%s) = %d, want %d", tc.now, got, tc.want)
		}
	}

	// 闰日出生的号码：非闰年按 3 月 1 日长岁。
	leap, err := Parse("440304198802291234", stubRegion)
	if err != nil {
		t.Fatal(err)
	}
	feb28, _ := time.Parse("2006-01-02", "2026-02-28")
	mar1, _ := time.Parse("2006-01-02", "2026-03-01")
	if got := leap.AgeAt(feb28); got != 37 {
		t.Errorf("2026-02-28 应 37 岁，实际 %d", got)
	}
	if got := leap.AgeAt(mar1); got != 38 {
		t.Errorf("2026-03-01 应 38 岁，实际 %d", got)
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"11010519491231002x":       "11010519491231002X", // 只有末位小写 x 被大写
		" 1101-05 1949 1231 002X ": "11010519491231002X",
		"１１０１０５１９４９１２３１００２Ｘ":       "11010519491231002X",
		"110105A9491231002X":       "110105A9491231002X", // 中间字母原样保留，交给校验判 chars
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}
