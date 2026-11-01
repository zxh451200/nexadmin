package pay

// 账单编码：支付宝的对账单是 GBK/GB18030。按 UTF-8 读表头永远找不到列名——
// 真凭据联调时才暴露的那类缺陷（沙箱实测：解出来的是「#支付宝业务汇总查询」）。
// 这两条用例同时钉住两个方向：GBK 必须能解、已经 UTF-8 的不许被二次转码搞坏。

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

const alipayBillSampleCSV = "商户订单号,支付宝交易号,业务名称,金额,服务费,退款金额\r\n" +
	"PAY-GBK-1,2026110122001400000001,交易,10.00,0.06,\r\n"

func TestAlipayBillParsesGBK(t *testing.T) {
	raw, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(alipayBillSampleCSV))
	if err != nil {
		t.Fatalf("编码成 GBK 失败：%v", err)
	}
	if string(raw) == alipayBillSampleCSV {
		t.Fatal("夹具仍是 UTF-8：这条用例没测到 GBK 路径")
	}
	rows, err := parseAlipayBill(raw)
	if err != nil {
		t.Fatalf("GBK 账单解析失败：%v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("流水行 = %d, want 1", len(rows))
	}
	if rows[0].PaymentNo != "PAY-GBK-1" || rows[0].AmountMinor != 1000 || rows[0].ChannelTxnID != "2026110122001400000001" {
		t.Fatalf("解析结果不对：%+v", rows[0])
	}
}

func TestAlipayBillParsesUTF8(t *testing.T) {
	rows, err := parseAlipayBill([]byte(alipayBillSampleCSV))
	if err != nil {
		t.Fatalf("UTF-8 账单解析失败：%v", err)
	}
	if len(rows) != 1 || rows[0].PaymentNo != "PAY-GBK-1" {
		t.Fatalf("UTF-8 路径被破坏：%+v", rows)
	}
}

// 沙箱实测（2026-11-01，APPID 9021000170680762）：downloadurl.query 给的链接是 ZIP，
// 内含两份 CSV——
//   - 业务明细.csv：#支付宝业务明细查询 说明段 + 完整明细表头 + 0 行流水（「交易合计：0笔」），
//     这就是支付宝「当日无交易」的合法空账单形状；
//   - 业务明细(汇总).csv：#支付宝业务汇总查询 说明段 + 门店汇总表头 + 合计行，
//     不是流水表，流进明细解析器只会报「找不到表头行」。

const alipayBillSandboxStub = "#支付宝业务汇总查询\n" +
	"#账号:[20887211182849430156]\n" +
	"#起始日期:[2016年07月11日 00:00:00]   终止日期:[2016年07月12日 00:00:00]\n" +
	"#---\n"

// alipayBillSandboxDetailCSV 按沙箱真实明细文件摘录：说明段 + 表头 + 无流水行。
const alipayBillSandboxDetailCSV = "#支付宝业务明细查询\n" +
	"#账号：[20887211182849430156]\n" +
	"#起始日期：[2016年07月11日 00:00:00]   终止日期：[2016年07月12日 00:00:00]\n" +
	"#-----------------------------------------业务明细列表----------------------------------------\n" +
	"支付宝交易号,商户订单号,业务类型,商品名称,创建时间,完成时间,门店编号,门店名称,操作员,终端号,对方账户,订单金额（元）,商家实收（元）,支付宝红包（元）,集分宝（元）,支付宝优惠（元）,商家优惠（元）,券核销金额（元）,券名称,商家红包消费金额（元）,卡消费金额（元）,退款批次号/请求号,服务费（元）,分润（元）,备注\n" +
	"#-----------------------------------------业务明细列表结束------------------------------------\n" +
	"#交易合计：0笔，商家实收共0.00元，商家优惠共0.00元\n" +
	"#退款合计：0笔，商家实收退款共0.00元，商家优惠退款共0.00元\n"

// alipayBillSandboxSummaryCSV 按沙箱真实汇总文件摘录：另一张表，带合计数据行。
const alipayBillSandboxSummaryCSV = "#支付宝业务汇总查询\n" +
	"#账号：[20887211182849430156]\n" +
	"#----------------------------------------业务汇总列表---------------------------------------\n" +
	"门店编号,门店名称,交易订单总笔数,退款订单总笔数,订单金额（元）,商家实收（元）,支付宝优惠（元）,商家优惠（元）,卡消费金额（元）,服务费（元）,分润（元）,实收净额（元）\n" +
	"合计   ,   ,0  ,0  ,0.00   ,0.00   ,0.00   ,0.00   ,0.00   ,0.00   ,0.00   ,0.00   \n" +
	"#----------------------------------------业务汇总列表结束-------------------------------------\n"

func TestAlipayBillSummaryOnlyStubIsEmptyBill(t *testing.T) {
	gbk, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(alipayBillSandboxStub))
	if err != nil {
		t.Fatalf("编码成 GBK 失败：%v", err)
	}
	rows, err := parseAlipayBill(gbk)
	if err != nil {
		t.Fatalf("只含说明段的样本账单应归为空账单，却报了错：%v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("样本账单应解析出 0 行，实际 %d 行：%+v", len(rows), rows)
	}
	// UTF-8 同形状（新网关可能不再转码）也走同一条路。
	rows, err = parseAlipayBill([]byte(alipayBillSandboxStub))
	if err != nil || len(rows) != 0 {
		t.Fatalf("UTF-8 样本账单应归为空账单：%v, %d 行", err, len(rows))
	}
}

// TestAlipayBillSandboxZipIsEmptyBill 沙箱真实形状回归：明细（表头+0行）+ 汇总（另一张表）
// 双文件 ZIP，必须解析成 0 行空账单而不是报错。
func TestAlipayBillSandboxZipIsEmptyBill(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range []struct{ name, data string }{
		{"20887211182849430156_20261030_业务明细.csv", alipayBillSandboxDetailCSV},
		{"20887211182849430156_20261030_业务明细(汇总).csv", alipayBillSandboxSummaryCSV},
	} {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		gbk, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(e.data))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(gbk); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	rows, err := parseAlipayBill(buf.Bytes())
	if err != nil {
		t.Fatalf("沙箱形状账单应归为空账单，却报了错：%v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("应解析出 0 行，实际 %d 行：%+v", len(rows), rows)
	}
	// 只有汇总文件（无明细）同样不报错——它本来就不是流水表。
	var onlySummary bytes.Buffer
	zw2 := zip.NewWriter(&onlySummary)
	w, err := zw2.Create("summary.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(alipayBillSandboxSummaryCSV)); err != nil {
		t.Fatal(err)
	}
	if err := zw2.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := parseAlipayBill(onlySummary.Bytes()); err != nil {
		t.Fatalf("只含汇总表的账单不应报错：%v", err)
	}
}

func TestAlipayBillSummaryOnlyGuardrails(t *testing.T) {
	// 纯注释但没点名「支付宝业务」：可能是任何东西，必须报错，不能静默当空账单。
	if _, err := parseAlipayBill([]byte("#某段注释\n#另一段注释\n")); err == nil {
		t.Fatal("无「支付宝业务」字样的纯注释文件被当成空账单")
	}
	// 明细文件带了官方标记但表头认不出（格式漂移）：必须报错，不能静默当空账单。
	drifted := strings.Replace(alipayBillSandboxDetailCSV, "支付宝交易号,商户订单号", "流水编号,订单编号", 1)
	if drifted == alipayBillSandboxDetailCSV {
		t.Fatal("夹具没改到表头，用例没测到目标")
	}
	if _, err := parseAlipayBill([]byte(drifted)); err == nil {
		t.Fatal("明细表头认不出（格式漂移）必须报错")
	}
	// 已有契约保持：无官方明细标记的「有表头 0 行」文件仍要报错（解析器与格式不匹配必须人看）。
	headerOnly := "#说明段\n服务费(元),支付宝交易号,金额（元）,商户订单号\n合计,,,\n"
	if _, err := parseAlipayBill([]byte(headerOnly)); err == nil {
		t.Fatal("无明细标记的 0 行文件既有契约被破坏")
	}
}
