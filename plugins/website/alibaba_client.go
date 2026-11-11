package website

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// TOP（淘宝开放平台）协议客户端：阿里国际站的 API 都走这个网关。
// 协议要点（developer.alibaba.com「API调用方法详解」treeId=456&articleId=101617）：
//   - 公共参数 app_key / method / timestamp(yyyy-MM-dd HH:mm:ss, GMT+8) / v=2.0 /
//     sign_method=hmac-sha256 / format=json / session(需授权的接口必传)；
//   - 签名：除 sign 与 byte[] 外的全部参数按参数名 ASCII 升序，名字与值直接拼接
//     （无分隔符），HMAC-SHA256(secret, 拼接串) 转大写十六进制；
//   - 响应 JSON 形如 {"<method 下划线>_response": {...}} 或 {"error_response": {...}}。
//
// 网关地址固定来自站点配置（默认正式环境），不是用户输入，无需 SSRF 校验；超时硬编码。

const (
	// AliGatewayDefault 是 TOP 正式环境网关。
	AliGatewayDefault = "https://gw.api.taobao.com/router/rest"
	// aliTimeout 是单次 API 调用的超时：图片上传受网络影响更大，取宽值。
	aliTimeout = 60 * time.Second
	// aliMaxBody 是读取响应体的上限：TOP 的 JSON 响应都在 KB 量级，10MB 是防呆。
	aliMaxBody = 10 << 20
)

// AliCredentials 是解密后的凭据（明文只存在于内存，不得写日志）。
type AliCredentials struct {
	AppKey     string
	AppSecret  string
	Session    string
	Gateway    string
	HTTPClient *http.Client
}

// aliAPIError 是网关返回的 error_response。
type aliAPIError struct {
	Code     any    `json:"code"` // TOP 的 code 可能是数字或字符串，统一转文本
	Msg      string `json:"msg"`
	SubCode  string `json:"sub_code"`
	SubMsg   string `json:"sub_msg"`
	RequestID string `json:"request_id"`
}

func (e *aliAPIError) Error() string {
	code := fmt.Sprint(e.Code)
	switch {
	case e.SubMsg != "":
		return code + " " + e.SubMsg
	case e.Msg != "":
		return code + " " + e.Msg
	default:
		return code
	}
}

// aliCall 调一个 TOP 接口：返回去掉「<method 下划线>_response」包裹层后的业务 JSON 对象。
// params 是业务参数（全部按 form 编码提交；TOP 要求 URL 过长或含 byte[] 时用 POST，
// 我们统一 POST form，业务参数天然不会出现在 URL 上）。
func aliCall(ctx context.Context, c AliCredentials, method string, params map[string]string) (map[string]json.RawMessage, error) {
	fields := make(map[string]string, len(params)+6)
	for k, v := range params {
		fields[k] = v
	}
	fields["method"] = method
	fields["app_key"] = c.AppKey
	fields["timestamp"] = aliTimestamp()
	fields["v"] = "2.0"
	fields["format"] = "json"
	fields["sign_method"] = "hmac-sha256"
	if c.Session != "" {
		fields["session"] = c.Session
	}
	fields["sign"] = aliSign(fields, c.AppSecret)
	return aliPost(ctx, c, method, fields, nil)
}

// aliUploadImage 调 alibaba.icbu.photobank.upload：multipart 上传一张图到图片银行，
// 返回（file_id, 阿里 CDN URL）。文件字段 image_bytes 不参与签名（byte[] 类型）。
func aliUploadImage(ctx context.Context, c AliCredentials, name string, data []byte) (int64, string, error) {
	fields := map[string]string{
		"file_name": name,
	}
	fields["method"] = "alibaba.icbu.photobank.upload"
	fields["app_key"] = c.AppKey
	fields["timestamp"] = aliTimestamp()
	fields["v"] = "2.0"
	fields["format"] = "json"
	fields["sign_method"] = "hmac-sha256"
	if c.Session != "" {
		fields["session"] = c.Session
	}
	fields["sign"] = aliSign(fields, c.AppSecret)

	files := []aliFile{{Field: "image_bytes", Name: name, Data: data}}
	res, err := aliPost(ctx, c, "alibaba.icbu.photobank.upload", fields, files)
	if err != nil {
		return 0, "", err
	}
	// 响应节点 upload_image_response：{file_name, photobank_url, file_id}
	var inner struct {
		FileName     string `json:"file_name"`
		PhotobankURL string `json:"photobank_url"`
		FileID       int64  `json:"file_id"`
	}
	if err := aliUnmarshalNode(res, "upload_image_response", &inner); err != nil {
		return 0, "", err
	}
	if inner.FileID == 0 {
		return 0, "", fmt.Errorf("photobank.upload: missing file_id in response")
	}
	return inner.FileID, inner.PhotobankURL, nil
}

// aliFile 是 multipart 上传的一个文件字段。
type aliFile struct {
	Field string
	Name  string
	Data  []byte
}

// aliPost 发出请求并解析响应。text 为表单字段（参与签名），files 为文件字段（不参与签名）。
func aliPost(ctx context.Context, c AliCredentials, method string, text map[string]string, files []aliFile) (map[string]json.RawMessage, error) {
	gw := strings.TrimSpace(c.Gateway)
	if gw == "" {
		gw = AliGatewayDefault
	}
	if !strings.HasPrefix(gw, "https://") {
		return nil, fmt.Errorf("ali gateway must be https: %s", gw)
	}

	var body io.Reader
	contentType := ""
	if len(files) == 0 {
		form := url.Values{}
		for k, v := range text {
			form.Set(k, v)
		}
		body = strings.NewReader(form.Encode())
		contentType = "application/x-www-form-urlencoded; charset=utf-8"
	} else {
		// multipart：TOP 要求 charset=utf-8；文件字段值不参与 URL 编码，也不参与签名。
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		for _, k := range sortedKeys(text) {
			_ = w.WriteField(k, text[k])
		}
		for _, f := range files {
			part, err := w.CreateFormFile(f.Field, f.Name)
			if err != nil {
				return nil, err
			}
			if _, err := part.Write(f.Data); err != nil {
				return nil, err
			}
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		body = &buf
		contentType = w.FormDataContentType()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gw, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	httpc := c.HTTPClient
	if httpc == nil {
		httpc = &http.Client{Timeout: aliTimeout}
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ali gateway unreachable: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, aliMaxBody))
	if err != nil {
		return nil, err
	}
	return aliParse(method, raw)
}

// aliParse 解析 TOP JSON 响应：优先 error_response（转 aliAPIError），
// 否则取「<method 下划线>_response」包裹层返回。
func aliParse(method string, raw []byte) (map[string]json.RawMessage, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("ali response not json: %s", aliSnippet(raw))
	}
	if e, ok := envelope["error_response"]; ok {
		var apiErr aliAPIError
		if jerr := json.Unmarshal(e, &apiErr); jerr == nil {
			return nil, &apiErr
		}
		return nil, fmt.Errorf("ali error: %s", aliSnippet(e))
	}
	inner, ok := envelope[methodToNode(method)]
	if !ok {
		return nil, fmt.Errorf("ali response missing %s: %s", methodToNode(method), aliSnippet(raw))
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(inner, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// aliUnmarshalNode 从业务响应对象里取 name 子节点并解到 v。
func aliUnmarshalNode(res map[string]json.RawMessage, name string, v any) error {
	raw, ok := res[name]
	if !ok {
		return fmt.Errorf("ali response missing %s", name)
	}
	return json.Unmarshal(raw, v)
}

// aliSign 计算 TOP 签名：除 sign 与文件字段外，按参数名 ASCII 升序把名字与值直接拼接，
// HMAC-SHA256(secret, 拼接串) 转大写十六进制。
func aliSign(params map[string]string, secret string) string {
	keys := sortedKeys(params)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString(params[k])
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(b.String()))
	return strings.ToUpper(hex.EncodeToString(mac.Sum(nil)))
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// aliTimestamp 返回 TOP 要求的北京时间格式 yyyy-MM-dd HH:mm:ss（误差上限 10 分钟）。
func aliTimestamp() string {
	cst := time.FixedZone("CST", 8*3600)
	return time.Now().In(cst).Format("2006-01-02 15:04:05")
}

// methodToNode 把 API 名转响应包裹层节点名：alibaba.icbu.product.add → alibaba_icbu_product_add_response。
func methodToNode(method string) string {
	return strings.ReplaceAll(method, ".", "_") + "_response"
}

// aliSnippet 截响应片段进错误信息：全量响应太长且可能含业务数据，只留排错够用的头。
func aliSnippet(raw []byte) string {
	s := string(raw)
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}
