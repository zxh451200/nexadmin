package pay

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"nexadmin/core/response"
)

// ConfigField 描述渠道凭据的一个字段：前端据此后渲染表单，不再让商户手写 JSON。
// Key 同时是 config JSON 的键；Type 取 text|password|url|select。
type ConfigField struct {
	Key         string   `json:"key"`
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Secret      bool     `json:"secret"`
	Options     []string `json:"options,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
}

// ProviderInfo 是「新建渠道」下拉的一项：渠道类型 + 它的凭据字段。
type ProviderInfo struct {
	Name   string        `json:"name"`
	Fields []ConfigField `json:"fields"`
}

// SchemaProvider 由渠道实现声明自己的凭据字段；未声明的实现退回 JSON 输入。
type SchemaProvider interface {
	ConfigSchema() []ConfigField
}

// AckProvider 声明渠道要求的回调应答体。支付宝/易支付必须回纯文本 success，
// 微信要 {"code":"SUCCESS"}；未声明时沿用统一 JSON。
type AckProvider interface {
	CallbackAck() (contentType, body string)
}

// ProviderInfos 返回已注册渠道类型（按名称排序，下拉稳定）。
func ProviderInfos() []ProviderInfo {
	providerMu.RLock()
	defer providerMu.RUnlock()
	names := make([]string, 0, len(providers))
	for n := range providers {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]ProviderInfo, 0, len(names))
	for _, n := range names {
		info := ProviderInfo{Name: n, Fields: []ConfigField{}}
		if sp, ok := providers[n].(SchemaProvider); ok {
			if f := sp.ConfigSchema(); f != nil {
				info.Fields = f
			}
		}
		out = append(out, info)
	}
	return out
}

// gatewayClient 是渠道 HTTP 调用的统一客户端；真实渠道都有超时，不能让下单请求挂死。
var gatewayClient = &http.Client{Timeout: 15 * time.Second}

// channelGatewayError 是渠道侧拒绝/网络失败：主文案走 i18n，细节放 reason。
func channelGatewayError(provider, detail string) error {
	return response.New(http.StatusBadGateway, errChannelError).WithMsg(provider + ": " + detail)
}

// channelConfigError 是渠道凭据缺失/非法：属于配置问题，返回 400 提示去补配置。
func channelConfigError(provider, detail string) error {
	return response.BadRequest(errChannelUnavailable).WithMsg(provider + ": " + detail)
}

// requireProviderConfig 校验渠道实现声明的必填凭据。
func requireProviderConfig(p Provider, cfg map[string]string) error {
	sp, ok := p.(SchemaProvider)
	if !ok {
		return nil
	}
	missing := make([]string, 0, 4)
	for _, f := range sp.ConfigSchema() {
		if f.Required && strings.TrimSpace(cfg[f.Key]) == "" {
			missing = append(missing, f.Key)
		}
	}
	if len(missing) > 0 {
		return channelConfigError(p.Name(), "缺少必填凭据 "+strings.Join(missing, "、"))
	}
	return nil
}

// absoluteNotifyURL 取渠道异步通知的完整地址。相对路径只有 mock 收银台能用，
// 真实网关无法回调相对地址，必须在渠道编辑里填完整 URL。
func absoluteNotifyURL(provider, v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", channelConfigError(provider, "未配置回调地址（渠道编辑里填 https://你的域名/api/v1/plugins/pay/notify/<渠道编码>）")
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", channelConfigError(provider, "回调地址必须是完整 URL（形如 https://你的域名/api/v1/plugins/pay/notify/<渠道编码>）")
	}
	return v, nil
}

// cnyOnly 真实渠道只收人民币：外币单要在业务侧换汇后再下单。
func cnyOnly(provider, currency string) error {
	c := strings.ToUpper(strings.TrimSpace(currency))
	if c == "" || c == "CNY" {
		return nil
	}
	return channelConfigError(provider, "仅支持 CNY，当前币种 "+c)
}

// yuanOf 把最小货币单位的整数金额格式化成两位小数（绝不经过 float）。
func yuanOf(minor int64) string {
	sign := ""
	if minor < 0 {
		sign, minor = "-", -minor
	}
	return fmt.Sprintf("%s%d.%02d", sign, minor/100, minor%100)
}

// minorFromYuan 解析 "12.34" 这样的金额为分；非法返回 false。
func minorFromYuan(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	neg := false
	if s[0] == '-' {
		neg, s = true, s[1:]
	}
	whole, frac, ok := strings.Cut(s, ".")
	if whole == "" {
		whole = "0"
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, false
	}
	f := int64(0)
	if ok {
		if frac == "" {
			frac = "0"
		}
		if len(frac) > 2 {
			return 0, false
		}
		for len(frac) < 2 {
			frac += "0"
		}
		f, err = strconv.ParseInt(frac, 10, 64)
		if err != nil {
			return 0, false
		}
	}
	v := w*100 + f
	if neg {
		v = -v
	}
	return v, true
}

// newRequestID 生成渠道调用需要的随机串（hex，无连字符）。
func newRequestID(n int) string { return randomToken(n) }
