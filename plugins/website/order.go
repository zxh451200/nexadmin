package website

// 站点订单：官网商品页的访客购买（匿名）→ pay 中台收款。
// 铁律与 pay 中台一致：金额由服务端按产品定价与数量计算，绝不透传客户端金额；
// 支付结果以订阅 pay.* 事件翻转本地状态，QueryByBiz 兜底（回调丢失补偿）。
// 订单是平台级表（与留言同归属），pay 支付单归属渠道所属租户（匿名下单在 pay 侧的
// 分配规则见 plugins/pay/service.go 的 Create anon 分支）。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"nexadmin/core/event"
	"nexadmin/core/response"
	"nexadmin/core/tenant"
	"nexadmin/pkg/idgen"
	"nexadmin/plugins/pay"
)

const (
	// OrderBizType 是站点订单在 pay 中台的业务幂等键类型。
	OrderBizType = "website_order"
	// maxOrderQty 是单笔购买数量上限：B2B 大单走询盘，在线购买是小额直购。
	maxOrderQty = 9999
)

// newSiteOrderNo 生成对外订单号：随机不可枚举（公开状态页按它查询，不能是顺序雪花）。
func newSiteOrderNo() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败极罕见；雪花兜底仍是唯一但不防枚举，可接受
		return fmt.Sprintf("SO%d", time.Now().UnixNano())
	}
	return "SO" + hex.EncodeToString(b)
}

// minorOf 定价金额 → 最小货币单位（全站价格按两位小数展示与存储；
// 零小数币种如 JPY 不在站点汇率表支持范围内，直接不启用收款）。
func minorOf(amount float64) int64 {
	if amount <= 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return 0
	}
	return int64(math.Round(amount * 100))
}

// SiteOrderForm 是访客下单表单（匿名 POST /website/orders 的 JSON）。
type SiteOrderForm struct {
	Slug    string `json:"slug"`
	Qty     int    `json:"qty"`
	Email   string `json:"email"`
	Phone   string `json:"phone"`
	Remark  string `json:"remark"`
	Channel string `json:"channel"`
}

// purchaseChannels 过滤出「访客可用的支付渠道」：已启用、已配置凭据、支持 page
// 跳转方式（微信原生扫码的 CodeURL 渲染不在本期范围）。
func purchaseChannels(chans []pay.ChannelView) []pay.ChannelView {
	out := make([]pay.ChannelView, 0, len(chans))
	for _, c := range chans {
		if !c.HasConfig {
			continue
		}
		if !c.Capabilities.SupportsMethod("page") {
			continue
		}
		out = append(out, c)
	}
	return out
}

// CreateSiteOrder 创建访客订单并拉起支付：返回订单与收银台地址。
// 币种策略：优先找「直接支持产品定价币种」的渠道；没有就找 CNY 渠道，
// 按站点汇率表把定价币种换算成 CNY 收款（外币单：orig = 定价币种，fx 由汇率表算出）。
func (s *Service) CreateSiteOrder(ctx context.Context, f SiteOrderForm, siteBaseURL, clientIP string) (*SiteOrder, string, error) {
	set, err := s.Settings(ctx)
	if err != nil {
		return nil, "", err
	}
	if !set.PurchaseEnabled {
		return nil, "", response.Unavailable("website.purchase_disabled")
	}
	f.Slug = strings.TrimSpace(f.Slug)
	var p Product
	if err := s.db.WithContext(ctx).Where("slug = ?", f.Slug).Take(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, "", response.NotFound("website.product_not_found")
		}
		return nil, "", err
	}
	if p.PriceMin <= 0 {
		return nil, "", response.Unavailable("website.purchase_unpriced")
	}
	qty := f.Qty
	if qty < 1 {
		qty = 1
	}
	if qty > maxOrderQty {
		return nil, "", response.BadRequest("website.order_qty_too_large", maxOrderQty)
	}
	email := strings.TrimSpace(f.Email)
	if email == "" || !strings.Contains(email, "@") || len(email) > 128 {
		return nil, "", response.BadRequest("website.order_email_required")
	}
	unitMinor := minorOf(p.PriceMin)
	if unitMinor <= 0 {
		return nil, "", response.Unavailable("website.purchase_unpriced")
	}
	amountMinor := unitMinor * int64(qty)
	order := &SiteOrder{
		OrderNo:      newSiteOrderNo(),
		ProductID:    p.ID,
		ProductSlug:  p.Slug,
		ProductTitle: p.Title,
		Qty:          qty,
		UnitPrice:    p.PriceMin,
		Currency:     strings.ToUpper(strings.TrimSpace(p.Currency)),
		AmountMinor:  amountMinor,
		BuyerEmail:   trimLen(email, 128),
		BuyerPhone:   trimLen(f.Phone, 64),
		Remark:       trimLen(f.Remark, 300),
		Status:       OrderPending,
	}
	if order.Currency == "" {
		order.Currency = "USD"
	}

	chans := purchaseChannels(pay.Channels(ctx))
	if len(chans) == 0 {
		return nil, "", response.Unavailable("website.purchase_unavailable")
	}
	// 渠道与币种：先找直接支持定价币种的渠道；找不到再找 CNY 渠道按汇率表换算。
	var ch *pay.ChannelView
	var payCurrency string
	var fxMicro int64
	for i := range chans {
		if chans[i].Capabilities.SupportsCurrency(order.Currency) {
			ch = &chans[i]
			payCurrency = order.Currency
			break
		}
	}
	if ch == nil {
		rates, rerr := parseCurrencyRates(set.CurrencyRatesJSON)
		cnyRate, hasCNY := rates["CNY"]
		curRate, hasCur := rates[order.Currency]
		if rerr != nil || !hasCNY || !hasCur || curRate <= 0 {
			return nil, "", response.Unavailable("website.purchase_unavailable")
		}
		for i := range chans {
			if chans[i].Capabilities.SupportsCurrency("CNY") {
				ch = &chans[i]
				payCurrency = "CNY"
				fxMicro = int64(math.Round((cnyRate / curRate) * 1e6))
				break
			}
		}
		if ch == nil || fxMicro <= 0 {
			return nil, "", response.Unavailable("website.purchase_unavailable")
		}
	}
	payAmountMinor := amountMinor
	if fxMicro > 0 && fxMicro != 1000000 {
		// 与 pay money.FxConvert 同口径：orig × (fxMicro / 1e6)，四舍五入到整数 minor
		payAmountMinor = int64(math.Round(float64(amountMinor) * float64(fxMicro) / 1e6))
	}
	if payAmountMinor <= 0 {
		return nil, "", response.Unavailable("website.purchase_unavailable")
	}

	in := pay.CreateInput{
		BizType:     OrderBizType,
		BizNo:       order.OrderNo,
		SubjectType: "website_product",
		SubjectID:   p.Slug,
		Subject:     fmt.Sprintf("%s × %d", p.Title, qty),
		AmountMinor: payAmountMinor,
		Currency:    payCurrency,
		ChannelCode: ch.Code,
		Method:      "page",
		Scene:       "website",
		ClientIP:    clientIP,
		ReturnURL:   siteBaseURL + "/orders/" + order.OrderNo,
		Metadata:    map[string]string{"orderNo": order.OrderNo},
	}
	if fxMicro > 0 {
		in.AmountOrigMinor = amountMinor
		in.OrigCurrency = order.Currency
		in.FxRateMicro = fxMicro
	}
	view, err := pay.Create(ctx, in)
	if err != nil {
		return nil, "", err
	}
	order.PaymentNo = view.PaymentNo
	order.PayCurrency = view.Currency
	order.PayAmountMinor = view.AmountMinor
	order.FxRateMicro = view.FxRateMicro
	if err := s.db.WithContext(ctx).Create(order).Error; err != nil {
		// 本地单落库失败：关掉刚建的支付单，避免悬空的 pending 资金单
		_, _ = pay.Close(tenant.Skip(ctx), view.PaymentNo)
		return nil, "", err
	}
	return order, view.PayURL, nil
}

// SiteOrderQuery 是管理端订单列表查询。Status 逗号拼接多值；Sort/Desc 表头排序；分页。
type SiteOrderQuery struct {
	Keyword  string
	Status   []string
	Sort     string
	Desc     bool
	Page     int
	PageSize int
}

// ValidOrderStatus 是订单状态筛选的取值白名单（与 model 里的状态常量一致）。
func ValidOrderStatus(st string) bool {
	switch st {
	case OrderPending, OrderPaid, OrderFailed, OrderClosed:
		return true
	}
	return false
}

// siteOrderFilter 是列表与导出共用的筛选，保证导出的文件与页面看到的列表同口径。
func siteOrderFilter(tx *gorm.DB, q SiteOrderQuery) *gorm.DB {
	if len(q.Status) > 0 {
		tx = tx.Where("status IN ?", q.Status)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		tx = tx.Where("LOWER(order_no) LIKE LOWER(?) OR LOWER(product_title) LIKE LOWER(?) OR LOWER(buyer_email) LIKE LOWER(?) OR LOWER(buyer_phone) LIKE LOWER(?)", like, like, like, like)
	}
	return tx
}

// ListSiteOrders 管理端订单列表（分页 + 关键词/状态过滤 + 表头排序）；默认新在前。
func (s *Service) ListSiteOrders(ctx context.Context, q SiteOrderQuery) ([]SiteOrder, int64, error) {
	tx := siteOrderFilter(s.db.WithContext(ctx).Model(&SiteOrder{}), q)
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []SiteOrder
	if err := tx.Order(contentOrder(q.Sort, q.Desc, map[string]string{
		"order_no":    "order_no",
		"product_title": "product_title",
		"status":      "status",
		"amount_minor": "amount_minor",
		"created_at":  "created_at",
	}, "id DESC")).Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// DeleteSiteOrders 批量删除订单（业务侧记录；资金流水仍以 pay 中台为准），返回实际删除条数。
func (s *Service) DeleteSiteOrders(ctx context.Context, ids []idgen.ID) (int64, error) {
	if len(ids) == 0 {
		return 0, response.BadRequest("website.order_ids_empty")
	}
	res := s.db.WithContext(ctx).Delete(&SiteOrder{}, "id IN ?", int64IDs(ids))
	if res.Error != nil {
		return 0, res.Error
	}
	if res.RowsAffected == 0 {
		return 0, response.NotFound("website.order_not_found")
	}
	return res.RowsAffected, nil
}

// SiteOrderByNo 按对外订单号取单（公开状态页与轮询用：订单号随机不可枚举）。
func (s *Service) SiteOrderByNo(ctx context.Context, no string) (*SiteOrder, error) {
	var o SiteOrder
	if err := s.db.WithContext(ctx).Where("order_no = ?", no).Take(&o).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, response.NotFound("website.order_not_found")
		}
		return nil, err
	}
	return &o, nil
}

// SyncSiteOrder 把本地订单与 pay 中台对齐：pending 时主动补查（回调丢失兜底），
// 翻转结果直接落库并返回最新状态。
func (s *Service) SyncSiteOrder(ctx context.Context, no string) (*SiteOrder, error) {
	o, err := s.SiteOrderByNo(ctx, no)
	if err != nil {
		return nil, err
	}
	if o.Status != OrderPending || o.PaymentNo == "" {
		return o, nil
	}
	// pay 侧查询带匿名上下文会撞租户闸：平台级站点单按全局查（tenant.Skip）
	view, err := pay.QueryByBiz(tenant.Skip(ctx), OrderBizType, o.OrderNo)
	if err != nil {
		return o, nil // pay 侧查询失败不拦状态页：按本地 pending 继续轮询
	}
	if view.Status == o.Status {
		return o, nil
	}
	switch view.Status {
	case pay.StatusSucceeded:
		o.Status = OrderPaid
	case pay.StatusFailed:
		o.Status = OrderFailed
	case pay.StatusClosed:
		o.Status = OrderClosed
	}
	updates := map[string]any{"status": o.Status, "updated_at": time.Now().UTC()}
	if o.Status == OrderPaid {
		updates["paid_at"] = time.Now().UTC()
	}
	if err := s.db.WithContext(ctx).Model(&SiteOrder{}).Where("id = ?", int64(o.ID)).
		Updates(updates).Error; err != nil {
		return nil, err
	}
	return o, nil
}

// handlePayEvent 订阅 pay.* 事件翻转站点订单状态：按 bizType 过滤，只认本插件的单。
func (s *Service) handlePayEvent(e event.Event) {
	var data pay.EventData
	switch v := e.Data.(type) {
	case pay.EventData:
		data = v
	case *pay.EventData:
		if v == nil {
			return
		}
		data = *v
	default:
		return
	}
	if data.Data.BizType != OrderBizType || data.Data.BizNo == "" {
		return
	}
	switch e.Name {
	case pay.EventSucceeded, pay.EventFailed, pay.EventClosed:
	default:
		return
	}
	ctx := context.Background()
	o, err := s.SiteOrderByNo(ctx, data.Data.BizNo)
	if err != nil || o.PaymentNo != data.Data.PaymentNo {
		return
	}
	next := o.Status
	switch e.Name {
	case pay.EventSucceeded:
		next = OrderPaid
	case pay.EventFailed:
		next = OrderFailed
	case pay.EventClosed:
		next = OrderClosed
	}
	if next == o.Status {
		return
	}
	updates := map[string]any{"status": next, "updated_at": time.Now().UTC()}
	if next == OrderPaid {
		updates["paid_at"] = time.Now().UTC()
	}
	// 状态翻转失败没有可靠的进程内上报面：事件重投或 QueryByBiz 兜底会再对齐
	_ = s.db.WithContext(ctx).Model(&SiteOrder{}).Where("id = ?", int64(o.ID)).Updates(updates).Error
}

// SiteOrderPayURL 重新拉起未付订单的收银台（幂等取回）：访客中途关掉收银台后，
// 从结果页「继续支付」回到渠道。pending 才有效，终态返回空。
func (s *Service) SiteOrderPayURL(ctx context.Context, no string) (*SiteOrder, string, error) {
	o, err := s.SyncSiteOrder(ctx, no)
	if err != nil {
		return nil, "", err
	}
	if o.Status != OrderPending {
		return o, "", nil
	}
	// 幂等取回：同 bizType/bizNo 的 Create 直接返回既有 pending 单的拉起参数。
	// 金额与币种取订单快照（pay 侧幂等命中后不会重新校验金额，但入参仍要过基本校验）；
	// 与首次下单同口径：换算单才带 orig 三件套，同币种单带 orig 会被 normalizeFx 拒掉。
	in := pay.CreateInput{
		BizType:     OrderBizType,
		BizNo:       o.OrderNo,
		SubjectType: "website_product",
		SubjectID:   o.ProductSlug,
		Subject:     fmt.Sprintf("%s × %d", o.ProductTitle, o.Qty),
		AmountMinor: o.PayAmountMinor,
		Currency:    o.PayCurrency,
		Method:      "page",
		Scene:       "website",
	}
	if o.PayCurrency != o.Currency {
		in.AmountOrigMinor = o.AmountMinor
		in.OrigCurrency = o.Currency
		in.FxRateMicro = o.FxRateMicro
	}
	view, err := pay.Create(ctx, in)
	if err != nil {
		return o, "", nil // 取回失败不拦状态页
	}
	// 幂等路径返回的可能还是旧单：确认 bizNo 匹配再给链接
	if view.BizNo != o.OrderNo {
		return o, "", nil
	}
	return o, view.PayURL, nil
}

// ---- HTTP 层（公开站） ----

// bindSiteOrderForm 解析并清洗下单表单。
func bindSiteOrderForm(c *gin.Context) (SiteOrderForm, bool) {
	var f SiteOrderForm
	if !response.BindJSON(c, &f) {
		return f, false
	}
	f.Slug = strings.TrimSpace(f.Slug)
	f.Email = strings.TrimSpace(f.Email)
	f.Phone = strings.TrimSpace(f.Phone)
	f.Remark = strings.TrimSpace(f.Remark)
	f.Channel = strings.TrimSpace(f.Channel)
	return f, true
}

// purchaseCreate POST /website/orders —— 匿名建单并返回收银台地址。
// fetch 提交回 JSON（site.js 解析后跳收银台）；无 JS 表单 303 直跳收银台，
// 失败 303 回商品页带 ?order_error=<key>（商品页渲染成提示条）。
// 开关关闭 / 无可用渠道一律 503 不留单。
func (h *siteHandler) purchaseCreate(c *gin.Context) {
	f, ok := bindSiteOrderForm(c)
	if !ok {
		return
	}
	langPrefix := strings.TrimSuffix(langOf(c).Path(""), "/")
	order, payURL, err := h.svc.CreateSiteOrder(c.Request.Context(), f, baseURL(c)+langPrefix, c.ClientIP())
	if err != nil {
		code := "website.order_failed"
		var re *response.Error
		if errors.As(err, &re) && re.Key != "" {
			code = re.Key
		}
		if wantsJSON(c) {
			response.Fail(c, err)
			return
		}
		c.Redirect(http.StatusSeeOther, langOf(c).Path("/products/"+f.Slug)+"?order_error="+url.QueryEscape(code))
		return
	}
	if wantsJSON(c) {
		response.OK(c, gin.H{"orderNo": order.OrderNo, "payUrl": payURL})
		return
	}
	if payURL != "" {
		c.Redirect(http.StatusSeeOther, payURL)
		return
	}
	c.Redirect(http.StatusSeeOther, langOf(c).Path("/orders/"+order.OrderNo))
}

// purchaseStatus GET /website/orders/:no/status —— 公开轮询端点：只回状态与金额，
// pending 时附带收银台地址（访客中途关掉收银台后「继续支付」用）。
func (h *siteHandler) purchaseStatus(c *gin.Context) {
	o, payURL, err := h.svc.SiteOrderPayURL(c.Request.Context(), c.Param("no"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	out := gin.H{
		"orderNo":      o.OrderNo,
		"status":       o.Status,
		"productTitle": o.ProductTitle,
		"qty":          o.Qty,
		"amountMinor":  o.AmountMinor,
		"currency":     o.Currency,
	}
	if payURL != "" {
		out["payUrl"] = payURL
	}
	response.OK(c, out)
}

// purchasePage GET /orders/:no —— 订单状态页（SSR）：访客支付完成跳回这里，
// site.js 轮询 status 直到终态；pending 且有收银台地址时渲染「继续支付」。
func (h *siteHandler) purchasePage(c *gin.Context) {
	lang := langOf(c)
	v, err := h.baseView(c.Request.Context(), lang.Code, 0)
	if err != nil {
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	o, payURL, err := h.svc.SiteOrderPayURL(c.Request.Context(), c.Param("no"))
	if err != nil {
		var re *response.Error
		if errors.As(err, &re) && re.Status == http.StatusNotFound {
			h.notFound(c, nil)
			return
		}
		c.String(http.StatusInternalServerError, "site unavailable")
		return
	}
	v.Title = v.composeTitle(v.TF("order.pageTitle", o.OrderNo))
	v.Order = &orderView{
		OrderNo:      o.OrderNo,
		ProductTitle: o.ProductTitle,
		Qty:          o.Qty,
		AmountText:   v.FxCur(o.Currency) + v.FxMoney(float64(o.AmountMinor)/100, o.Currency),
		Status:       o.Status,
		PayURL:       payURL,
	}
	h.render(c, "order", http.StatusOK, v)
}

// listSiteOrders GET /plugins/website/site-orders —— 管理端订单列表（分页/关键词/状态/排序）。
func (h *handler) listSiteOrders(c *gin.Context) {
	page, size := response.ParsePage(c)
	statuses := response.ParseList(c.Query("status"), response.MaxFilterValues)
	for _, st := range statuses {
		if !ValidOrderStatus(st) {
			response.Fail(c, response.BadRequest("website.order_status_invalid"))
			return
		}
	}
	sortField, desc := response.ParseSort(c)
	q := SiteOrderQuery{Page: page, PageSize: size, Keyword: c.Query("keyword"), Status: statuses, Sort: sortField, Desc: desc}
	list, total, err := h.svc.ListSiteOrders(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, q.Page, q.PageSize)
}

// deleteSiteOrders DELETE /plugins/website/site-orders?ids=1,2,3 —— 批量删除订单。
func (h *handler) deleteSiteOrders(c *gin.Context) {
	ids, err := parseIDs(c.Query("ids"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	n, err := h.svc.DeleteSiteOrders(c.Request.Context(), ids)
	respond(c, map[string]int64{"deleted": n}, err)
}

// orderErrorText 把无 JS 下单失败回跳的 i18n key 翻成站点文案；未知码给通用失败。
func orderErrorText(v *siteView, code string) string {
	switch code {
	case "":
		return ""
	case "website.order_email_required":
		return v.T("order.err.email")
	case "website.order_qty_too_large":
		return v.T("order.err.qty")
	case "website.purchase_disabled", "website.purchase_unavailable", "website.purchase_unpriced":
		return v.T("order.err.unavailable")
	}
	return v.T("order.err.fail")
}
