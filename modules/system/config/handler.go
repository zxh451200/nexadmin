package config

import (
	"strings"

	"github.com/gin-gonic/gin"

	"nexadmin/core"
	"nexadmin/core/response"
	"nexadmin/pkg/idgen"
)

// platformCaller 报告调用方是否为平台级。判据单源在 tenant.IsPlatformCallerID（经 core.IsPlatformCaller），
// 这里不再自己写一份 `Super || tid == DefaultID`——中间件、载荷、通知收件人与这一处必须同一条线。
func platformCaller(c *gin.Context) bool { return core.IsPlatformCaller(c) }

// secretKeySuffixes 是「字段名以此结尾就算密钥」的后缀全集（比对前两边都走 normalizeKey：
// 小写、去掉下划线与连字符）。用后缀而不是精确名，是因为设置里的键都带前缀——mail 的 password、
// sms 的 accessSecret、SSO 的 clientSecret、smtpPassword、bindPassword——精确表会把变体一个个漏掉；
// 用包含匹配又会把 keyboard 当成 keyb 家族误码掉。后缀这两个坑都避开。
// 刻意不进表：publickey（公钥本就是公开的）、signature（签名结果是产物不是凭据）。
var secretKeySuffixes = []string{
	"password", "passwd", "passphrase", "secret", "apikey", "accesskey", "accesskeyid",
	"clientsecret", "privatekey", "token", "accesstoken", "refreshtoken", "authtoken",
	"credential", "sessionkey", "signkey", "secretkey",
}

// secretKeyContextPaths 是按「位置」才算密钥的字段——名字表抓不到的那一类。
// 钉钉 / 飞书 / 企业微信的 webhook 地址自带 access_key 或 access_token，而字段名只是 url；
// 同一个 url 出现在 site.logoUrl 里就是普通展示字段，所以只能按路径判。
var secretKeyContextPaths = map[string]bool{"notify.robots[].url": true}

// normalizeKey 把字段名收成比对形态（小写、去掉下划线 / 连字符 / 空格）。
func normalizeKey(k string) string {
	return strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(k))
}

// secretKeyIgnore 是「后缀像密钥、但值不是凭据」的字段（键为规范化形态，值就是理由）。
// 后缀表一定会认出它们，所以例外必须留名——否则一个数量上限被码成 ********，
// 界面显示「已配置」，保存时又把占位符当原值写回去，坏的是功能而不是安全性。
// 现在没有条目：ai.maxTokens 以 s 结尾，后缀表本来就不会码它——上一轮我以为是误码，
// 加了这条登记，闸立刻报「死条目」，删掉。留着这张表是因为它总有用途：
// 出现「名字像凭据但值是数量/开关/公开值」的字段时，在这里写理由，而不是让运行时误码。
var secretKeyIgnore = map[string]string{}

// isSecretKey 报告字段名是否按密钥处理：规范化后按后缀判定，例外表优先。
func isSecretKey(k string) bool {
	n := normalizeKey(k)
	if _, ok := secretKeyIgnore[n]; ok {
		return false
	}
	for _, suffix := range secretKeySuffixes {
		if strings.HasSuffix(n, suffix) {
			return true
		}
	}
	return false
}

// restrictAI 把 GET /settings 里的 ai 分区缩减成租户可见的最小信息（不含地址与密钥）。
func restrictAI(all map[string]any) {
	if ai, ok := all["ai"].(map[string]any); ok {
		all["ai"] = map[string]any{"enabled": ai["enabled"], "model": ai["model"]}
	}
}

func restrictSecrets(v any) { restrictSecretsAt(v, "") }

// restrictSecretsAt 递归抹掉非平台调用方不该看到的密钥字段：字符串值换成与 ai 分区同一个掩码，
// 非字符串（少见的对象型密钥）直接删键；整分区不返回会让设置页变成一片空白，那是另一种「入口蒸发」。
// path 是当前节点的位置（形如 notify.robots[].url 的前缀），只用于查 secretKeyContextPaths——
// robots 的 webhook 地址自带 access_token，字段名却只是 url，按名字表永远抓不到；
// 同一个 url 出现在 site.logoUrl 里就是普通展示字段，不许误伤。
func restrictSecretsAt(v any, path string) {
	switch node := v.(type) {
	case map[string]any:
		for k, child := range node {
			childPath := k
			if path != "" {
				childPath = path + "." + k
			}
			if k == "robots" {
				childPath = "notify.robots[]"
			}
			if isSecretKey(k) || secretKeyContextPaths[childPath] {
				if s, ok := child.(string); ok && s != "" {
					node[k] = secretMask
				} else {
					delete(node, k)
				}
				continue
			}
			restrictSecretsAt(child, childPath)
		}
	case []any:
		for _, item := range node {
			restrictSecretsAt(item, path)
		}
	}
}

// Handler 是参数与设置页的 HTTP 层。
type Handler struct{ svc *Service }

func pathID(c *gin.Context) (idgen.ID, bool) {
	id, err := idgen.Parse(c.Param("id"))
	if err != nil {
		response.Fail(c, response.BadRequest("error.bad_request").Wrap(err))
		return 0, false
	}
	return id, true
}

func done(c *gin.Context, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, nil)
}

// List GET /configs
func (h *Handler) List(c *gin.Context) {
	var q ListQuery
	if !response.BindQuery(c, &q) {
		return
	}
	page, size := response.ParsePage(c)
	list, total, err := h.svc.List(c.Request.Context(), q, page, size)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Page(c, list, total, page, size)
}

// Get GET /configs/:id
func (h *Handler) Get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	v, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Create POST /configs
func (h *Handler) Create(c *gin.Context) {
	var in Input
	if !response.BindJSON(c, &in) {
		return
	}
	v, err := h.svc.Create(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// Update PUT /configs/:id
func (h *Handler) Update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in Input
	if !response.BindJSON(c, &in) {
		return
	}
	v, err := h.svc.Update(c.Request.Context(), id, in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// DeleteOne DELETE /configs/:id
func (h *Handler) DeleteOne(c *gin.Context) {
	if id, ok := pathID(c); ok {
		done(c, h.svc.Delete(c.Request.Context(), []idgen.ID{id}))
	}
}

// DeleteMany DELETE /configs?ids=1,2
func (h *Handler) DeleteMany(c *gin.Context) {
	var ids []idgen.ID
	for _, s := range strings.Split(c.Query("ids"), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		id, err := idgen.Parse(s)
		if err != nil {
			response.Fail(c, response.BadRequest("error.bad_request").Wrap(err))
			return
		}
		ids = append(ids, id)
	}
	done(c, h.svc.Delete(c.Request.Context(), ids))
}

// GetUpload GET /configs/upload 与 GET /config/upload（后者供上传组件读取，登录即可）
func (h *Handler) GetUpload(c *gin.Context) {
	v, err := h.svc.Upload(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// SaveUpload PUT /configs/upload
func (h *Handler) SaveUpload(c *gin.Context) {
	var in UploadSettings
	if !response.BindJSON(c, &in) {
		return
	}
	v, err := h.svc.SaveUpload(c.Request.Context(), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// UploadTypes GET /configs/upload-types
func (h *Handler) UploadTypes(c *gin.Context) { response.OK(c, uploadTypeOptions) }

// RefreshCache POST /configs/refresh-cache：参数每次直接读库；唯一的进程内缓存是上传限制（几秒短缓存），这里立即丢弃它。
func (h *Handler) RefreshCache(c *gin.Context) {
	h.svc.invalidateUpload()
	response.OK(c, nil)
}

// ---- 设置页 ----

// Settings GET /settings
func (h *Handler) Settings(c *gin.Context) {
	v, err := h.svc.AllSettings(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	if !platformCaller(c) {
		restrictAI(v)
		// 其余分区的凭据同样不外泄：security/auth/sms/notify 里的密码与密钥是部署级的。
		restrictSecrets(v)
	}
	response.OK(c, v)
}

// PublicSettings GET /settings/public（匿名）
func (h *Handler) PublicSettings(c *gin.Context) {
	v, err := h.svc.Public(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// SaveSection PUT /settings/:section
// 整表 sys_setting 没有 tenant_id：任何分区都是部署级的一行，所以写入口在路由上统一挂
// core.RequirePlatform()（module.go），这里不再按分区名特判——原先只有 ai 分区受限，
// security / auth / sms / notify 谁都能改，一个租户把 maxAttempts 设成 1 就能把别人全锁在门外。
func (h *Handler) SaveSection(c *gin.Context) {
	var body map[string]any
	if !response.BindJSON(c, &body) {
		return
	}
	v, err := h.svc.SaveSection(c.Request.Context(), c.Param("section"), body)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// TestSection POST /settings/:section/test
// 同 SaveSection：测试一把也要拿部署级凭据去连外部服务，写入口已经整组只认平台调用方。
func (h *Handler) TestSection(c *gin.Context) {
	var body map[string]any
	if !response.BindJSON(c, &body) {
		return
	}
	v, err := h.svc.TestSection(c.Request.Context(), c.Param("section"), body)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}
