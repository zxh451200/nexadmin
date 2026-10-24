package dict

// 出厂字典，与前端 web/src/framework/dict/seed.ts 一致；顺序决定 ID 与排序（第 i 个类型 sort=(i+1)*10）。

type seedItem struct{ Value, Label, LabelEn, Tag string }

type seedType struct {
	Code, Name, Remark string
	Disabled           bool
	Items              []seedItem
}

var seedTypes = []seedType{
	{Code: "sys_status", Name: "通用状态", Remark: "通用启用/停用状态", Disabled: false, Items: []seedItem{
		{"1", "启用", "Enabled", "success"},
		{"0", "停用", "Disabled", "default"},
	}},
	{Code: "sys_gender", Name: "性别", Remark: "用户性别", Disabled: false, Items: []seedItem{
		{"0", "未知", "Unknown", "default"},
		{"1", "男", "Male", "info"},
		{"2", "女", "Female", "warning"},
	}},
	{Code: "yes_no", Name: "是否", Remark: "布尔类取值", Disabled: false, Items: []seedItem{
		{"1", "是", "Yes", "success"},
		{"0", "否", "No", "default"},
	}},
	{Code: "notice_type", Name: "公告类型", Remark: "通知公告分类", Disabled: false, Items: []seedItem{
		{"notice", "通知", "Notice", "info"},
		{"announcement", "公告", "Announcement", "warning"},
		{"activity", "活动", "Activity", "success"},
	}},
	{Code: "notice_level", Name: "公告级别", Remark: "", Disabled: false, Items: []seedItem{
		{"normal", "普通", "Normal", "default"},
		{"important", "重要", "Important", "destructive"},
	}},
	{Code: "notice_status", Name: "公告状态", Remark: "", Disabled: false, Items: []seedItem{
		{"draft", "草稿", "Draft", "default"},
		{"published", "已发布", "Published", "success"},
		{"revoked", "已撤回", "Revoked", "warning"},
	}},
	{Code: "message_category", Name: "消息分类", Remark: "", Disabled: false, Items: []seedItem{
		{"system", "系统消息", "System", "info"},
		{"business", "业务消息", "Business", "success"},
		{"security", "安全提醒", "Security", "destructive"},
		{"todo", "待办提醒", "To-do", "warning"},
	}},
	{Code: "oper_type", Name: "操作类型", Remark: "", Disabled: false, Items: []seedItem{
		{"create", "新增", "Create", "success"},
		{"update", "修改", "Update", "info"},
		{"delete", "删除", "Delete", "destructive"},
		{"export", "导出", "Export", "default"},
		{"import", "导入", "Import", "default"},
		{"grant", "授权", "Grant", "warning"},
		{"sort", "调整顺序", "Reorder", "default"},
		{"other", "其他", "Other", "default"},
	}},
	{Code: "login_status", Name: "登录状态", Remark: "", Disabled: false, Items: []seedItem{
		{"success", "成功", "Success", "success"},
		{"failed", "失败", "Failed", "destructive"},
	}},
	{Code: "menu_type", Name: "菜单类型", Remark: "", Disabled: false, Items: []seedItem{
		{"dir", "目录", "Directory", "default"},
		{"menu", "菜单", "Menu", "info"},
		{"button", "按钮", "Button", "warning"},
	}},
	{Code: "data_scope", Name: "数据范围", Remark: "", Disabled: false, Items: []seedItem{
		{"all", "全部数据", "All data", "default"},
		{"custom", "自定义部门", "Custom departments", "default"},
		{"dept", "本部门", "Own department", "default"},
		{"deptAndChild", "本部门及以下", "Department and below", "default"},
		{"self", "仅本人", "Self only", "default"},
	}},
	{Code: "tenant_status", Name: "租户状态", Remark: "租户生命周期状态", Disabled: false, Items: []seedItem{
		{"active", "正常", "Active", "success"},
		{"suspended", "已停用", "Suspended", "warning"},
		{"expired", "已过期", "Expired", "destructive"},
	}},
	{Code: "online_device", Name: "在线设备", Remark: "", Disabled: true, Items: []seedItem{
		{"web", "网页端", "Web", "info"},
		{"mobile", "手机", "Mobile", "success"},
		{"tablet", "平板", "Tablet", "warning"},
		{"other", "其他", "Other", "default"},
	}},
}

// isBuiltinType 编码是否属于出厂字典类型。
func isBuiltinType(code string) bool {
	for _, st := range seedTypes {
		if st.Code == code {
			return true
		}
	}
	return false
}

// isBuiltinItem 某出厂字典类型下的 value 是否属于出厂数据项。
func isBuiltinItem(code, value string) bool {
	for _, st := range seedTypes {
		if st.Code != code {
			continue
		}
		for _, it := range st.Items {
			if it.Value == value {
				return true
			}
		}
	}
	return false
}
