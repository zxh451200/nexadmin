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
		{"im", "在线客服", "Live chat", "warning"},
		{"website", "官网留言", "Website message", "info"},
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
	// 计量单位：商品/报价/库存共用。业务表只存 value（piece、set、m²…），展示文案一律按当前语言取，
	// 所以英文站显示 set、中文站显示「套」，不需要在每个页面写死单位。
	// 内置项只锁 value：运营可以改译名（i18nJson）、可以新增「托」「柜」这类自定义单位，也可以停用。
	// value 词表与后台商品表单的原 UNITS 常量完全一致（piece / m² / L / mL 这些写法不能改，
	// 存量商品存的就是它们，改一个值就会让同一个单位在库里出现两种写法）。
	// 必须追加在末尾：类型 ID 由下标 i 推导（typeSeedID），插在中间会把既有安装的种子 ID 全部挤位。
	{Code: "unit", Name: "计量单位", Remark: "商品/报价/库存的计量单位，库里存 value，界面按语言显示 label", Disabled: false, Items: []seedItem{
		{"piece", "件", "Piece", "default"},
		{"set", "套", "Set", "default"},
		{"unit", "台", "Unit", "default"},
		{"pair", "对", "Pair", "default"},
		{"dozen", "打", "Dozen", "default"},
		{"roll", "卷", "Roll", "default"},
		{"sheet", "张", "Sheet", "default"},
		{"bag", "袋", "Bag", "default"},
		{"box", "箱", "Box", "default"},
		{"carton", "纸箱", "Carton", "default"},
		{"pallet", "托盘", "Pallet", "default"},
		{"barrel", "桶", "Barrel", "default"},
		{"kg", "千克", "Kilogram", "default"},
		{"g", "克", "Gram", "default"},
		{"ton", "吨", "Tonne", "default"},
		{"lb", "磅", "Pound", "default"},
		{"m", "米", "Meter", "default"},
		{"cm", "厘米", "Centimeter", "default"},
		{"mm", "毫米", "Millimeter", "default"},
		{"m²", "平方米", "Square meter", "default"},
		{"m³", "立方米", "Cubic meter", "default"},
		{"L", "升", "Liter", "default"},
		{"mL", "毫升", "Milliliter", "default"},
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
