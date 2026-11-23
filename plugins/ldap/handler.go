package ldap

import (
	"github.com/gin-gonic/gin"

	"nexadmin/core/response"
)

// 路由前缀:/api/v1/plugins/ldap。权限码:ldap:list 查看;ldap:edit 编辑与测试。
// 平台级共享配置(表里没有 tenant_id,一行服务所有租户):读接口只需登录 + ldap:list,
// 租户管理员也能看到当前配置(前端用 PlatformNotice 提示只读、收掉写入口);
// 写与测试才加 core.RequirePlatform() 限定平台调用方。

type handler struct{ svc *Service }

// GET /plugins/ldap/config
func (h *handler) config(c *gin.Context) {
	v, err := h.svc.ConfigView(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, v)
}

// PUT /plugins/ldap/config
func (h *handler) configSave(c *gin.Context) {
	var f LdapConfigForm
	if !response.BindJSON(c, &f) {
		return
	}
	v, err := h.svc.ConfigSave(c.Request.Context(), f)
	respond(c, v, err)
}

// POST /plugins/ldap/test —— 连通性测试(拨号 + 绑定 + 搜索空跑)。
// body 可选:带表单时测「当前填写值」(密码留空沿用已保存密码,不落库);不带时测已保存配置。
func (h *handler) test(c *gin.Context) {
	var form *LdapConfigForm
	if c.Request.Body != nil && c.Request.ContentLength > 0 {
		f := LdapConfigForm{}
		if !response.BindJSON(c, &f) {
			return
		}
		form = &f
	}
	if err := h.svc.TestConnection(c.Request.Context(), form); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, data)
}
