package idcard

import (
	"errors"
	"testing"
	"time"
)

// 测试用区划码表：只放样本里出现过的码，其余一律 ok=false。
var stubRegions = map[string]string{
	"110105": "朝阳区",
	"110101": "东城区",
	"440304": "福田区",
	"500154": "开州区",
	"610104": "莲湖区",
	"810000": "香港特别行政区",
	"830000": "台湾地区",
}

func stubResolver(code string) (string, bool) {
	name, ok := stubRegions[code]
	return name, ok
}

func TestParseValid(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		number   string
		legacy   bool
		region   string
		birthday string
		sex      string
	}{
		{"末位X", "11010519491231002X", "11010519491231002X", false, "朝阳区", "1949-12-31", SexFemale},
		{"小写x与空白", " 11010119900307803x ", "11010119900307803X", false, "东城区", "1990-03-07", SexMale},
		{"纯数字", "500154199301135886", "500154199301135886", false, "开州区", "1993-01-13", SexFemale},
		{"闰日出生的号码", "440304198802291234", "440304198802291234", false, "福田区", "1988-02-29", SexMale},
		{"港澳居民居住证", "810000199408230021", "810000199408230021", false, "香港特别行政区", "1994-08-23", SexFemale},
		{"台湾居民居住证", "830000199201300022", "830000199201300022", false, "台湾地区", "1992-01-30", SexFemale},
		{"全角数字", "５００１５４１９９３０１１３５８８６", "500154199301135886", false, "开州区", "1993-01-13", SexFemale},
		{"15位升级", "610104620927690", "610104196209276908", true, "莲湖区", "1962-09-27", SexFemale},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, err := Parse(tc.raw, stubResolver)
			if err != nil {
				t.Fatalf("Parse(%q) 报错: %v", tc.raw, err)
			}
			if info.Number != tc.number {
				t.Errorf("Number = %q, want %q", info.Number, tc.number)
			}
			if info.Legacy != tc.legacy {
				t.Errorf("Legacy = %v, want %v", info.Legacy, tc.legacy)
			}
			if info.RegionName != tc.region {
				t.Errorf("RegionName = %q, want %q", info.RegionName, tc.region)
			}
			if got := info.Birthday.Format("2006-01-02"); got != tc.birthday {
				t.Errorf("Birthday = %s, want %s", got, tc.birthday)
			}
			if info.Sex != tc.sex {
				t.Errorf("Sex = %q, want %q", info.Sex, tc.sex)
			}
			if info.CheckBit != tc.number[len(tc.number)-1:] {
				t.Errorf("CheckBit = %q, want %q", info.CheckBit, tc.number[17:])
			}
		})
	}
}

func TestParseLengthWithoutResolver(t *testing.T) {
	// 不注入 resolver 时，区划码不存在的号码仍按号码规则判定通过。
	if _, err := Parse("999999199003077759", nil); err != nil {
		t.Fatalf("region=nil 应跳过区划码校验，实际: %v", err)
	}
	if _, err := Parse("999999199003077759", stubResolver); ReasonOf(err) != ReasonRegion {
		t.Fatalf("未知区划码应报 region，实际: %v", err)
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
			_, err := Parse(tc.raw, stubResolver)
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
	// 15 位号码自身没有校验位，升级后的号码校验位是自己补的，必须能再解析回去。
	up, err := Upgrade("610104620927690")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(up, stubResolver); err != nil {
		t.Fatalf("升级后的号码应可解析: %v", err)
	}
}

func TestAgeAt(t *testing.T) {
	info, err := Parse("11010119900307803X", stubResolver)
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
	leap, err := Parse("440304198802291234", stubResolver)
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
