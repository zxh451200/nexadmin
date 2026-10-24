// Package sms 是短信发送能力（不是模块）：log（开发用）、webhook（POST 到任意网关，带 HMAC 签名）、
// aliyun（Dysmsapi RPC 签名 v1）、tencent（TC3-HMAC-SHA256），签名均使用标准库。上层只依赖 Provider 接口。
package sms

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"
)

// Provider 发送一条模板短信。template 是模板标识（云厂商模板 ID，或 webhook 网关自定义的模板名），
// params 是模板变量，例如 {"code": "123456"}。
type Provider interface {
	Send(ctx context.Context, phone, template string, params map[string]string) error
}

// Config 是 sms.* 配置（已展开 ${ENV}）。
type Config struct {
	Provider string // log | webhook | aliyun | tencent；空 = 未启用
	Proxy    string // auth.proxy 同款，国内网关一般不需要
	Timeout  time.Duration

	WebhookURL    string
	WebhookSecret string

	AliyunKeyID     string
	AliyunKeySecret string
	AliyunSignName  string
	AliyunEndpoint  string // 默认 https://dysmsapi.aliyuncs.com

	TencentSecretID   string
	TencentSecretKey  string
	TencentSDKAppID   string
	TencentSignName   string
	TencentRegion     string              // 默认 ap-guangzhou
	TencentEndpoint   string              // 默认 https://sms.tencentcloudapi.com
	TencentParamOrder map[string][]string // 模板 ID -> 参数名顺序；未指定时按参数名排序

	Log *zap.Logger
}

// New 按配置创建 Provider。Provider 为空返回 (nil, nil)，表示未启用短信。
func New(c Config) (Provider, error) {
	if c.Timeout <= 0 {
		c.Timeout = 10 * time.Second
	}
	switch strings.ToLower(strings.TrimSpace(c.Provider)) {
	case "":
		return nil, nil
	case "log":
		return &logProvider{log: c.Log}, nil
	case "webhook":
		return newWebhook(c)
	case "aliyun":
		return newAliyun(c)
	case "tencent":
		return newTencent(c)
	}
	return nil, fmt.Errorf("sms: unknown provider %q (want log, webhook, aliyun or tencent)", c.Provider)
}

// logProvider 只把短信内容打到日志里，仅供开发。
type logProvider struct{ log *zap.Logger }

func (l *logProvider) Send(_ context.Context, phone, template string, params map[string]string) error {
	if l.log == nil {
		return nil
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fields := []zap.Field{zap.String("phone", MaskPhone(phone)), zap.String("template", template)}
	for _, k := range keys {
		fields = append(fields, zap.String("param."+k, params[k]))
	}
	l.log.Warn("sms(log provider): message NOT sent, development only", fields...)
	return nil
}

// MaskPhone 138****8000。
func MaskPhone(p string) string {
	if len(p) == 11 {
		return p[:3] + "****" + p[7:]
	}
	return p
}
