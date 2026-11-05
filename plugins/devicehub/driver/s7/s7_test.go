package s7

import (
	"context"
	"strings"
	"testing"
)

// S7 地址解析是这个驱动里唯一能在没有 PLC 的情况下证明对错的逻辑，也是风险最高的一段：
// 解析错一个字节偏移，读回来的是别的存储区的真数据，比直接报错更坏。
// 这里同时钉住两个本轮修掉的缺陷（详见 parse 的注释）：
// kind 大小写与 readBytes/writeBytes 不一致（非 DB 区全部报「未知区域」，I 区只读守卫形同不存在），
// 以及 "DB1" 这类缺点号的写法被通用分支当成「DB0 偏移 1」。

func TestParseDBAreas(t *testing.T) {
	cases := []struct {
		address, dataType string
		want              area
	}{
		{"DB1.DBX0.0", "bool", area{kind: 'd', db: 1, byte: 0, bit: 0, size: 1, wl: 1}},
		{"DB7.DBX12.7", "bool", area{kind: 'd', db: 7, byte: 12, bit: 7, size: 1, wl: 1}},
		{"db3.dbw4", "int16", area{kind: 'd', db: 3, byte: 4, size: 2, wl: 1}},
		{"DB4.DBD8", "float32", area{kind: 'd', db: 4, byte: 8, size: 4, wl: 1}},
		{"DB4.DBD8", "int32", area{kind: 'd', db: 4, byte: 8, size: 4, wl: 1}},
	}
	for _, tc := range cases {
		got, err := parse(tc.address, tc.dataType)
		if err != nil {
			t.Errorf("parse(%q, %q) 报错：%v", tc.address, tc.dataType, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parse(%q, %q) = %+v, want %+v", tc.address, tc.dataType, got, tc.want)
		}
	}
}

// 非 DB 区的 kind 必须是小写——readBytes / writeBytes 只认小写。
func TestParseNonDBAreasUseLowercaseKind(t *testing.T) {
	cases := []struct {
		address string
		want    area
	}{
		{"M0.0", area{kind: 'm', byte: 0, bit: 0, size: 1}},
		{"MW10", area{kind: 'm', byte: 10, size: 2}},
		{"MD12", area{kind: 'm', byte: 12, size: 4}},
		{"MB5", area{kind: 'm', byte: 5, size: 1}},
		{"I0.1", area{kind: 'i', byte: 0, bit: 1, size: 1}},
		{"IW6", area{kind: 'i', byte: 6, size: 2}},
		{"Q0.2", area{kind: 'q', byte: 0, bit: 2, size: 1}},
		{"QW8", area{kind: 'q', byte: 8, size: 2}},
		// 定时器 / 计数器固定 16 位，与 dataType 无关
		{"T1", area{kind: 't', byte: 1, size: 2}},
		{"C10", area{kind: 'c', byte: 10, size: 2}},
	}
	for _, tc := range cases {
		got, err := parse(tc.address, "float32")
		if err != nil {
			t.Errorf("parse(%q) 报错：%v", tc.address, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parse(%q) = %+v, want %+v", tc.address, got, tc.want)
		}
		if got.kind >= 'A' && got.kind <= 'Z' {
			t.Errorf("parse(%q) 的 kind = %q，大写会让 readBytes 走 default 报「未知区域」", tc.address, string(got.kind))
		}
	}
}

func TestParseRejects(t *testing.T) {
	for _, addr := range []string{
		"",
		"DB1",            // 缺点号：不能被当成「DB0 偏移 1」
		"DB0.DBX0.0",     // DB0 不是合法 DB 号
		"DB1.DBB10",      // DB 区没有 DBB 写法
		"DB1.DBL0",       // 不认识的宽度字母
		"DB1.DBX0.8",     // 位号 0~7
		"M0.8",           // 同上
		"X1",             // 没有的区域字母
		"M",              // 没有偏移
		"MW-1",           // 负偏移
		"DBabc.DBW0",     // DB 号不是数字
		"ns=3;s=\"Tag\"", // OPC UA 写法混进来
	} {
		if got, err := parse(addr, "float32"); err == nil {
			t.Errorf("parse(%q) 应当报错，实际返回 %+v", addr, got)
		}
	}
}

// I 区只读：这条守卫在 kind 归一小写之前是死的（大写 'I' 匹配不到 'i'）。
func TestWriteRejectsInputArea(t *testing.T) {
	s := &session{}
	err := s.Write(context.Background(), "I0.0", "bool", "1")
	if err == nil {
		t.Fatal("写输入区 I 必须被拒")
	}
	if !strings.Contains(err.Error(), "只读") {
		t.Errorf("报错要说清是只读，实际：%v", err)
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	d, err := New("192.168.1.30:102", nil)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := d.(*session)
	if !ok {
		t.Fatalf("New 返回的类型 = %T", d)
	}
	if s.cfg.PollIntervalMs != 5000 || s.cfg.TimeoutMs != 2000 || s.cfg.Slot != 1 {
		t.Errorf("缺省配置不对： %+v", s.cfg)
	}
	// 显式传 0 也要回到默认值：0 毫秒超时会把每次请求立刻掐死
	if _, err := New(" 10.0.0.5:102 ", []byte(`{"rack":0,"slot":0,"pollIntervalMs":0,"timeoutMs":0}`)); err != nil {
		t.Fatal(err)
	}
	d2, _ := New("10.0.0.5:102", []byte(`{"slot":2,"pollIntervalMs":8000,"timeoutMs":1500}`))
	if got := d2.(*session).cfg; got.Slot != 2 || got.PollIntervalMs != 8000 || got.TimeoutMs != 1500 {
		t.Errorf("显式配置没生效： %+v", got)
	}
}
