package ldap

// 协议级验收：用最小 LDAP 服务端（真实 TCP + BER）验证 authenticator 全路径。
// 不依赖外部目录服务；AD 特有行为（分页、referral、TLS）仍需真实目录联调。

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"

	"nexadmin/core/auth"
)

const (
	svcDN    = "cn=svc,dc=example,dc=com"
	svcPass  = "svc-secret"
	userDN   = "uid=alice,ou=people,dc=example,dc=com"
	userPass = "alice-secret"
)

type fakeLDAP struct {
	ln      net.Listener
	port    int
	binds   []string
	tlsCert *tls.Certificate
}

func startFakeLDAP(t *testing.T) *fakeLDAP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeLDAP{ln: ln, port: ln.Addr().(*net.TCPAddr).Port}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.handle(c)
		}
	}()
	return f
}

func (f *fakeLDAP) handle(c net.Conn) {
	defer c.Close()
	for {
		tag, val, err := readTLV(c)
		if err != nil || tag != 0x30 {
			return
		}
		children := splitTLV(val)
		if len(children) < 2 {
			return
		}
		msgID := intFrom(children[0].val)
		switch children[1].tag {
		case 0x77: // ExtendedRequest（StartTLS）
			if f.tlsCert == nil {
				return
			}
			_, _ = c.Write(message(msgID, extendedResponse()))
			tc := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{*f.tlsCert}, MinVersion: tls.VersionTLS12})
			_ = tc.SetDeadline(time.Now().Add(3 * time.Second))
			if err := tc.Handshake(); err != nil {
				return
			}
			c = tc
		case 0x60: // BindRequest
			parts := splitTLV(children[1].val)
			if len(parts) < 3 {
				return
			}
			dn, pass := string(parts[1].val), string(parts[2].val)
			f.binds = append(f.binds, dn)
			code := 49
			if (dn == svcDN && pass == svcPass) || (dn == userDN && pass == userPass) {
				code = 0
			}
			_, _ = c.Write(message(msgID, bindResponse(code)))
		case 0x63: // SearchRequest
			_, _ = c.Write(message(msgID, searchEntry()))
			_, _ = c.Write(message(msgID, searchDone()))
		case 0x42: // UnbindRequest
			return
		default:
			return
		}
	}
}

type tlv struct {
	tag byte
	val []byte
}

func splitTLV(b []byte) []tlv {
	var out []tlv
	for len(b) > 0 {
		tag, val, rest, ok := parseTLV(b)
		if !ok {
			return out
		}
		out = append(out, tlv{tag, val})
		b = rest
	}
	return out
}

func readTLV(r io.Reader) (byte, []byte, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := int(hdr[1])
	if n == 0x81 {
		var b [1]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, nil, err
		}
		n = int(b[0])
	} else if n == 0x82 {
		var b [2]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, nil, err
		}
		n = int(binary.BigEndian.Uint16(b[:]))
	} else if n > 0x80 {
		return 0, nil, errors.New("unsupported length")
	}
	val := make([]byte, n)
	if _, err := io.ReadFull(r, val); err != nil {
		return 0, nil, err
	}
	return hdr[0], val, nil
}

func parseTLV(b []byte) (byte, []byte, []byte, bool) {
	if len(b) < 2 {
		return 0, nil, nil, false
	}
	tag, n := b[0], int(b[1])
	off := 2
	switch b[1] {
	case 0x81:
		if len(b) < 3 {
			return 0, nil, nil, false
		}
		n, off = int(b[2]), 3
	case 0x82:
		if len(b) < 4 {
			return 0, nil, nil, false
		}
		n, off = int(binary.BigEndian.Uint16(b[2:4])), 4
	}
	if len(b) < off+n {
		return 0, nil, nil, false
	}
	return tag, b[off : off+n], b[off+n:], true
}

func intFrom(b []byte) int {
	var n int
	for _, c := range b {
		n = n<<8 | int(c)
	}
	return n
}

func encInt(tag byte, n int) []byte {
	var b []byte
	for {
		b = append([]byte{byte(n & 0xff)}, b...)
		n >>= 8
		if n == 0 {
			break
		}
	}
	if b[0]&0x80 != 0 {
		b = append([]byte{0}, b...)
	}
	return enc(tag, b)
}

func enc(tag byte, val []byte) []byte {
	out := []byte{tag}
	switch {
	case len(val) < 0x80:
		out = append(out, byte(len(val)))
	case len(val) < 0x100:
		out = append(out, 0x81, byte(len(val)))
	default:
		out = append(out, 0x82, byte(len(val)>>8), byte(len(val)))
	}
	return append(out, val...)
}

func seq(parts ...[]byte) []byte    { return enc(0x30, bytes.Join(parts, nil)) }
func str(tag byte, s string) []byte { return enc(tag, []byte(s)) }

func message(msgID int, op []byte) []byte { return seq(encInt(0x02, msgID), op) }

func bindResponse(code int) []byte {
	return enc(0x61, bytes.Join([][]byte{encInt(0x0A, code), str(0x04, ""), str(0x04, "")}, nil))
}

func searchEntry() []byte {
	attr := func(name, value string) []byte {
		return seq(str(0x04, name), enc(0x31, str(0x04, value)))
	}
	attrs := seq(attr("uid", "alice"), attr("mail", "alice@example.com"), attr("cn", "Alice"))
	return enc(0x64, bytes.Join([][]byte{str(0x04, userDN), attrs}, nil))
}

func extendedResponse() []byte {
	return enc(0x78, bytes.Join([][]byte{encInt(0x0A, 0), str(0x04, ""), str(0x04, "")}, nil))
}

func searchDone() []byte {
	return enc(0x65, bytes.Join([][]byte{encInt(0x0A, 0), str(0x04, ""), str(0x04, "")}, nil))
}

func configDB(t *testing.T, port int, bindPass string, calls int) *gorm.DB {
	return configDBWith(t, port, bindPass, false, calls)
}

func configDBWith(t *testing.T, port int, bindPass string, startTLS bool, calls int) *gorm.DB {
	return configDBCA(t, port, bindPass, startTLS, "", calls)
}

func configDBCA(t *testing.T, port int, bindPass string, startTLS bool, caCert string, calls int) *gorm.DB {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	cols := []string{"id", "enabled", "host", "port", "use_tls", "start_tls", "bind_dn", "bind_password", "base_dn", "user_filter",
		"attr_username", "attr_email", "attr_nickname", "match_existing", "auto_create", "default_role", "timeout_ms", "ca_cert", "created_at", "updated_at"}
	for i := 0; i < calls; i++ {
		mock.ExpectQuery("SELECT \\* FROM `plg_ldap_config`").WillReturnRows(sqlmock.NewRows(cols).AddRow(
			1, true, "127.0.0.1", port, false, startTLS, svcDN, bindPass, "dc=example,dc=com", "(uid=%s)",
			"uid", "mail", "cn", false, false, "", 3000, caCert, time.Now(), time.Now(),
		))
	}
	return db
}

func TestAuthenticateAgainstFakeDirectory(t *testing.T) {
	f := startFakeLDAP(t)
	a := &authenticator{svc: NewService(configDB(t, f.port, svcPass, 2), nil)}

	id, err := a.Authenticate(context.Background(), "alice", userPass)
	if err != nil {
		t.Fatalf("valid login: %v", err)
	}
	if id.Subject != userDN || id.Username != "alice" || id.Email != "alice@example.com" || id.Nickname != "Alice" || id.ProviderKey != "ldap" {
		t.Fatalf("identity: %+v", id)
	}
	if len(f.binds) != 2 || f.binds[0] != svcDN || f.binds[1] != userDN {
		t.Fatalf("expected service bind then user bind, got %v", f.binds)
	}

	if _, err := a.Authenticate(context.Background(), "alice", "wrong"); !errors.Is(err, auth.ErrExternalRejected) {
		t.Fatalf("wrong password must be rejected: %v", err)
	}
}

func TestAuthenticateServiceBindFailureIsNotCredentialError(t *testing.T) {
	f := startFakeLDAP(t)
	a := &authenticator{svc: NewService(configDB(t, f.port, "wrong-svc-pass", 1), nil)}
	if _, err := a.Authenticate(context.Background(), "alice", userPass); err == nil || errors.Is(err, auth.ErrExternalRejected) {
		t.Fatalf("service bind failure must surface as configuration error: %v", err)
	}
}

func TestAuthenticateConnectionFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	a := &authenticator{svc: NewService(configDB(t, port, svcPass, 1), nil)}
	if _, err := a.Authenticate(context.Background(), "alice", userPass); err == nil {
		t.Fatal("closed port must produce an error")
	}
}

func TestTestConnectionAgainstFakeDirectory(t *testing.T) {
	f := startFakeLDAP(t)
	svc := NewService(configDB(t, f.port, svcPass, 1), nil)
	if err := svc.TestConnection(context.Background()); err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if len(f.binds) != 1 || f.binds[0] != svcDN {
		t.Fatalf("test connection must bind the service account once, got %v", f.binds)
	}
}

func TestTestConnectionRequiresHostAndBaseDN(t *testing.T) {
	// 无配置行 → defaultConfig（host/base_dn 为空）→ 必须在 dial 之前拒绝。
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT \\* FROM `plg_ldap_config`").WillReturnError(gorm.ErrRecordNotFound)
	err = NewService(db, nil).TestConnection(context.Background())
	if err == nil || !strings.Contains(err.Error(), "ldap.host_basedn_required") {
		t.Fatalf("missing host/base_dn must be rejected before dialing, got %v", err)
	}
}

// StartTLS 只信任系统根证书：自签证书必须被拒绝，不能静默跳过校验。
func TestStartTLSRejectsSelfSignedCertificate(t *testing.T) {
	f := startFakeLDAP(t)
	f.tlsCert = selfSignedCert(t)
	a := &authenticator{svc: NewService(configDBWith(t, f.port, svcPass, true, 1), nil)}
	_, err := a.Authenticate(context.Background(), "alice", userPass)
	if err == nil || errors.Is(err, auth.ErrExternalRejected) {
		t.Fatalf("self-signed StartTLS must fail as a transport error, got %v", err)
	}
	if !strings.Contains(err.Error(), "starttls") {
		t.Fatalf("expected starttls error, got %v", err)
	}
}

func selfSignedCert(t *testing.T) *tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "fake-ldap"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// 配置了内网自签 CA 后，StartTLS 必须能正常握手（此前的正向用例因只信任系统根证书无法覆盖）。
func TestStartTLSTrustsConfiguredCA(t *testing.T) {
	f := startFakeLDAP(t)
	f.tlsCert = selfSignedCert(t)
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.tlsCert.Certificate[0]})
	a := &authenticator{svc: NewService(configDBCA(t, f.port, svcPass, true, string(caPEM), 1), nil)}
	id, err := a.Authenticate(context.Background(), "alice", userPass)
	if err != nil {
		t.Fatalf("StartTLS with trusted CA: %v", err)
	}
	if id.Subject != userDN {
		t.Fatalf("identity: %+v", id)
	}
}

func TestTLSConfigCAValidation(t *testing.T) {
	if _, err := tlsConfig("not a pem", "127.0.0.1"); err == nil {
		t.Fatal("invalid PEM must be rejected at dial time")
	}
	cfg, err := tlsConfig("", "127.0.0.1")
	if err != nil || cfg.RootCAs != nil {
		t.Fatalf("empty CA must use system roots: %+v %v", cfg, err)
	}
	if validCACert("not a pem") {
		t.Fatal("validCACert must reject invalid PEM")
	}
}
