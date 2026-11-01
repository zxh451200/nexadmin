package pay

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nexadmin/core/response"
)

// 渠道自检替代的是「凭据填错，只能等客户付第一笔款才暴露」。所以每项结论的
// **分类**必须准：把签名被拒、把网络超时报成「通过」，比根本没有自检更糟。

func alipayProbeConfig(t *testing.T, gateway string) map[string]string {
	t.Helper()
	_, priv, pub := testRSA(t)
	return map[string]string{
		"appId": "2021000000000000", "privateKey": priv, "alipayPublicKey": pub, "gateway": gateway,
	}
}

// alipaySignedBody 按支付宝口径构造一次「签过名」的网关应答：签名是对应答节点
// 的原始 JSON 串签的（alipay.call 就是这么验的）。不签的桩只能测到「缺签名」，
// 测不到探针真正要区分的渠道语义。
func alipaySignedBody(t *testing.T, nodeKey, nodeJSON string) string {
	t.Helper()
	key, _, _ := testRSA(t)
	sig := rsaSignSHA256(key, nodeJSON)
	return `{"` + nodeKey + `":` + nodeJSON + `,"sign":"` + sig + `"}`
}

func TestAlipayProbeClassifiesGatewayAnswers(t *testing.T) {
	cases := []struct {
		name    string
		nodeKey string
		node    string
		// 网关级错误（签名不对、权限不足）走 error_response 节点，
		// 业务级应答才挂在方法名下——两条路径的文案完全不同，都要覆盖。
		wantFail bool
		wantWord string
	}{
		{
			name: "查无此单说明签名被接受", nodeKey: "alipay_trade_query_response",
			node:     `{"code":"40004","sub_code":"ACQ.TRADE_NOT_EXIST","sub_msg":"交易不存在"}`,
			wantWord: "查无此单",
		},
		{
			name: "无效签名要报成密钥不对", nodeKey: "error_response",
			node:     `{"code":"40002","sub_code":"isv.invalid-signature","sub_msg":"无效签名"}`,
			wantFail: true, wantWord: "签名",
		},
		{
			name: "未签约该产品要报成渠道拒绝，不许算通过", nodeKey: "error_response",
			node:     `{"code":"40004","sub_code":"ACQ.PERMISSION_NOT_MATCH","sub_msg":"没有权限"}`,
			wantFail: true, wantWord: "没有权限",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(alipaySignedBody(t, c.nodeKey, c.node)))
			}))
			defer srv.Close()
			res, err := alipayProvider{}.Probe(context.Background(), alipayProbeConfig(t, srv.URL))
			if err != nil {
				t.Fatal(err)
			}
			gw := probeItem(t, res, "gateway")
			if c.wantFail && gw.State != ProbeFail {
				t.Fatalf("want fail, got %s (%s)", gw.State, gw.Detail)
			}
			if !c.wantFail && gw.State != ProbePass {
				t.Fatalf("want pass, got %s (%s)", gw.State, gw.Detail)
			}
			if !strings.Contains(gw.Detail, c.wantWord) {
				t.Errorf("诊断里要能看到 %q，got detail=%q", c.wantWord, gw.Detail)
			}
		})
	}
}

func TestAlipayProbeReportsMissingCredentials(t *testing.T) {
	res, err := alipayProvider{}.Probe(context.Background(), map[string]string{"appId": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if item := probeItem(t, res, "credentials"); item.State != ProbeFail {
		t.Fatalf("缺凭据必须在 credentials 项失败，got %s", item.State)
	}
	if len(res.Failed()) == 0 {
		t.Fatal("Failed() 没抓到任何失败项，计数是空的")
	}
}

// 网关不可达绝不能粉饰成通过。
func TestAlipayProbeGatewayUnreachableFails(t *testing.T) {
	// 127.0.0.1:1 必然连不上；私钥用真密钥，保证失败原因落在网关而不是凭据。
	res, err := alipayProvider{}.Probe(context.Background(), alipayProbeConfig(t, "http://127.0.0.1:1"))
	if err != nil {
		t.Fatal(err)
	}
	if item := probeItem(t, res, "gateway"); item.State != ProbeFail {
		t.Fatalf("unreachable gateway must fail, got %s (%s)", item.State, item.Detail)
	}
	if item := probeItem(t, res, "privateKey"); item.State != ProbePass {
		t.Fatalf("私钥可解析这项不该被连坐，got %s", item.State)
	}
}

func TestWechatProbeChecksKeysAndCerts(t *testing.T) {
	_, priv, _ := testRSA(t)
	var hitCertPath bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/certificates") {
			hitCertPath = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"SIGN_ERROR","message":"签名校验失败"}`))
	}))
	defer srv.Close()
	cfg := map[string]string{
		"appId": "wx0000000000000000", "mchId": "1900000101", "privateKey": priv,
		"apiV3Key": strings.Repeat("k", 32), "serialNo": "SERIAL1", "gateway": srv.URL,
	}
	res, err := wechatProvider{}.Probe(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !hitCertPath {
		t.Fatal("探针没有真的去下载平台证书（用例空转）")
	}
	if item := probeItem(t, res, "apiV3Key"); item.State != ProbePass {
		t.Fatalf("32 位 apiV3Key 应通过，got %s", item.State)
	}
	if item := probeItem(t, res, "platformCerts"); item.State != ProbeFail {
		t.Fatalf("证书下载失败必须报 fail（它是「商户私钥签名对不对」的强证据），got %s", item.State)
	}

	short := map[string]string{"apiV3Key": "too-short"}
	for k, v := range cfg {
		if _, over := short[k]; !over {
			short[k] = v
		}
	}
	res2, err := wechatProvider{}.Probe(context.Background(), short)
	if err != nil {
		t.Fatal(err)
	}
	if item := probeItem(t, res2, "apiV3Key"); item.State != ProbeFail {
		t.Fatal("长度不对的 apiV3Key 必须报 fail")
	}
}

// 安全回归：易支付探针不许把商户密钥拼进 URL——查询串会被上游 access log 与
// 中间代理原样记下来，等于公开张贴密钥。
func TestEpayProbeNeverPutsMerchantKeyInURL(t *testing.T) {
	const secret = "SUPER-SECRET-KEY"
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"TRADE_NOT_EXIST"}`))
	}))
	defer srv.Close()
	cfg := map[string]string{
		"gateway": srv.URL, "pid": "1001", "key": secret,
		"payType": "alipay", "queryUrl": srv.URL + "/api.php",
	}
	res, err := epayProvider{}.Probe(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(gotQuery, secret) {
		t.Fatalf("商户密钥出现在查询串里：%s", gotQuery)
	}
	if !strings.Contains(gotQuery, "sign=") {
		t.Fatalf("按协议该带 sign，got %s", gotQuery)
	}
	if item := probeItem(t, res, "gateway"); item.State != ProbePass {
		t.Fatalf("上游应答可解析应通过，got %s (%s)", item.State, item.Detail)
	}
}

// 没配主动查询地址时必须是 skipped：这种情况下密钥填错确实发现不了，
// 报「通过」是自欺，报「失败」是冤枉。
func TestEpayProbeWithoutQueryUrlIsSkipped(t *testing.T) {
	cfg := map[string]string{"gateway": "https://pay.example.com", "pid": "1", "key": "k", "payType": "alipay"}
	res, err := epayProvider{}.Probe(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if item := probeItem(t, res, "gateway"); item.State != ProbeSkipped {
		t.Fatalf("want skipped, got %s", item.State)
	}
	if len(res.Failed()) != 0 {
		t.Fatalf("skipped 不该计入失败：%v", res.Failed())
	}
}

func TestMockProbeIsHonest(t *testing.T) {
	res, err := mockProvider{}.Probe(context.Background(), map[string]string{"secret": "k"})
	if err != nil {
		t.Fatal(err)
	}
	if item := probeItem(t, res, "gateway"); item.State != ProbeSkipped {
		t.Fatalf("mock 没有外部网关，gateway 必须 skipped，got %s", item.State)
	}
	res2, err := mockProvider{}.Probe(context.Background(), map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Failed()) == 0 {
		t.Fatal("缺 secret 必须报失败")
	}
}

// 服务层：渠道不支持自检时报契约错误码，而不是回一张空表让前端以为「全通过」。
func TestProbeChannelUnsupportedProvider(t *testing.T) {
	RegisterProvider(noProbeProvider{})
	db, m := mockDB(t)
	s := NewService(db, nopLog())
	m.ExpectQuery("SELECT \\* FROM `plg_pay_channel` WHERE code").
		WillReturnRows(channelRow("c-noprobe", noProbeProvider{}.Name(), `{"k":"v"}`))
	_, err := s.ProbeChannel(tenantCtx, "c-noprobe")
	var re *response.Error
	if !errors.As(err, &re) {
		t.Fatalf("want business error, got %T %v", err, err)
	}
	if re.Key != errCapabilityUnsupported {
		t.Fatalf("want %s, got %s", errCapabilityUnsupported, re.Key)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// noProbeProvider 只实现 Provider，故意不实现 ProbeProvider。
type noProbeProvider struct{}

func (noProbeProvider) Name() string { return "noprobe" }
func (noProbeProvider) Create(context.Context, PayRequest, map[string]string) (*PayParams, error) {
	return nil, errMockPending
}
func (noProbeProvider) Query(context.Context, string, map[string]string) (*ChannelStatus, error) {
	return &ChannelStatus{}, nil
}
func (noProbeProvider) Refund(context.Context, RefundRequest, map[string]string) (*RefundResult, error) {
	return &RefundResult{OK: true}, nil
}
func (noProbeProvider) VerifyCallback(CallbackInput, map[string]string) (*CallbackResult, error) {
	return nil, errMockPending
}

func probeItem(t *testing.T, res *ProbeResult, name string) ProbeCheck {
	t.Helper()
	for _, c := range res.Checks {
		if c.Name == name {
			return c
		}
	}
	var got []string
	for _, c := range res.Checks {
		got = append(got, c.Name+"="+c.State)
	}
	t.Fatalf("自检结果里没有 %q 项（实际：%s）", name, strings.Join(got, ", "))
	return ProbeCheck{}
}
