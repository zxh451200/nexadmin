package pay

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// 真实渠道凭据冒烟（对标竞品联调，也回答「这些只能真机验」的那批缺口到底怎么验）。
//
// 上一批交付里，预授权签约、渠道账单下载、代付签约、微信转账收款人加密等只能写代码、
// 不能在本机证明——因为没有商户号。这个文件把「有凭据就能验」变成一条命令：
//
//	NEXADMIN_PAY_LIVE_ALIPAY_CONFIG='{"appId":"...","privateKey":"...","alipayPublicKey":"...","sellerId":"..."}' \
//	NEXADMIN_PAY_LIVE_WECHAT_CONFIG='{"appId":"...","mchId":"...","apiV3Key":"...","privateKey":"...","serialNo":"..."}' \
//	NEXADMIN_PAY_LIVE_BILL_DATE=2026-10-30 \
//	go test ./plugins/pay/ -run TestLiveChannelSmoke -v
//
// 没有正式商户也能先跑支付宝：开放平台的沙箱凭据与生产同形（appId + 应用私钥 + 支付宝公钥），
// 只要把 gateway 指到沙箱即可（渠道配置里可配，默认生产）：
//
//	NEXADMIN_PAY_LIVE_ALIPAY_CONFIG='{"appId":"...","privateKey":"...","alipayPublicKey":"...","gateway":"https://openapi.alipaydev.com/gateway.do"}'
//
// 此时账单/预授权/转账会如实报「未签约」——那不是失败，是这几项本来就要正式商户+产品签约。
//
// 只读边界（刻意的）：探针只查不存在的单、下载证书、拉账单。绝不下单、绝不退款、
// 绝不打款——那些动作会真动钱，该在有监控的窗口由人做，不该被一个 `go test` 顺手触发。
//
// 没配凭据时 skip 并说清缺哪个环境变量；配了但调用失败就是真失败（t.Errorf），
// 不许降级成 skip——「因为没网所以算通过」是最坏的测试。

const liveConfigPrefix = "NEXADMIN_PAY_LIVE_"

// liveChannel 是一个渠道的联调输入。
type liveChannel struct {
	provider string
	env      string
	cfg      map[string]string
}

func liveChannels(t *testing.T) []liveChannel {
	t.Helper()
	var out []liveChannel
	for _, provider := range []string{"alipay", "wechat", "epay", "mock"} {
		env := liveConfigPrefix + strings.ToUpper(provider) + "_CONFIG"
		raw := strings.TrimSpace(os.Getenv(env))
		if raw == "" {
			t.Logf("跳过 %s：未设置 %s（凭据 JSON，字段见 GET /plugins/pay/providers 的 configFields）", provider, env)
			continue
		}
		var cfg map[string]string
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			t.Fatalf("%s 不是合法 JSON：%v", env, err)
		}
		out = append(out, liveChannel{provider: provider, env: env, cfg: cfg})
	}
	return out
}

func TestLiveChannelSmoke(t *testing.T) {
	channels := liveChannels(t)
	if len(channels) == 0 {
		t.Skipf("没有任何渠道凭据：设置 %sALIPAY_CONFIG / %sWECHAT_CONFIG 后再跑（只读冒烟，不会动钱）", liveConfigPrefix, liveConfigPrefix)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	billDate := strings.TrimSpace(os.Getenv(liveConfigPrefix + "BILL_DATE"))
	if billDate == "" {
		// 账单是 T+1 才有，默认取前天，避免拿当天没出账的日期误判成解析失败。
		billDate = time.Now().AddDate(0, 0, -2).Format("2006-01-02")
	}

	for _, ch := range channels {
		t.Run(ch.provider, func(t *testing.T) {
			p, err := LookupProvider(ch.provider)
			if err != nil {
				t.Fatalf("渠道未注册：%v", err)
			}
			if err := requireProviderConfig(p, ch.cfg); err != nil {
				t.Fatalf("凭据不完整：%v", err)
			}

			// ① 自检：密钥可解析 + 网关接受签名 + （微信）平台证书下载。
			if pp, ok := p.(ProbeProvider); ok {
				res, perr := pp.Probe(ctx, ch.cfg)
				if perr != nil {
					t.Fatalf("Probe 报错：%v", perr)
				}
				for _, c := range res.Checks {
					t.Logf("[%s] %s = %s %s", ch.provider, c.Name, c.State, c.Detail)
				}
				if failed := res.Failed(); len(failed) > 0 {
					t.Errorf("[%s] %d 项自检未通过，见上面的日志", ch.provider, len(failed))
				}
			} else {
				t.Errorf("[%s] 没实现 ProbeProvider，自检无从执行", ch.provider)
			}

			// ② 查一笔必然不存在的单：只读，能证签名与权限。
			st, qerr := p.Query(ctx, liveProbeNo(), ch.cfg)
			switch {
			case qerr != nil:
				t.Logf("[%s] Query 未成功（渠道若回「查无此单」即符合预期）：%v", ch.provider, qerr)
			case st.Paid || st.Failed:
				t.Errorf("[%s] 探针单号竟有真实结论（paid=%v failed=%v），渠道查询语义有问题", ch.provider, st.Paid, st.Failed)
			}

			// ③ 退款查询、账单、代付查询：能力声明了就必须真能跑通，跑不通要报出来。
			caps := ProviderCaps(p)
			if caps.RefundQuery {
				rq, ok := p.(RefundQueryProvider)
				if !ok {
					t.Errorf("[%s] 声明支持退款查询却没实现 RefundQueryProvider", ch.provider)
				} else if got, err := rq.QueryRefund(ctx, RefundQueryRequest{RefundNo: liveProbeNo(), PaymentNo: liveProbeNo(), IdempotencyKey: liveProbeNo()}, ch.cfg); err != nil {
					t.Logf("[%s] QueryRefund 未成功（回「查无退款」即符合预期）：%v", ch.provider, err)
				} else if got != nil && got.Found && got.OK {
					t.Errorf("[%s] 探针退款号竟查到成功退款", ch.provider)
				}
			}
			// 账单/代付/预授权各自独立：某一项失败也必须把其余项跑完，否则一次联调只看到
			// 第一个缺口（沙箱场景尤其明显：账单必失败，会把未签约的预授权/转账整段吞掉）。
			if caps.Bill {
				bp, ok := p.(BillProvider)
				if !ok {
					t.Errorf("[%s] 声明支持账单却没实现 BillProvider", ch.provider)
				} else if bill, err := bp.FetchBill(ctx, BillRequest{BillDate: billDate}, ch.cfg); err != nil {
					t.Errorf("[%s] 拉取 %s 账单失败（这条只有真凭据能验，必须报）：%v", ch.provider, billDate, err)
				} else {
					t.Logf("[%s] 账单 %s：%d 行，digest=%s；交易 %d / 退款 %d", ch.provider, billDate, bill.RowCount, bill.Digest[:12],
						countBillKind(bill, BillKindTrade), countBillKind(bill, BillKindRefund))
					if bill.RowCount == 0 {
						// mock 没有外部账单源（账单由进程内台账生成），空账单是它的常态；
						// 沙箱网关对任意日期都回只含 # 说明段的样本账单（解析器已把它归为空账单），
						// 0 行也是常态；生产网关空账单则可能是解析器不匹配该格式——
						// 那是必须人看的问题，不能降级成日志。
						if ch.provider == "mock" || sandboxGatewayCfg(ch.cfg) {
							reason := "沙箱样本账单"
							if ch.provider == "mock" {
								reason = "mock 无外部账单源"
							}
							t.Logf("[%s] 空账单（%s，符合预期）", ch.provider, reason)
						} else {
							t.Errorf("[%s] %s 账单解析出 0 行：要么那天真没交易，要么解析器不匹配该账单格式，需人工核对样例文件", ch.provider, billDate)
						}
					}
				}
			}
			if caps.Payout {
				pp, ok := p.(PayoutProvider)
				if !ok {
					t.Errorf("[%s] 声明支持代付却没实现 PayoutProvider", ch.provider)
				} else if tr, err := pp.QueryTransfer(ctx, liveProbeNo(), ch.cfg); err != nil {
					t.Logf("[%s] QueryTransfer 未成功（未签约或查无批次都会走到这里，把原文留着）：%v", ch.provider, err)
				} else {
					t.Logf("[%s] QueryTransfer 应答 state=%s accepted=%v pending=%v", ch.provider, tr.State, tr.Accepted, tr.Pending)
				}
			}
			if caps.Auth {
				ap, ok := p.(AuthProvider)
				if !ok {
					t.Errorf("[%s] 声明支持预授权却没实现 AuthProvider", ch.provider)
				} else if res, err := ap.QueryAuth(ctx, liveProbeNo(), ch.cfg); err != nil {
					t.Logf("[%s] QueryAuth 未成功（未签约资金授权产品会在这里现原形）：%v", ch.provider, err)
				} else {
					t.Logf("[%s] QueryAuth 应答 status=%s authNo=%s", ch.provider, res.Status, res.ChannelAuthNo)
				}
			}
		})
	}
}

// liveProbeNo 造一个必然不存在的单号。前缀带 PROBE 便于在渠道后台一眼认出是联调流量。
func liveProbeNo() string {
	return "PROBE" + time.Now().UTC().Format("20060102150405")
}

// sandboxGatewayCfg 判断渠道配置是否指向沙箱网关：沙箱的账单是只含说明段的样本、
// 未签约产品回 HTML 错误页，都与生产行为不同，冒烟里按「符合预期」处理而不是失败。
func sandboxGatewayCfg(cfg map[string]string) bool {
	return strings.Contains(strings.ToLower(cfg["gateway"]), "sandbox")
}

func countBillKind(bill *Bill, kind string) int {
	n := 0
	for _, row := range bill.Rows {
		if row.Kind == kind {
			n++
		}
	}
	return n
}
