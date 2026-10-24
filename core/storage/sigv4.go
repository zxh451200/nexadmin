package storage

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	sigAlgorithm    = "AWS4-HMAC-SHA256"
	amzTimeFormat   = "20060102T150405Z"
	amzDateFormat   = "20060102"
	unsignedPayload = "UNSIGNED-PAYLOAD"
	emptyPayload    = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

// Signer 实现 AWS Signature Version 4（Authorization 头与 presign 查询串两种）。
type Signer struct {
	AccessKey, SecretKey string
	Region, Service      string // Service 对 S3 是 "s3"
}

// 这些头不参与签名：它们可能被 Go 传输层或中间代理改写。
var skipSignHeaders = map[string]bool{
	"authorization": true, "user-agent": true, "content-length": true, "accept-encoding": true,
	"connection": true, "expect": true, "transfer-encoding": true, "x-amzn-trace-id": true,
}

// uriEncode 按 SigV4 规则编码：只有 A-Za-z0-9-_.~ 不编码；encodeSlash=false 时保留 /。
func uriEncode(s string, encodeSlash bool) string {
	const hexUpper = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		case c == '/' && !encodeSlash:
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hexUpper[c>>4])
			b.WriteByte(hexUpper[c&15])
		}
	}
	return b.String()
}

// canonicalQuery 把查询参数按编码后的键、值排序；空值写成 "k="。
func canonicalQuery(q url.Values) string {
	pairs := make([]string, 0, len(q))
	for k, vs := range q {
		ek := uriEncode(k, true)
		for _, v := range vs {
			pairs = append(pairs, ek+"="+uriEncode(v, true))
		}
	}
	sort.Strings(pairs)
	return strings.Join(pairs, "&")
}

// canonicalURI 对解码后的路径逐段编码一次（S3 不做二次编码）。
func canonicalURI(u *url.URL) string {
	if u.Path == "" {
		return "/"
	}
	return uriEncode(u.Path, false)
}

func requestHost(req *http.Request) string {
	if req.Host != "" {
		return req.Host
	}
	return req.URL.Host
}

// canonicalHeaders 返回规范化头与 SignedHeaders 列表（host + 请求上所有未排除的头）。
func canonicalHeaders(req *http.Request) (string, string) {
	vals := map[string]string{"host": requestHost(req)}
	for k, vs := range req.Header {
		lk := strings.ToLower(k)
		if skipSignHeaders[lk] {
			continue
		}
		parts := make([]string, len(vs))
		for i, v := range vs {
			parts[i] = strings.Join(strings.Fields(v), " ")
		}
		vals[lk] = strings.Join(parts, ",")
	}
	names := make([]string, 0, len(vals))
	for k := range vals {
		names = append(names, k)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, k := range names {
		b.WriteString(k + ":" + vals[k] + "\n")
	}
	return b.String(), strings.Join(names, ";")
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

func hashHex(data string) string {
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}

func (s Signer) scope(t time.Time) string {
	return t.UTC().Format(amzDateFormat) + "/" + s.Region + "/" + s.Service + "/aws4_request"
}

func (s Signer) signature(t time.Time, stringToSign string) string {
	k := hmacSHA256([]byte("AWS4"+s.SecretKey), t.UTC().Format(amzDateFormat))
	k = hmacSHA256(k, s.Region)
	k = hmacSHA256(k, s.Service)
	k = hmacSHA256(k, "aws4_request")
	return hex.EncodeToString(hmacSHA256(k, stringToSign))
}

// details 计算规范请求、待签字符串和签名。query 是已排好序的规范查询串。
func (s Signer) details(req *http.Request, query, payloadHash string, t time.Time) (canonicalReq, stringToSign, signature, signedHeaders string) {
	ch, signedHeaders := canonicalHeaders(req)
	canonicalReq = strings.Join([]string{req.Method, canonicalURI(req.URL), query, ch, signedHeaders, payloadHash}, "\n")
	stringToSign = strings.Join([]string{sigAlgorithm, t.UTC().Format(amzTimeFormat), s.scope(t), hashHex(canonicalReq)}, "\n")
	return canonicalReq, stringToSign, s.signature(t, stringToSign), signedHeaders
}

// SignRequest 给请求加上 x-amz-date、x-amz-content-sha256 和 Authorization 头。
func (s Signer) SignRequest(req *http.Request, payloadHash string, t time.Time) {
	if req.Header == nil {
		req.Header = http.Header{}
	}
	req.Header.Set("X-Amz-Date", t.UTC().Format(amzTimeFormat))
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	q, _ := url.ParseQuery(req.URL.RawQuery)
	_, _, sig, signed := s.details(req, canonicalQuery(q), payloadHash, t)
	req.Header.Set("Authorization", sigAlgorithm+" Credential="+s.AccessKey+"/"+s.scope(t)+
		",SignedHeaders="+signed+",Signature="+sig)
}

// Presign 返回带签名查询串的完整 URL（payload 固定为 UNSIGNED-PAYLOAD，只签 host 和 req 上已有的头）。
func (s Signer) Presign(req *http.Request, expires time.Duration, t time.Time) string {
	q, _ := url.ParseQuery(req.URL.RawQuery)
	_, signedHeaders := canonicalHeaders(req)
	q.Set("X-Amz-Algorithm", sigAlgorithm)
	q.Set("X-Amz-Credential", s.AccessKey+"/"+s.scope(t))
	q.Set("X-Amz-Date", t.UTC().Format(amzTimeFormat))
	q.Set("X-Amz-Expires", strconv.FormatInt(int64(expires/time.Second), 10))
	q.Set("X-Amz-SignedHeaders", signedHeaders)
	query := canonicalQuery(q)
	_, _, sig, _ := s.details(req, query, unsignedPayload, t)
	u := *req.URL
	u.RawQuery = query + "&X-Amz-Signature=" + sig
	u.RawPath = uriEncode(u.Path, false)
	return u.String()
}
