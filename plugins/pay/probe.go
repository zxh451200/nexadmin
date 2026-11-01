package pay

// 渠道凭据自检（对标竞品后台的「渠道连通性测试」）。
//
// 为什么是真功能而不是测试脚手架：凭据是运营在管理端手填的，填错一个字符
// （私钥换行丢了、公钥粘成应用公钥、APIv3 密钥少一位）此前只有等真客户付第一笔
// 款才暴露。自检只做只读动作——查一笔必然不存在的单、下载平台证书、本地签一次——
// 绝不下单、绝不退款、绝不代付：探针不该产生任何资金事实。
//
// 结论口径：拿不准一律算失败。「网络超时」和「签名被拒」混成一句「看起来正常」，
// 比报错了更难排。

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"nexadmin/core/response"
)

// probeNotExistMarker 是各家「查无此单」的原始码：命中它才说明请求被网关接受、
// 签名有效，只是这笔单不存在——这正是探针要的结论。
var probeNotExistMarker = []string{
	"NOT_EXIST", "RESOURCE_NOT_EXISTS", "不存在", "NOTFOUND", "NOT FOUND", "ORDERNOTEXIST",
}

// probeGatewayVerdict 把一次「查必然不存在的单」的结果折成一项自检结论。
//
// 渠道对「查无此单」有两种口径：报错误（微信 RESOURCE_NOT_EXISTS），或给一个空的
// 未定结论（支付宝 Query 刻意不报错，因为「还没有这笔单」是正常状态）。两种都说明
// 请求被网关接受、本次签名有效；反过来，探针单号竟然查出了真实结论才是大问题。
func probeGatewayVerdict(name string, st *ChannelStatus, err error) ProbeCheck {
	if err == nil {
		if st != nil && !st.Paid && !st.Failed && st.ChannelTxnID == "" && st.GatewayStatus == "" {
			return probePass(name, "网关可达且接受了本次签名（应答：查无此单）")
		}
		return probeFail(name, "探针单号竟然查出了真实结论，说明渠道查询语义未被正确实现")
	}
	msg := err.Error()
	up := strings.ToUpper(msg)
	var re *response.Error
	if errors.As(err, &re) {
		switch re.Key {
		case errChannelUnavailable:
			// 配置类错误（缺凭据、凭据不可解析、回调地址不是完整 URL）：就是自检要抓的错。
			return probeFail(name, "配置/凭据问题："+msg)
		case errChannelError:
			if strings.Contains(msg, "网络") || strings.Contains(up, "TLS") || strings.Contains(up, "CONTEXT DEADLINE") {
				return probeFail(name, "网关不可达："+msg)
			}
		}
	}
	for _, marker := range probeNotExistMarker {
		if strings.Contains(up, strings.ToUpper(marker)) {
			return probePass(name, "网关可达且接受了本次签名（应答：查无此单）")
		}
	}
	if strings.Contains(up, "SIGN") || strings.Contains(msg, "签名") {
		return probeFail(name, "签名被渠道拒绝（私钥/公钥或算法不对）："+msg)
	}
	return probeFail(name, "渠道返回未预期的应答："+msg)
}

// probeProbeNo 生成一个必然不存在的探针单号（探针不建单，号只需符合渠道格式）。
func probeProbeNo() string {
	return "PROBE" + time.Now().UTC().Format("20060102150405.000000000")
}

// ---- 支付宝 ----

func (p alipayProvider) Probe(ctx context.Context, cfg map[string]string) (*ProbeResult, error) {
	out := &ProbeResult{Provider: p.Name()}
	out.Checks = append(out.Checks, probeFailErr("credentials", requireProviderConfig(p, cfg)))
	if _, err := alipayPrivateKey(cfg["privateKey"]); err != nil {
		out.Checks = append(out.Checks, probeFailErr("privateKey", err))
	} else {
		out.Checks = append(out.Checks, probePass("privateKey", "应用私钥可解析"))
	}
	if pub, err := alipayPublicKey(cfg["alipayPublicKey"]); err != nil {
		out.Checks = append(out.Checks, probeFailErr("alipayPublicKey", err))
	} else if pub.N.BitLen() < 2048 {
		// 支付宝自 2015 年起只发 2048 位及以上密钥：更短的基本是把应用公钥或旧证书粘错了。
		out.Checks = append(out.Checks, probeFail("alipayPublicKey", "公钥长度不足 2048 位，疑似粘成应用公钥或旧证书"))
	} else {
		out.Checks = append(out.Checks, probePass("alipayPublicKey", "支付宝公钥可解析"))
	}
	// 「公钥/私钥是同一对吗」本地答不了：应用私钥签名、支付宝公钥验签，两把钥匙本就不同。
	// 唯一能证伪的是真打一次网关——签名不对渠道会直接拒，所以 gateway 项承担了这个角色。
	st, qerr := p.Query(ctx, probeProbeNo(), cfg)
	out.Checks = append(out.Checks, probeGatewayVerdict("gateway", st, qerr))
	return out, nil
}

// ---- 微信支付 ----

func (p wechatProvider) Probe(ctx context.Context, cfg map[string]string) (*ProbeResult, error) {
	out := &ProbeResult{Provider: p.Name()}
	out.Checks = append(out.Checks, probeFailErr("credentials", requireProviderConfig(p, cfg)))
	if _, err := parseRSAPrivateKey(cfg["privateKey"]); err != nil {
		out.Checks = append(out.Checks, probeFailErr("privateKey", err))
	} else {
		out.Checks = append(out.Checks, probePass("privateKey", "商户 API 私钥可解析"))
	}
	if len(strings.TrimSpace(cfg["apiV3Key"])) != 32 {
		out.Checks = append(out.Checks, probeFail("apiV3Key", "APIv3 密钥必须是 32 位（回调解密与平台证书下载都依赖它）"))
	} else {
		out.Checks = append(out.Checks, probePass("apiV3Key", "APIv3 密钥长度正确"))
	}
	// 平台证书下载：这一项同时验掉了「商户私钥签名对不对」「序列号填没填对」，
	// 微信只在签名正确时才下发证书。
	certs, err := p.downloadCerts(ctx, cfg)
	switch {
	case err != nil:
		out.Checks = append(out.Checks, probeFail("platformCerts", "平台证书下载失败："+err.Error()))
	case certs == nil || len(certs.certs) == 0:
		out.Checks = append(out.Checks, probeFail("platformCerts", "微信下发了一份证书都没有"))
	default:
		serials := make([]string, 0, len(certs.certs))
		for serial := range certs.certs {
			serials = append(serials, serial)
		}
		out.Checks = append(out.Checks, probePass("platformCerts", "取到 "+strconv.Itoa(len(serials))+" 份平台证书，序列号 "+strings.Join(serials, "/")))
	}
	st, qerr := p.Query(ctx, probeProbeNo(), cfg)
	out.Checks = append(out.Checks, probeGatewayVerdict("gateway", st, qerr))
	return out, nil
}

// ---- 易支付 ----

func (p epayProvider) Probe(ctx context.Context, cfg map[string]string) (*ProbeResult, error) {
	out := &ProbeResult{Provider: p.Name()}
	out.Checks = append(out.Checks, probeFailErr("credentials", requireProviderConfig(p, cfg)))
	// MD5 自检：签名函数是纯本地的，能算出稳定摘要说明密钥与参数拼装没坏。
	// 注意它证不了「密钥填对了」——那只有真报文才能证，所以不把 gateway 项省掉。
	sample := map[string]string{"pid": cfg["pid"], "out_trade_no": "PROBE", "total_amount": "0.01"}
	if strings.TrimSpace(cfg["key"]) == "" || epaySign(sample, cfg["key"]) == "" {
		out.Checks = append(out.Checks, probeFail("signing", "MD5 签名算不出来，检查商户密钥"))
	} else {
		out.Checks = append(out.Checks, probePass("signing", "MD5 签名可生成"))
	}
	// 易支付协议各家分支的查询接口不统一（有的没有 act=order_query），
	// 所以只在显式配了 queryUrl 时才探网关，否则如实报「没查」。
	queryURL := strings.TrimSpace(cfg["queryUrl"])
	if queryURL == "" {
		out.Checks = append(out.Checks, probeSkip("gateway", "未配置主动查询地址，无法验证凭据是否被上游接受（MD5 密钥填错只能等真回调发现）"))
		return out, nil
	}
	// 按易支付协议签名再查：密钥只参与摘要计算，绝不拼进 URL——查询串会被
	// 上游的 access log 与中间代理原样记下来，等于把商户密钥公开贴出去。
	params := map[string]string{
		"act": "order_query", "pid": cfg["pid"], "type": cfg["payType"],
		"out_trade_no": probeProbeNo(),
	}
	params["sign"] = epaySign(params, cfg["key"])
	params["sign_type"] = "MD5"
	q := url.Values{}
	for k, v := range params {
		if strings.TrimSpace(v) != "" {
			q.Set(k, v)
		}
	}
	res, err := epayGet(ctx, queryURL+"?"+q.Encode())
	if err != nil {
		out.Checks = append(out.Checks, probeGatewayVerdict("gateway", nil, err))
		return out, nil
	}
	if code := strings.TrimSpace(res["status"]); code == "" {
		out.Checks = append(out.Checks, probeFail("gateway", "查询应答里没有 status 字段：可能被拒签"))
		return out, nil
	}
	out.Checks = append(out.Checks, probePass("gateway", "上游应答可解析"))
	return out, nil
}

// ---- mock ----

func (p mockProvider) Probe(_ context.Context, cfg map[string]string) (*ProbeResult, error) {
	out := &ProbeResult{Provider: p.Name()}
	if strings.TrimSpace(cfg["secret"]) == "" {
		out.Checks = append(out.Checks, probeFail("credentials", "mock 渠道缺少签名密钥 secret"))
		return out, nil
	}
	out.Checks = append(out.Checks,
		probePass("credentials", "mock 渠道只需一个回调签名密钥"),
		probeSkip("gateway", "模拟渠道没有外部网关"),
	)
	return out, nil
}

// ---- 服务层 ----

// ChannelProbe 是对管理端返回的一次自检结果。
type ChannelProbe struct {
	ChannelCode string       `json:"channelCode"`
	Provider    string       `json:"provider"`
	Checks      []ProbeCheck `json:"checks"`
	Failed      int          `json:"failed"`
	Skipped     int          `json:"skipped"`
	CheckedAt   time.Time    `json:"checkedAt"`
}

// ProbeChannel 自检一个渠道实例的凭据与连通性。
//
// 返回的 err 只表示「这次自检没能跑完」（渠道不存在、类型未注册、该类型不支持自检）；
// 跑完了但有检查项失败，是通过返回值里的 Failed 计数表达的——否则「凭据错」和
// 「渠道不支持自检」在调用方看来会长得一样。
func (s *Service) ProbeChannel(ctx context.Context, code string) (*ChannelProbe, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, response.BadRequest(errChannelInvalid)
	}
	p, cfg, err := s.channelOf(ctx, code)
	if err != nil {
		return nil, err
	}
	pp, ok := p.(ProbeProvider)
	if !ok {
		return nil, capabilityUnsupported(p.Name(), "凭据自检")
	}
	res, err := pp.Probe(ctx, cfg)
	if err != nil {
		return nil, err
	}
	out := &ChannelProbe{ChannelCode: code, Provider: p.Name(), Checks: res.Checks, CheckedAt: time.Now().UTC()}
	for _, c := range res.Checks {
		switch c.State {
		case ProbeFail:
			out.Failed++
		case ProbeSkipped:
			out.Skipped++
		}
	}
	// 自检结果必须留一条日志：运营点完按钮说「没问题」，排障的人要能查到当时到底查了什么。
	if out.Failed > 0 {
		s.logWarn("pay: channel probe failed", "channel", code, "provider", p.Name(),
			"failed", out.Failed, "skipped", out.Skipped)
	} else {
		s.logInfo("pay: channel probe passed", "channel", code, "provider", p.Name(),
			"checks", len(out.Checks), "skipped", out.Skipped)
	}
	return out, nil
}
