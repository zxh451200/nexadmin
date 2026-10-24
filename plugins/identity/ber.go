package identity

import (
	"errors"
	"fmt"
	"io"
)

// 本文件是 LDAPv3 所需的最小 BER 编解码（只支持单字节标签、定长编码）。
// 没有使用 go-ldap/ldap：本机 module cache 里没有，沙箱无外网，也不引入未经审核的依赖。

// BER / LDAP 标签。
const (
	tagBool       = 0x01
	tagInt        = 0x02
	tagOctet      = 0x04
	tagEnum       = 0x0a
	tagSeq        = 0x30
	tagSet        = 0x31
	tagBindReq    = 0x60
	tagBindResp   = 0x61
	tagUnbindReq  = 0x42
	tagSearchReq  = 0x63
	tagSearchEnt  = 0x64
	tagSearchDone = 0x65
	tagSearchRef  = 0x73
	tagExtReq     = 0x77
	tagExtResp    = 0x78
	tagSimpleAuth = 0x80
	tagExtName    = 0x80
)

var errBER = errors.New("ldap: malformed BER data")

func encLen(n int) []byte {
	if n < 0x80 {
		return []byte{byte(n)}
	}
	var b []byte
	for v := n; v > 0; v >>= 8 {
		b = append([]byte{byte(v)}, b...)
	}
	return append([]byte{0x80 | byte(len(b))}, b...)
}

// enc 编码一个 TLV，value 为各部分按顺序拼接。
func enc(tag byte, parts ...[]byte) []byte {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, 0, n+6)
	out = append(out, tag)
	out = append(out, encLen(n)...)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func encInt(tag byte, v int) []byte {
	var b []byte
	for {
		b = append([]byte{byte(v)}, b...)
		v >>= 8
		if (v == 0 && b[0]&0x80 == 0) || (v == -1 && b[0]&0x80 != 0) {
			break
		}
	}
	return enc(tag, b)
}

func encStr(tag byte, s string) []byte { return enc(tag, []byte(s)) }

func encBool(v bool) []byte {
	if v {
		return enc(tagBool, []byte{0xff})
	}
	return enc(tagBool, []byte{0x00})
}

// parseTLV 解出 b 开头的一个 TLV，返回标签、值与剩余字节。
func parseTLV(b []byte) (tag byte, val, rest []byte, err error) {
	if len(b) < 2 || b[0]&0x1f == 0x1f {
		return 0, nil, nil, errBER
	}
	tag = b[0]
	n, hdr, err := decodeLen(b[1:])
	if err != nil {
		return 0, nil, nil, err
	}
	start := 1 + hdr
	if n > len(b)-start {
		return 0, nil, nil, errBER
	}
	return tag, b[start : start+n], b[start+n:], nil
}

// decodeLen 解析长度字段，返回长度与长度字段本身占的字节数。拒绝不定长与超过 4 字节的长度。
func decodeLen(b []byte) (n, used int, err error) {
	if len(b) == 0 {
		return 0, 0, errBER
	}
	if b[0] < 0x80 {
		return int(b[0]), 1, nil
	}
	k := int(b[0] & 0x7f)
	if k == 0 || k > 4 || len(b) < 1+k {
		return 0, 0, errBER
	}
	for _, c := range b[1 : 1+k] {
		n = n<<8 | int(c)
	}
	if n < 0 {
		return 0, 0, errBER
	}
	return n, 1 + k, nil
}

// readTLV 从流里读一个完整 TLV（返回完整字节，含头），值长度超过 max 即报错，防止恶意服务端撑爆内存。
func readTLV(r io.Reader, max int) ([]byte, error) {
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return nil, err
	}
	if hdr[0]&0x1f == 0x1f {
		return nil, errBER
	}
	lenBytes := []byte{hdr[1]}
	if hdr[1] >= 0x80 {
		k := int(hdr[1] & 0x7f)
		if k == 0 || k > 4 {
			return nil, errBER
		}
		ext := make([]byte, k)
		if _, err := io.ReadFull(r, ext); err != nil {
			return nil, err
		}
		lenBytes = append(lenBytes, ext...)
	}
	n, _, err := decodeLen(lenBytes)
	if err != nil {
		return nil, err
	}
	if n > max {
		return nil, fmt.Errorf("ldap: response too large (%d > %d bytes)", n, max)
	}
	out := append([]byte{hdr[0]}, lenBytes...)
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return append(out, body...), nil
}

func decodeInt(b []byte) (int, error) {
	if len(b) == 0 || len(b) > 4 {
		return 0, errBER
	}
	v := int(int8(b[0]))
	for _, c := range b[1:] {
		v = v<<8 | int(c)
	}
	return v, nil
}
