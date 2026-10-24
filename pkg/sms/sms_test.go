package sms

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 阿里云文档里的签名示例（testid / testsecret）。
func TestRPCSignKnownVectors(t *testing.T) {
	cases := []struct {
		name   string
		method string
		params map[string]string
		want   string
	}{
		{"DescribeRegions", "GET", map[string]string{
			"Timestamp": "2016-02-23T12:46:24Z", "Format": "XML", "AccessKeyId": "testid", "Action": "DescribeRegions",
			"SignatureMethod": "HMAC-SHA1", "SignatureNonce": "3ee8c1b8-83d3-44af-a94f-4e0ad82fd6cf",
			"Version": "2014-05-26", "SignatureVersion": "1.0"}, "OLeaidS1JvxuMvnyHOwuJ+uX5qY="},
		{"DescribeDedicatedHosts", "GET", map[string]string{
			"AccessKeyId": "testid", "Action": "DescribeDedicatedHosts", "Format": "JSON", "RegionId": "cn-beijing",
			"SignatureMethod": "HMAC-SHA1", "SignatureNonce": "edb2b34af0af9a6d14deaf7c1a5315eb", "SignatureVersion": "1.0",
			"Timestamp": "2023-03-13T08:34:30Z", "Version": "2014-05-26"}, "9NaGiOspFP5UPcwX8Iwt2YJXXuk="},
	}
	for _, c := range cases {
		if got := rpcSign(c.method, c.params, "testsecret"); got != c.want {
			t.Errorf("%s: signature = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestPercentEncode(t *testing.T) {
	if got := percentEncode("a b*c~d/e+f"); got != "a%20b%2Ac~d%2Fe%2Bf" {
		t.Fatalf("percentEncode = %s", got)
	}
}

func TestAliyunSend(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got, _ = url.ParseQuery(string(b))
		_, _ = w.Write([]byte(`{"Code":"OK","Message":"OK","RequestId":"r1"}`))
	}))
	defer srv.Close()
	p, err := New(Config{Provider: "aliyun", AliyunKeyID: "testid", AliyunKeySecret: "testsecret", AliyunSignName: "NexAdmin", AliyunEndpoint: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Send(context.Background(), "13800138000", "SMS_1", map[string]string{"code": "123456"}); err != nil {
		t.Fatal(err)
	}
	sig := got.Get("Signature")
	got.Del("Signature")
	flat := map[string]string{}
	for k := range got {
		flat[k] = got.Get(k)
	}
	if sig == "" || sig != rpcSign("POST", flat, "testsecret") {
		t.Fatalf("signature mismatch: %q", sig)
	}
	if got.Get("PhoneNumbers") != "13800138000" || got.Get("TemplateCode") != "SMS_1" || got.Get("TemplateParam") != `{"code":"123456"}` || got.Get("Action") != "SendSms" {
		t.Fatalf("bad params: %v", got)
	}
}

func TestAliyunBusinessError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Code":"isv.BUSINESS_LIMIT_CONTROL","Message":"limit","RequestId":"r2"}`))
	}))
	defer srv.Close()
	p, _ := New(Config{Provider: "aliyun", AliyunKeyID: "a", AliyunKeySecret: "b", AliyunSignName: "c", AliyunEndpoint: srv.URL})
	if err := p.Send(context.Background(), "13800138000", "T", nil); err == nil || !strings.Contains(err.Error(), "BUSINESS_LIMIT_CONTROL") {
		t.Fatalf("want business error, got %v", err)
	}
}

func TestWebhookHMAC(t *testing.T) {
	const secret = "s3cret"
	var body []byte
	var hdr http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		hdr = r.Header.Clone()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	p, err := New(Config{Provider: "webhook", WebhookURL: srv.URL, WebhookSecret: secret})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Send(context.Background(), "13800138000", "login", map[string]string{"code": "654321"}); err != nil {
		t.Fatal(err)
	}
	ts, nonce := hdr.Get("X-Timestamp"), hdr.Get("X-Nonce")
	if want := WebhookSign(secret, ts, nonce, body); hdr.Get("X-Signature") != want {
		t.Fatalf("signature = %s, want %s", hdr.Get("X-Signature"), want)
	}
	if WebhookSign("other", ts, nonce, body) == hdr.Get("X-Signature") {
		t.Fatal("signature must depend on secret")
	}
	var b webhookBody
	if err := json.Unmarshal(body, &b); err != nil || b.Phone != "13800138000" || b.Template != "login" || b.Params["code"] != "654321" || b.Nonce != nonce {
		t.Fatalf("bad body: %s", body)
	}
	if n, _ := strconv.ParseInt(ts, 10, 64); time.Since(time.Unix(n, 0)) > time.Minute || b.Timestamp != n {
		t.Fatalf("bad timestamp %s", ts)
	}
}

func TestWebhookNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
	defer srv.Close()
	p, _ := New(Config{Provider: "webhook", WebhookURL: srv.URL, WebhookSecret: "x"})
	if err := p.Send(context.Background(), "13800138000", "t", nil); err == nil {
		t.Fatal("want error on HTTP 500")
	}
}

func TestNewValidation(t *testing.T) {
	if p, err := New(Config{}); p != nil || err != nil {
		t.Fatalf("empty provider should be disabled, got %v %v", p, err)
	}
	for _, c := range []Config{{Provider: "nope"}, {Provider: "webhook"}, {Provider: "webhook", WebhookURL: "ftp://x", WebhookSecret: "s"}, {Provider: "aliyun"}} {
		if _, err := New(c); err == nil {
			t.Errorf("want error for %+v", c)
		}
	}
	if p, err := New(Config{Provider: "log"}); err != nil || p == nil {
		t.Fatalf("log provider: %v", err)
	}
}
