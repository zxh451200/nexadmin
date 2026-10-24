package identity

import (
	"errors"
	"strings"
	"testing"
)

const casOKXML = `<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas">
  <cas:authenticationSuccess>
    <cas:user>  carol  </cas:user>
    <cas:attributes>
      <cas:displayName>Carol C</cas:displayName>
      <cas:mail>carol@example.com</cas:mail>
      <cas:mail>second@example.com</cas:mail>
      <cas:sAMAccountName>carol.c</cas:sAMAccountName>
    </cas:attributes>
  </cas:authenticationSuccess>
</cas:serviceResponse>`

func TestParseCASSuccess(t *testing.T) {
	r, err := parseCASResponse([]byte(casOKXML))
	if err != nil {
		t.Fatal(err)
	}
	if r.User != "carol" || r.Attrs["displayname"][0] != "Carol C" || len(r.Attrs["mail"]) != 2 {
		t.Fatalf("result = %+v", r)
	}
	id, err := r.identity(CASConfig{AttrNickname: "displayName", AttrEmail: "mail", AttrUsername: "sAMAccountName", MatchExisting: true, DefaultRole: "staff"})
	if err != nil || id.Username != "carol.c" || id.Subject != "carol.c" || id.Nickname != "Carol C" || id.Email != "carol@example.com" || !id.MatchExisting || id.AutoCreate {
		t.Fatalf("identity = %+v err=%v", id, err)
	}
	// 没配属性映射：用 cas:user
	if id, err := r.identity(CASConfig{}); err != nil || id.Username != "carol" {
		t.Fatalf("default mapping: %+v %v", id, err)
	}
	// 账号名不合规：拒绝，不猜测式转换
	bad := &casResult{User: "ca rol/../x"}
	if _, err := bad.identity(CASConfig{}); err == nil {
		t.Fatal("invalid principal must be rejected")
	}
}

func TestParseCASFailure(t *testing.T) {
	_, err := parseCASResponse([]byte(`<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas"><cas:authenticationFailure code="INVALID_TICKET">Ticket ST-1 not recognized</cas:authenticationFailure></cas:serviceResponse>`))
	var cf *casFailure
	if !errors.As(err, &cf) || cf.Code != "INVALID_TICKET" {
		t.Fatalf("err = %v", err)
	}
}

func TestParseCASRejectsMalformedAndHostile(t *testing.T) {
	ok := func(inner string) string {
		return `<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas">` + inner + `</cas:serviceResponse>`
	}
	cases := map[string]string{
		"empty":           "",
		"not xml":         "hello",
		"truncated":       `<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas"><cas:authenticationSuccess><cas:user>x`,
		"wrong namespace": `<cas:serviceResponse xmlns:cas="http://evil.example/ns"><cas:authenticationSuccess><cas:user>carol</cas:user></cas:authenticationSuccess></cas:serviceResponse>`,
		"empty user":      ok(`<cas:authenticationSuccess><cas:user>   </cas:user></cas:authenticationSuccess>`),
		"no user":         ok(`<cas:authenticationSuccess></cas:authenticationSuccess>`),
		"control char":    ok("<cas:authenticationSuccess><cas:user>a&#1;b</cas:user></cas:authenticationSuccess>"),
		"both":            ok(`<cas:authenticationSuccess><cas:user>carol</cas:user></cas:authenticationSuccess><cas:authenticationFailure code="X"/>`),
		"neither":         ok(`<cas:other/>`),
		"wrong root":      `<serviceResponse><authenticationSuccess><user>carol</user></authenticationSuccess></serviceResponse>`,
		// XXE：外部实体、参数实体、内部实体炸弹，全部必须被拒绝，且不会读文件 / 发请求
		"xxe file": `<?xml version="1.0"?><!DOCTYPE r [<!ENTITY xxe SYSTEM "file:///etc/passwd">]>` +
			`<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas"><cas:authenticationSuccess><cas:user>&xxe;</cas:user></cas:authenticationSuccess></cas:serviceResponse>`,
		"xxe http": `<!DOCTYPE r [<!ENTITY % remote SYSTEM "http://127.0.0.1:1/evil.dtd">%remote;]>` +
			`<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas"><cas:authenticationSuccess><cas:user>carol</cas:user></cas:authenticationSuccess></cas:serviceResponse>`,
		"entity bomb": `<!DOCTYPE lolz [<!ENTITY lol "lol"><!ENTITY lol2 "&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;">]>` +
			`<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas"><cas:authenticationSuccess><cas:user>&lol2;</cas:user></cas:authenticationSuccess></cas:serviceResponse>`,
		"undeclared entity": ok(`<cas:authenticationSuccess><cas:user>&xxe;</cas:user></cas:authenticationSuccess>`),
		"doctype only":      `<!DOCTYPE foo>` + ok(`<cas:authenticationSuccess><cas:user>carol</cas:user></cas:authenticationSuccess>`),
	}
	for name, body := range cases {
		r, err := parseCASResponse([]byte(body))
		if err == nil {
			t.Errorf("%s: must be rejected, got %+v", name, r)
		}
		var cf *casFailure
		if name == "both" && errors.As(err, &cf) {
			t.Errorf("both: must not be treated as a plain CAS failure")
		}
	}
}

func TestParseCASNeverLeaksEntityContent(t *testing.T) {
	body := `<!DOCTYPE r [<!ENTITY xxe SYSTEM "file:///etc/passwd">]><cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas"><cas:authenticationSuccess><cas:user>&xxe;</cas:user></cas:authenticationSuccess></cas:serviceResponse>`
	r, err := parseCASResponse([]byte(body))
	if err == nil || r != nil || strings.Contains(err.Error(), "root:") {
		t.Fatalf("r=%v err=%v", r, err)
	}
}

func TestCASServiceURLComesFromConfigOnly(t *testing.T) {
	cfg := CASConfig{ServerURL: "https://cas.example.com/cas", ServiceBaseURL: "https://admin.example.com", Version: "3"}
	su := casServiceURL(cfg, 42, "abc123")
	if su != "https://admin.example.com/api/v1/plugins/identity/cas/callback?source=42&state=abc123" {
		t.Fatalf("service = %s", su)
	}
	if got := casLoginRedirect(cfg, su); !strings.HasPrefix(got, "https://cas.example.com/cas/login?service=https%3A%2F%2Fadmin.example.com%2F") {
		t.Fatalf("login redirect = %s", got)
	}
	if got := casLogoutRedirect(cfg); got != "https://cas.example.com/cas/logout?service=https%3A%2F%2Fadmin.example.com%2F" {
		t.Fatalf("logout redirect = %s", got)
	}
	if casValidatePath(cfg) != "/p3/serviceValidate" || casValidatePath(CASConfig{Version: "2"}) != "/serviceValidate" {
		t.Fatal("validate path by version")
	}
}

func TestCASConfigNormalize(t *testing.T) {
	good := CASConfig{ServerURL: "https://cas.example.com/cas/", ServiceBaseURL: "https://admin.example.com/", Version: "2"}
	if err := good.normalize(); err != nil || good.ServerURL != "https://cas.example.com/cas" || good.ServiceBaseURL != "https://admin.example.com" {
		t.Fatalf("cfg=%+v err=%v", good, err)
	}
	for name, c := range map[string]CASConfig{
		"ftp server":        {ServerURL: "ftp://x", ServiceBaseURL: "https://a"},
		"userinfo":          {ServerURL: "https://u:p@cas.example.com", ServiceBaseURL: "https://a"},
		"query":             {ServerURL: "https://cas.example.com?x=1", ServiceBaseURL: "https://a"},
		"base has path":     {ServerURL: "https://cas.example.com", ServiceBaseURL: "https://a/b"},
		"base missing":      {ServerURL: "https://cas.example.com"},
		"bad port":          {ServerURL: "https://cas.example.com:99999", ServiceBaseURL: "https://a"},
		"bad version":       {ServerURL: "https://cas.example.com", ServiceBaseURL: "https://a", Version: "9"},
		"javascript scheme": {ServerURL: "javascript:alert(1)", ServiceBaseURL: "https://a"},
	} {
		c := c
		if err := c.normalize(); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
}
