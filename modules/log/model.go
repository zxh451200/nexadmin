package log

import (
	"encoding/json"
	"time"

	"nexadmin/core/database"
	"nexadmin/core/datascope"
	"nexadmin/pkg/idgen"
)

// 日志状态、失败原因分类（失败原因只给分类，不暴露细节，防账号枚举；真实原因只进服务端日志）。
const (
	StatusSuccess = "success"
	StatusFailed  = "failed"
)

// OperLog 对应 sys_oper_log。
type OperLog struct {
	database.TenantModel
	Module     string `gorm:"size:64;not null"`
	Type       string `gorm:"size:32;not null"`
	Operator   string `gorm:"size:64;not null"`
	IP         string `gorm:"column:ip;size:64"`
	Location   string `gorm:"size:128"`
	Status     string `gorm:"size:16;not null"`
	CostMs     int
	Method     string `gorm:"size:16;not null"`
	URL        string `gorm:"column:url;size:500;not null"`
	Params     string `gorm:"type:text"`
	Response   string `gorm:"type:text"`
	ErrorStack string `gorm:"type:text"`
}

func (OperLog) TableName() string { return "sys_oper_log" }

// LoginLog 对应 sys_login_log。
type LoginLog struct {
	database.TenantModel
	Username   string `gorm:"size:64;not null"`
	LoginType  string `gorm:"size:32;not null"`
	IP         string `gorm:"column:ip;size:64"`
	Location   string `gorm:"size:128"`
	Browser    string `gorm:"size:64"`
	OS         string `gorm:"column:os;size:64"`
	Status     string `gorm:"size:16;not null"`
	FailReason string `gorm:"size:32"`
}

func (LoginLog) TableName() string { return "sys_login_log" }

// OperView 与前端 OperLog 一致。
type OperView struct {
	ID         idgen.ID  `json:"id"`
	Module     string    `json:"module"`
	Type       string    `json:"type"`
	Operator   string    `json:"operator"`
	IP         string    `json:"ip"`
	Location   string    `json:"location,omitempty"`
	Status     string    `json:"status"`
	CostMs     int       `json:"costMs"`
	Method     string    `json:"method"`
	URL        string    `json:"url"`
	Params     any       `json:"params,omitempty"`
	Response   any       `json:"response,omitempty"`
	ErrorStack string    `json:"errorStack,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

// LoginView 与前端 LoginLog 一致。
type LoginView struct {
	ID         idgen.ID  `json:"id"`
	Username   string    `json:"username"`
	LoginType  string    `json:"loginType"`
	IP         string    `json:"ip"`
	Location   string    `json:"location,omitempty"`
	Browser    string    `json:"browser,omitempty"`
	OS         string    `json:"os,omitempty"`
	Status     string    `json:"status"`
	FailReason string    `json:"failReason,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Query 是两种日志列表共用的查询条件。
type Query struct {
	Keyword   string
	Status    string
	Type      string // 操作类型（操作日志）
	LoginType string // 登录方式（登录日志）
	StartDate string // YYYY-MM-DD，按应用时区
	EndDate   string
	Page      int
	PageSize  int
	Scope     *datascope.Scope // 操作者的数据范围；nil = 不裁剪（仅内部调用），HTTP 入口必须设置
}

// jsonOrText 把库里的文本还原成接口值：合法 JSON 原样输出，否则当字符串。
func jsonOrText(s string) any {
	if s == "" {
		return nil
	}
	if json.Valid([]byte(s)) {
		return json.RawMessage(s)
	}
	return s
}

func toOperView(l OperLog) OperView {
	return OperView{ID: l.ID, Module: l.Module, Type: l.Type, Operator: l.Operator, IP: l.IP, Location: l.Location,
		Status: l.Status, CostMs: l.CostMs, Method: l.Method, URL: l.URL, Params: jsonOrText(l.Params),
		Response: jsonOrText(l.Response), ErrorStack: l.ErrorStack, CreatedAt: l.CreatedAt}
}

func toLoginView(l LoginLog) LoginView {
	return LoginView{ID: l.ID, Username: l.Username, LoginType: l.LoginType, IP: l.IP, Location: l.Location,
		Browser: l.Browser, OS: l.OS, Status: l.Status, FailReason: l.FailReason, CreatedAt: l.CreatedAt}
}
