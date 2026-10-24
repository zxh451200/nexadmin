package identity

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- 内存 LDAP 测试服务：用同一套 BER 编解码实现 bind / search / StartTLS，足以端到端验证客户端 ----

type testEntry struct {
	dn       string
	attrs    map[string][]string // 键为小写属性名
	password string
}

type bindRec struct {
	dn        string
	emptyPass bool
	succeeded bool
}

type testLDAP struct {
	t        *testing.T
	ln       net.Listener
	tlsCfg   *tls.Config
	implicit bool // ldaps：连接即 TLS

	mu            sync.Mutex
	entries       []testEntry
	binds         []bindRec
	filters       []string
	unauthSuccess bool // 模拟“DN + 空口令也返回成功”的服务端（未认证绑定），用来证明客户端不会发出空口令 bind
	bindDelay     time.Duration
	noPaging      bool   // 模拟不支持 RFC 2696 的服务端：忽略分页 control，一次返回全部（受 sizeLimit 约束）
	pageReqs      []bool // 每次搜索请求是否带了 control
}

func newTestLDAP(t *testing.T, entries []testEntry, tlsCfg *tls.Config, implicit bool) *testLDAP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen on loopback: %v", err)
	}
	s := &testLDAP{t: t, ln: ln, entries: entries, tlsCfg: tlsCfg, implicit: implicit}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.handle(c)
		}
	}()
	return s
}

func (s *testLDAP) hostPort() string { return s.ln.Addr().String() }

func (s *testLDAP) url() string {
	if s.implicit {
		return "ldaps://" + s.hostPort()
	}
	return "ldap://" + s.hostPort()
}

func (s *testLDAP) bindLog() []bindRec {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]bindRec(nil), s.binds...)
}

func (s *testLDAP) filterLog() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.filters...)
}

func ldapResult(tag byte, code int, diag string) []byte {
	return enc(tag, encInt(tagEnum, code), encStr(tagOctet, ""), encStr(tagOctet, diag))
}

func ldapMsg(id int, op []byte) []byte { return enc(tagSeq, encInt(tagInt, id), op) }

func (s *testLDAP) handle(raw net.Conn) {
	defer raw.Close()
	var conn net.Conn = raw
	if s.implicit {
		conn = tls.Server(raw, s.tlsCfg)
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(conn)
	for {
		msg, err := readTLV(br, 1<<20)
		if err != nil {
			return
		}
		_, body, _, _ := parseTLV(msg)
		_, idv, rest, _ := parseTLV(body)
		id, _ := decodeInt(idv)
		tag, op, ctl, err := parseTLV(rest)
		if err != nil {
			return
		}
		switch tag {
		case tagUnbindReq:
			return
		case tagBindReq:
			_, _ = conn.Write(ldapMsg(id, s.doBind(op)))
		case tagSearchReq:
			for _, m := range s.doSearch(id, op, ctl) {
				_, _ = conn.Write(m)
			}
		case tagExtReq:
			if s.tlsCfg == nil || s.implicit {
				_, _ = conn.Write(ldapMsg(id, ldapResult(tagExtResp, 2, "no TLS")))
				continue
			}
			_, _ = conn.Write(ldapMsg(id, ldapResult(tagExtResp, 0, "")))
			tc := tls.Server(conn, s.tlsCfg)
			conn, br = tc, bufio.NewReader(tc)
		}
	}
}

func (s *testLDAP) doBind(op []byte) []byte {
	_, _, rest, _ := parseTLV(op) // version
	_, dn, rest, _ := parseTLV(rest)
	_, pw, _, _ := parseTLV(rest)
	rec := bindRec{dn: string(dn), emptyPass: len(pw) == 0}
	code := 49
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if strings.EqualFold(e.dn, rec.dn) && ((len(pw) > 0 && e.password == string(pw)) || (len(pw) == 0 && s.unauthSuccess)) {
			code = 0
		}
	}
	rec.succeeded = code == 0
	s.binds = append(s.binds, rec)
	if s.bindDelay > 0 {
		time.Sleep(s.bindDelay)
	}
	return ldapResult(tagBindResp, code, "")
}

// testCerts 生成一个自签 CA 与一张 127.0.0.1 的服务端证书，返回 CA 的 PEM 与服务端 TLS 配置。
func testCerts(t *testing.T) (caPEM string, server *tls.Config) {
	t.Helper()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-ca"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, caTpl, caTpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, _ := x509.ParseCertificate(caDER)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, DNSNames: []string{"localhost"},
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	pair := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})), &tls.Config{Certificates: []tls.Certificate{pair}}
}
