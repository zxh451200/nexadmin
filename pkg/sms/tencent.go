package sms

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// tencent 使用短信 API 2021-01-11 SendSms，TC3-HMAC-SHA256 签名。
type tencent struct {
	secretID, secretKey, appID, sign, region, endpoint string
	paramOrder                                         map[string][]string
	hc                                                 *http.Client
	now                                                func() time.Time
}

func newTencent(c Config) (Provider, error) {
	if strings.TrimSpace(c.TencentSecretID) == "" || strings.TrimSpace(c.TencentSecretKey) == "" ||
		strings.TrimSpace(c.TencentSDKAppID) == "" || strings.TrimSpace(c.TencentSignName) == "" {
		return nil, fmt.Errorf("sms: tencent: secret_id, secret_key, sdk_app_id and sign_name are required")
	}
	endpoint := strings.TrimRight(strings.TrimSpace(c.TencentEndpoint), "/")
	if endpoint == "" {
		endpoint = "https://sms.tencentcloudapi.com"
	}
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" ||
		u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, fmt.Errorf("sms: tencent: endpoint must be an http(s) origin without path, credentials, query or fragment")
	}
	region := strings.TrimSpace(c.TencentRegion)
	if region == "" {
		region = "ap-guangzhou"
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if c.Proxy != "" {
		proxy, err := url.Parse(c.Proxy)
		if err != nil || proxy.Host == "" ||
			(proxy.Scheme != "http" && proxy.Scheme != "https" && proxy.Scheme != "socks5" && proxy.Scheme != "socks5h") {
			return nil, fmt.Errorf("sms: tencent: invalid proxy URL")
		}
		tr.Proxy = http.ProxyURL(proxy)
	}
	order := make(map[string][]string, len(c.TencentParamOrder))
	for template, keys := range c.TencentParamOrder {
		seen := make(map[string]bool, len(keys))
		for _, key := range keys {
			if strings.TrimSpace(key) == "" || seen[key] {
				return nil, fmt.Errorf("sms: tencent: param_order contains an empty or duplicate parameter name")
			}
			seen[key] = true
		}
		order[template] = append([]string{}, keys...)
	}
	return &tencent{
		secretID: c.TencentSecretID, secretKey: c.TencentSecretKey, appID: c.TencentSDKAppID,
		sign: c.TencentSignName, region: region, endpoint: endpoint, paramOrder: order,
		hc: &http.Client{
			Timeout: c.Timeout, Transport: tr,
			// 重定向会改变签名的 Host，也不能把凭据转发到其它地址。
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		},
		now: time.Now,
	}, nil
}

func tencentSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func tencentHMAC(key []byte, value string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(value))
	return m.Sum(nil)
}

func (t *tencent) authorize(req *http.Request, body []byte, now time.Time) {
	const signedHeaders = "content-type;host"
	const contentType = "application/json; charset=utf-8"
	date := now.UTC().Format("2006-01-02")
	timestamp := strconv.FormatInt(now.Unix(), 10)
	scope := date + "/sms/tc3_request"
	canonicalHeaders := "content-type:" + contentType + "\nhost:" + req.URL.Host + "\n"
	canonicalRequest := "POST\n/\n\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + tencentSHA256(body)
	stringToSign := "TC3-HMAC-SHA256\n" + timestamp + "\n" + scope + "\n" + tencentSHA256([]byte(canonicalRequest))
	dateKey := tencentHMAC([]byte("TC3"+t.secretKey), date)
	serviceKey := tencentHMAC(dateKey, "sms")
	signingKey := tencentHMAC(serviceKey, "tc3_request")
	signature := hex.EncodeToString(tencentHMAC(signingKey, stringToSign))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-TC-Action", "SendSms")
	req.Header.Set("X-TC-Version", "2021-01-11")
	req.Header.Set("X-TC-Region", t.region)
	req.Header.Set("X-TC-Timestamp", timestamp)
	req.Header.Set("Authorization", "TC3-HMAC-SHA256 Credential="+t.secretID+"/"+scope+
		", SignedHeaders="+signedHeaders+", Signature="+signature)
}

func (t *tencent) Send(ctx context.Context, phone, template string, params map[string]string) error {
	phone = strings.TrimSpace(phone)
	// 现有验证码服务使用国内 11 位号码；国际号码由调用方传入 E.164 格式。
	if len(phone) == 11 && phone[0] == '1' {
		phone = "+86" + phone
	}
	if len(phone) < 3 || len(phone) > 16 || phone[0] != '+' || phone[1] == '0' {
		return fmt.Errorf("sms: tencent: phone must be E.164 or an 11-digit mainland China mobile number")
	}
	for _, ch := range phone[1:] {
		if ch < '0' || ch > '9' {
			return fmt.Errorf("sms: tencent: invalid phone number")
		}
	}
	if strings.TrimSpace(template) == "" {
		return fmt.Errorf("sms: tencent: template ID is required")
	}
	keys, configured := t.paramOrder[template]
	if !configured {
		keys = make([]string, 0, len(params))
		for key := range params {
			keys = append(keys, key)
		}
		sort.Strings(keys)
	}
	if len(keys) != len(params) {
		return fmt.Errorf("sms: tencent: template parameters do not match param_order")
	}
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		value, ok := params[key]
		if !ok {
			return fmt.Errorf("sms: tencent: template parameter %q is missing", key)
		}
		values = append(values, value)
	}
	body, err := json.Marshal(struct {
		PhoneNumberSet   []string `json:"PhoneNumberSet"`
		SmsSdkAppId      string   `json:"SmsSdkAppId"`
		SignName         string   `json:"SignName"`
		TemplateId       string   `json:"TemplateId"`
		TemplateParamSet []string `json:"TemplateParamSet"`
	}{[]string{phone}, t.appID, t.sign, template, values})
	if err != nil {
		return fmt.Errorf("sms: tencent: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint+"/", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("sms: tencent: create request: %w", err)
	}
	t.authorize(req, body, t.now())
	resp, err := t.hc.Do(req)
	if err != nil {
		return fmt.Errorf("sms: tencent request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("sms: tencent HTTP %d", resp.StatusCode)
	}
	var out struct {
		Response struct {
			RequestID     string `json:"RequestId"`
			Error         *struct{ Code, Message string }
			SendStatusSet []struct{ Code, Message string }
		}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<16)+1))
	if err != nil {
		return fmt.Errorf("sms: tencent: read response: %w", err)
	}
	if len(raw) > 1<<16 {
		return fmt.Errorf("sms: tencent: response exceeds 64 KiB")
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("sms: tencent: invalid JSON response: %w", err)
	}
	if e := out.Response.Error; e != nil {
		return fmt.Errorf("sms: tencent code=%s message=%s requestId=%s", e.Code, e.Message, out.Response.RequestID)
	}
	if len(out.Response.SendStatusSet) != 1 {
		return fmt.Errorf("sms: tencent: expected one send status, got %d requestId=%s",
			len(out.Response.SendStatusSet), out.Response.RequestID)
	}
	status := out.Response.SendStatusSet[0]
	if status.Code != "Ok" {
		return fmt.Errorf("sms: tencent code=%s message=%s requestId=%s", status.Code, status.Message, out.Response.RequestID)
	}
	return nil
}
