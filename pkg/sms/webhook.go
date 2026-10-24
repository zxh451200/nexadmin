package sms

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// webhook：把短信请求 POST 成 JSON 交给任意网关（国内短信平台、自建服务都能接）。
//
//	POST <url>
//	X-Timestamp: <unix 秒>
//	X-Nonce: <随机串>
//	X-Signature: sha256=<hex(HMAC-SHA256(secret, timestamp + "." + nonce + "." + body))>
//	{"phone":"13800138000","template":"login","params":{"code":"123456"},"timestamp":1700000000,"nonce":"..."}
//
// 网关返回任意 2xx 视为发送成功。接收方应校验签名并拒绝过期（建议 5 分钟）或重复 nonce 的请求。
type webhook struct {
	url, secret string
	hc          *http.Client
}

func newWebhook(c Config) (Provider, error) {
	u, err := url.Parse(c.WebhookURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("sms: webhook: sms.webhook.url must be an http(s) URL")
	}
	if c.WebhookSecret == "" {
		return nil, fmt.Errorf("sms: webhook: sms.webhook.secret is required")
	}
	return &webhook{url: c.WebhookURL, secret: c.WebhookSecret, hc: &http.Client{Timeout: c.Timeout}}, nil
}

// WebhookSign 计算 webhook 的签名头值（含 sha256= 前缀）；网关侧用同样的算法校验。
func WebhookSign(secret, timestamp, nonce string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(timestamp + "." + nonce + "."))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

type webhookBody struct {
	Phone     string            `json:"phone"`
	Template  string            `json:"template"`
	Params    map[string]string `json:"params"`
	Timestamp int64             `json:"timestamp"`
	Nonce     string            `json:"nonce"`
}

func (w *webhook) Send(ctx context.Context, phone, template string, params map[string]string) error {
	nb := make([]byte, 12)
	if _, err := rand.Read(nb); err != nil {
		return err
	}
	nonce, ts := hex.EncodeToString(nb), time.Now().Unix()
	body, err := json.Marshal(webhookBody{Phone: phone, Template: template, Params: params, Timestamp: ts, Nonce: nonce})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	tss := strconv.FormatInt(ts, 10)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timestamp", tss)
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("X-Signature", WebhookSign(w.secret, tss, nonce, body))
	resp, err := w.hc.Do(req)
	if err != nil {
		return fmt.Errorf("sms: webhook request failed: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("sms: webhook returned HTTP %d", resp.StatusCode)
	}
	return nil
}
