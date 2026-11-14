package pay

// 异步导出：支付单导出一律登记 core/job 任务，worker 流式写 xlsx 到存储，
// 用户在「导出中心」下载（无论数据量大小，行为一致；小任务几秒完成）。

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"nexadmin/core/auth"
	"nexadmin/core/job"
	"nexadmin/core/response"
	"nexadmin/pkg/excel"
	"nexadmin/pkg/idgen"
)

const (
	jobTypeOrderExport = "pay.order-export"
	// orderExportMax 是单次异步导出的支付单行数上限：财务导出宁可被要求缩窄范围，
	// 也不能拿到一份静默截断的对账单——少一行就是差异，超限报错而不是截断。
	orderExportMax = 50000
	// orderExportBatch 是每批读取行数。
	orderExportBatch = 500
)

const orderXlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// orderExportHeader 是导出列的白名单，中文表头与前端原 downloadCsv 的表头一致。
var orderExportHeader = []string{"支付单号", "业务类型", "主体", "渠道实例", "支付方式", "状态", "金额", "已退金额", "手续费", "渠道流水号", "创建时间", "支付时间"}

// orderExportParams 是任务参数：只含筛选条件与操作者身份，不含任何凭据。
// 枚举类筛选（Status/Channel/BizType/Scene/FailReason）是逗号拼接多值，与列表 query 参数同一契约。
type orderExportParams struct {
	Keyword     string `json:"keyword,omitempty"`
	Status      string `json:"status,omitempty"`
	Channel     string `json:"channelCode,omitempty"`
	SubjectID   string `json:"subjectId,omitempty"`
	BizType     string `json:"bizType,omitempty"`
	Scene       string `json:"scene,omitempty"`
	FailReason  string `json:"failReason,omitempty"`
	CreatedFrom string `json:"createdFrom,omitempty"`
	CreatedTo   string `json:"createdTo,omitempty"`
	ActorID     int64  `json:"actorId"`
}

// query 把任务参数还原成列表查询：多值按 CSV 解析，与 orderQueryFrom 同一契约。
func (p orderExportParams) query() OrderQuery {
	return OrderQuery{
		Keyword:    p.Keyword,
		Status:     response.ParseList(p.Status, response.MaxFilterValues),
		Channel:    response.ParseList(p.Channel, response.MaxFilterValues),
		SubjectID:  p.SubjectID,
		BizType:    response.ParseList(p.BizType, response.MaxFilterValues),
		Scene:      response.ParseList(p.Scene, response.MaxFilterValues),
		FailReason: response.ParseList(p.FailReason, response.MaxFilterValues),
		CreatedFrom: p.CreatedFrom, CreatedTo: p.CreatedTo,
	}
}

// orderExportBody 是 POST /orders/export-async 的请求体（与查询参数同形）。
type orderExportBody struct {
	Keyword     string `json:"keyword"`
	Status      string `json:"status"`
	ChannelCode string `json:"channelCode"`
	SubjectID   string `json:"subjectId"`
	BizType     string `json:"bizType"`
	Scene       string `json:"scene"`
	FailReason  string `json:"failReason"`
	CreatedFrom string `json:"createdFrom"`
	CreatedTo   string `json:"createdTo"`
}

// exportOrdersAsync POST /plugins/pay/orders/export-async：创建导出任务并立即返回任务 ID。
func (h *handler) exportOrdersAsync(c *gin.Context) {
	m := job.Default()
	if m == nil {
		fail(c, response.Unavailable("pay.export_center_disabled"))
		return
	}
	q := orderQueryFrom(c)
	if c.Request.Method == "POST" && c.Request.ContentLength != 0 {
		var b orderExportBody
		if c.ShouldBindJSON(&b) == nil {
			pick := func(dst *string, v string) {
				if v != "" {
					*dst = v
				}
			}
			pick(&q.Keyword, b.Keyword)
			pick(&q.SubjectID, b.SubjectID)
			pick(&q.CreatedFrom, b.CreatedFrom)
			pick(&q.CreatedTo, b.CreatedTo)
			// 多值字段（body 里同样是逗号拼接串）：非空才覆盖 query 解析结果
			if s := response.ParseList(b.Status, response.MaxFilterValues); len(s) > 0 {
				q.Status = s
			}
			if s := response.ParseList(b.ChannelCode, response.MaxFilterValues); len(s) > 0 {
				q.Channel = s
			}
			if s := response.ParseList(b.BizType, response.MaxFilterValues); len(s) > 0 {
				q.BizType = s
			}
			if s := response.ParseList(b.Scene, response.MaxFilterValues); len(s) > 0 {
				q.Scene = s
			}
			if s := response.ParseList(b.FailReason, response.MaxFilterValues); len(s) > 0 {
				q.FailReason = s
			}
		}
	}
	var owner int64
	if cl := auth.ClaimsFrom(c); cl != nil {
		owner = int64(cl.UserID)
	}
	j, err := m.Submit(c.Request.Context(), job.Submit{
		Type:  jobTypeOrderExport,
		Owner: idgen.ID(owner),
		Params: orderExportParams{
			Keyword: q.Keyword, Status: strings.Join(q.Status, ","), Channel: strings.Join(q.Channel, ","),
			SubjectID: q.SubjectID, BizType: strings.Join(q.BizType, ","), Scene: strings.Join(q.Scene, ","),
			FailReason: strings.Join(q.FailReason, ","),
			CreatedFrom: q.CreatedFrom, CreatedTo: q.CreatedTo,
			ActorID: owner,
		},
	})
	switch {
	case errors.Is(err, job.ErrTooMany):
		fail(c, response.New(429, "error.too_many"))
		return
	case err != nil:
		fail(c, err)
		return
	}
	response.OK(c, gin.H{"id": j.ID.String(), "type": j.Type, "status": j.Status})
}

// RunOrderExportJob 是 pay.order-export 的任务处理器：按列表同款筛选流式分批写 xlsx，逐批汇报进度。
// 数据范围（租户）以 ctx 为准由 job 管理器注入，与列表查询同一套 orderFilter 语义。
func (s *Service) RunOrderExportJob(ctx context.Context, j *job.Job, report job.Report) (job.ResultFile, error) {
	var p orderExportParams
	if err := j.Decode(&p); err != nil {
		return job.ResultFile{}, job.Errorf("任务参数无效")
	}
	tx, _, _, err := s.orderFilter(ctx, p.query())
	if err != nil {
		return job.ResultFile{}, err
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return job.ResultFile{}, err
	}
	if total > orderExportMax {
		return job.ResultFile{}, job.Errorf("命中 %d 笔支付单，超过单次导出上限 %d 笔，请缩小筛选范围后重试", total, orderExportMax)
	}
	report(0, fmt.Sprintf("共 %d 行", total))

	name := fmt.Sprintf("pay-orders-%s.xlsx", time.Now().Format("20060102"))
	return job.WriteResult(ctx, j, name, orderXlsxContentType, func(w io.Writer) error {
		x := excel.NewWriter(w)
		sh, err := x.NewSheet("订单", excel.SheetOptions{Header: orderExportHeader})
		if err != nil {
			return err
		}
		var done int64
		last := int64(math.MaxInt64)
		for done < total {
			if err := ctx.Err(); err != nil {
				return err
			}
			var rows []Order
			if err := tx.Where("id < ?", last).Order("id DESC").
				Limit(int(min(orderExportBatch, total-done))).Find(&rows).Error; err != nil {
				return err
			}
			if len(rows) == 0 {
				break
			}
			for i := range rows {
				if err := writeOrderExportRow(sh, &rows[i]); err != nil {
					return err
				}
			}
			done += int64(len(rows))
			last = int64(rows[len(rows)-1].ID)
			report(int(done*100/max(total, 1)), fmt.Sprintf("已导出 %d / %d 行", done, total))
		}
		return x.Close()
	})
}

// writeOrderExportRow 写一行导出数据：列序与 orderExportHeader 一致，取值口径与前端原
// downloadCsv 相同（业务列 = bizType/bizNo、主体 = subject 缺省回退 subjectId、状态用中文）。
func writeOrderExportRow(sh *excel.Sheet, o *Order) error {
	txn := ""
	if o.ChannelTxnID != nil {
		txn = *o.ChannelTxnID
	}
	subject := o.Subject
	if subject == "" {
		subject = o.SubjectID
	}
	return sh.WriteRow(
		o.PaymentNo, o.BizType+"/"+o.BizNo, subject, o.ChannelCode, o.Method,
		orderStatusName(o.Status), formatMoneyCell(o.AmountMinor, o.Currency),
		formatMoneyCell(o.RefundedMinor, o.Currency), formatMoneyCell(o.FeeMinor, o.Currency),
		txn, o.CreatedAt, o.PaidAt,
	)
}

// orderStatusName 把状态码翻成前端 zh-CN 文案（与 plugin_pay.status.* 一致）；未知状态原样输出。
var orderStatusName = func() func(string) string {
	names := map[string]string{
		StatusPending:           "待支付",
		StatusSucceeded:         "已支付",
		StatusFailed:            "支付失败",
		StatusClosed:            "已关闭",
		StatusRefunding:         "退款中",
		StatusPartiallyRefunded: "部分退款",
		StatusRefunded:          "已退款",
	}
	return func(s string) string {
		if n, ok := names[s]; ok {
			return n
		}
		return s
	}
}()

// zeroDecimalCurrencies 是 ISO 4217 零小数位币种（与前端 format.ts 的 ZERO_DECIMAL_CURRENCIES 同一份名单）：
// 渠道侧 minor 就是主单位，÷100 显示会差 100 倍。
var zeroDecimalCurrencies = map[string]bool{
	"JPY": true, "KRW": true, "VND": true, "CLP": true, "ISK": true, "XAF": true, "XOF": true, "XPF": true,
}

// formatMoneyCell 与前端 formatMoney 同口径：`<currency> <minor 格式化>`，
// 零小数位币种按 minor 直出，其余按两位小数。
func formatMoneyCell(minor int64, currency string) string {
	if zeroDecimalCurrencies[strings.ToUpper(currency)] {
		return fmt.Sprintf("%s %d", currency, minor)
	}
	sign := ""
	if minor < 0 {
		sign, minor = "-", -minor
	}
	return fmt.Sprintf("%s %s%d.%02d", currency, sign, minor/100, minor%100)
}
