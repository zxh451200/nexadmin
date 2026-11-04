package modbus

// Modbus 地址解析与字节解码的纯函数单测（不连真设备）。

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestAddr(t *testing.T) {
	cases := []struct {
		in     string
		prefix string
		off    uint16
		wantOK bool
	}{
		{"hr:40001", "hr", 40000, true}, // 1 起算编号 40001 → 0 基偏移 40000
		{"hr:40101", "hr", 40100, true},
		{"ir:30001", "ir", 30000, true},
		{"co:1", "co", 0, true},
		{"di:10001", "di", 10000, true},
		{"HR:0x10", "hr", 15, true}, // 大写前缀 + 十六进制
		{"hr:0", "", 0, false},      // 1 起算
		{"40001", "", 0, false},     // 缺前缀
		{"xx:1", "", 0, false},      // 未知前缀
	}
	for _, c := range cases {
		prefix, off, err := addr(c.in)
		if c.wantOK {
			if err != nil || prefix != c.prefix || off != c.off {
				t.Fatalf("addr(%q) = %q,%d,%v; want %q,%d", c.in, prefix, off, err, c.prefix, c.off)
			}
		} else if err == nil {
			t.Fatalf("addr(%q) 应该拒绝", c.in)
		}
	}
}

func TestDecode(t *testing.T) {
	f32 := make([]byte, 4)
	binary.BigEndian.PutUint32(f32, math.Float32bits(42.7))
	i16 := make([]byte, 2)
	binary.BigEndian.PutUint16(i16, uint16(math.MaxInt16))

	if v, err := decode("float32", f32); err != nil || math.Abs(v.Num-42.7) > 1e-5 {
		t.Fatalf("float32 decode = %v, %v", v.Num, err)
	}
	if v, err := decode("int16", i16); err != nil || v.Num != math.MaxInt16 {
		t.Fatalf("int16 decode = %v, %v", v.Num, err)
	}
	if v, err := decode("bool", []byte{1}); err != nil || v.Num != 1 {
		t.Fatalf("bool decode = %v, %v", v.Num, err)
	}
	if _, err := decode("int32", make([]byte, 4)); err != nil {
		t.Fatalf("int32 decode: %v", err)
	}
	// 长度不足要报错，不能读出零值误导人
	if _, err := decode("float32", f32[:2]); err == nil {
		t.Fatal("float32 with 2 bytes should fail")
	}
	if _, err := decode("unknown", f32); err == nil {
		t.Fatal("unknown type should fail")
	}
}

func TestRegsOf(t *testing.T) {
	if regsOf("int16") != 1 || regsOf("bool") != 1 {
		t.Fatal("16 位类型应占 1 个寄存器")
	}
	if regsOf("float32") != 2 || regsOf("int32") != 2 {
		t.Fatal("32 位类型应占 2 个寄存器")
	}
}
