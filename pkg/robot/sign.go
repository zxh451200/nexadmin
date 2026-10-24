package robot

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// apiError 是机器人接口返回的业务错误（HTTP 200 但 errcode/code 非 0）。
type apiError struct {
	code int
	msg  string
}

func (e *apiError) Error() string { return fmt.Sprintf("robot: api error %d: %s", e.code, e.msg) }

// dingtalkSign 钉钉加签：HMAC-SHA256(key=secret, msg=timestamp+"\n"+secret)，Base64（URL 编码由调用方做）。timestamp 为毫秒。
func dingtalkSign(timestampMs int64, secret string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(strconv.FormatInt(timestampMs, 10) + "\n" + secret))
	return base64.StdEncoding.EncodeToString(m.Sum(nil))
}

// feishuSign 飞书签名：HMAC-SHA256(key=timestamp+"\n"+secret, msg=空)，Base64。timestamp 为秒。
func feishuSign(timestampSec int64, secret string) string {
	m := hmac.New(sha256.New, []byte(strconv.FormatInt(timestampSec, 10)+"\n"+secret))
	return base64.StdEncoding.EncodeToString(m.Sum(nil))
}

// build 生成请求地址与 JSON 报文。每次调用都重新取时间戳，所以重试也是新签名。
func (c *Client) build(title, content string) (string, []byte, error) {
	var payload map[string]any
	target := c.cfg.URL
	now := c.cfg.now()
	switch c.cfg.Type {
	case DingTalk:
		if c.cfg.Markdown {
			text := content
			if title != "" {
				text = "### " + title + "\n\n" + content
			}
			if title == "" {
				title = "notice"
			}
			payload = map[string]any{"msgtype": "markdown", "markdown": map[string]any{"title": title, "text": text}}
		} else {
			payload = map[string]any{"msgtype": "text", "text": map[string]any{"content": joinTitle(title, content)}}
		}
		if c.cfg.Secret != "" {
			u, err := url.Parse(c.cfg.URL)
			if err != nil {
				return "", nil, err
			}
			ts := now.UnixMilli()
			q := u.Query()
			q.Set("timestamp", strconv.FormatInt(ts, 10))
			q.Set("sign", dingtalkSign(ts, c.cfg.Secret))
			u.RawQuery = q.Encode()
			target = u.String()
		}
	case Feishu:
		if c.cfg.Markdown {
			card := map[string]any{"elements": []any{map[string]any{"tag": "markdown", "content": content}}}
			if title != "" {
				card["header"] = map[string]any{"title": map[string]any{"tag": "plain_text", "content": title}}
			}
			payload = map[string]any{"msg_type": "interactive", "card": card}
		} else {
			payload = map[string]any{"msg_type": "text", "content": map[string]any{"text": joinTitle(title, content)}}
		}
		if c.cfg.Secret != "" {
			ts := now.Unix()
			payload["timestamp"] = strconv.FormatInt(ts, 10)
			payload["sign"] = feishuSign(ts, c.cfg.Secret)
		}
	case WeCom:
		if c.cfg.Markdown {
			text := content
			if title != "" {
				text = "**" + title + "**\n" + content
			}
			payload = map[string]any{"msgtype": "markdown", "markdown": map[string]any{"content": text}}
		} else {
			payload = map[string]any{"msgtype": "text", "text": map[string]any{"content": joinTitle(title, content)}}
		}
	}
	b, err := json.Marshal(payload)
	return target, b, err
}

func joinTitle(title, content string) string {
	if title == "" {
		return content
	}
	return title + "\n" + content
}

// checkReply 解析各家的业务返回码：钉钉 / 企业微信 errcode，飞书 code（旧版 StatusCode）。
func (c *Client) checkReply(raw []byte) error {
	var r struct {
		ErrCode    *int   `json:"errcode"`
		ErrMsg     string `json:"errmsg"`
		Code       *int   `json:"code"`
		Msg        string `json:"msg"`
		StatusCode *int   `json:"StatusCode"`
		StatusMsg  string `json:"StatusMessage"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return &apiError{code: -1, msg: "unparseable response"}
	}
	switch {
	case r.ErrCode != nil && *r.ErrCode != 0:
		return &apiError{code: *r.ErrCode, msg: r.ErrMsg}
	case r.Code != nil && *r.Code != 0:
		return &apiError{code: *r.Code, msg: r.Msg}
	case r.StatusCode != nil && *r.StatusCode != 0:
		return &apiError{code: *r.StatusCode, msg: r.StatusMsg}
	case r.ErrCode == nil && r.Code == nil && r.StatusCode == nil:
		return &apiError{code: -1, msg: "missing result code"}
	}
	return nil
}
