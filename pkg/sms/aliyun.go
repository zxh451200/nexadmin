package sms

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // 阿里云 RPC 签名 v1 规定使用 HMAC-SHA1
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// aliyun：短信服务 Dysmsapi（2017-05-25）SendSms，RPC 风格签名 v1（HMAC-SHA1）。
// AccessKey 请用只有短信权限的 RAM 子账号，并在配置里写 ${ENV} 引用。
type aliyun struct {
	keyID, secret, sign, endpoint string
	hc                            *http.Client
	now                           func() time.Time
}

func newAliyun(c Config) (Provider, error) {
	if c.AliyunKeyID == "" || c.AliyunKeySecret == "" || c.AliyunSignName == "" {
		return nil, fmt.Errorf("sms: aliyun: access_key_id, access_key_secret and sign_name are required")
	}
	ep := strings.TrimRight(c.AliyunEndpoint, "/")
	if ep == "" {
		ep = "https://dysmsapi.aliyuncs.com"
	}
	return &aliyun{keyID: c.AliyunKeyID, secret: c.AliyunKeySecret, sign: c.AliyunSignName, endpoint: ep,
		hc: &http.Client{Timeout: c.Timeout}, now: time.Now}, nil
}

// percentEncode 是 RPC 签名要求的编码：RFC 3986，空格为 %20，* 编码，~ 不编码。
func percentEncode(s string) string {
	s = url.QueryEscape(s)
	s = strings.ReplaceAll(s, "+", "%20")
	s = strings.ReplaceAll(s, "*", "%2A")
	return strings.ReplaceAll(s, "%7E", "~")
}

// rpcSign 计算 RPC 签名 v1：params 不含 Signature；key 为 AccessKeySecret + "&"。
func rpcSign(method string, params map[string]string, secret string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, percentEncode(k)+"="+percentEncode(params[k]))
	}
	toSign := method + "&" + percentEncode("/") + "&" + percentEncode(strings.Join(parts, "&"))
	m := hmac.New(sha1.New, []byte(secret+"&"))
	m.Write([]byte(toSign))
	return base64.StdEncoding.EncodeToString(m.Sum(nil))
}

func (a *aliyun) Send(ctx context.Context, phone, template string, params map[string]string) error {
	tp, err := json.Marshal(params)
	if err != nil {
		return err
	}
	nb := make([]byte, 16)
	if _, err := rand.Read(nb); err != nil {
		return err
	}
	p := map[string]string{
		"AccessKeyId": a.keyID, "Action": "SendSms", "Format": "JSON", "PhoneNumbers": phone, "RegionId": "cn-hangzhou",
		"SignName": a.sign, "SignatureMethod": "HMAC-SHA1", "SignatureNonce": hex.EncodeToString(nb),
		"SignatureVersion": "1.0", "TemplateCode": template, "TemplateParam": string(tp),
		"Timestamp": a.now().UTC().Format("2006-01-02T15:04:05Z"), "Version": "2017-05-25",
	}
	p["Signature"] = rpcSign(http.MethodPost, p, a.secret)
	form := url.Values{}
	for k, v := range p {
		form.Set(k, v)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint+"/", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.hc.Do(req)
	if err != nil {
		return fmt.Errorf("sms: aliyun request failed: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		Code, Message, RequestID string
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err := json.Unmarshal(raw, &out); err != nil || resp.StatusCode/100 != 2 {
		return fmt.Errorf("sms: aliyun HTTP %d", resp.StatusCode)
	}
	if out.Code != "OK" {
		return fmt.Errorf("sms: aliyun code=%s message=%s requestId=%s", out.Code, out.Message, out.RequestID)
	}
	return nil
}
