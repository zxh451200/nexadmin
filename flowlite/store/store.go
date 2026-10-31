// Package store 定义 flowlite 的持久化端口。宿主用任意数据库实现这些接口，
// 引擎与运行时本身不认识任何数据库、多租户或框架类型。
package store

import (
	"context"
	"errors"
	"time"
)

// 存储层哨兵错误。宿主实现必须用这两个错误表达「未找到」与「并发冲突」。
var (
	ErrNotFound = errors.New("flowlite/store: not found")
	ErrConflict = errors.New("flowlite/store: revision conflict")
)

// 定义状态。
const (
	DefDraft     = "draft"
	DefPublished = "published"
	DefDisabled  = "disabled"
)

// Definition 是一条流程定义记录。Definition 是 engine.Definition 的 JSON；
// XML 是原始 BPMN XML（可选）——保留它可以无损往返标准建模器，避免每次编辑重排布局。
type Definition struct {
	ID         string
	Key        string
	Name       string
	Status     string
	Version    int
	Definition []byte
	XML        []byte
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// DefQuery 是定义列表过滤条件。
type DefQuery struct {
	Keyword  string
	Status   string
	Page     int
	PageSize int
}

// DefStore 是流程定义的持久化端口。Save 对已存在的 ID 递增 Version。
type DefStore interface {
	Get(ctx context.Context, id string) (*Definition, error)
	GetByKey(ctx context.Context, key string) (*Definition, error)
	Save(ctx context.Context, def *Definition) (*Definition, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, q DefQuery) ([]*Definition, int64, error)
}

// Instance 是一条流程实例记录。State 是 engine.Instance 的 JSON；Snapshot 是发起时
// 的 engine.Definition JSON——在途实例永远按自己的快照执行，不受后续改流程影响。
type Instance struct {
	ID          string
	DefID       string
	DefKey      string
	DefVersion  int
	Title       string
	BizType     string
	BusinessKey string
	Submitter   string
	Status      string
	Revision    int64
	// TenantID 是宿主自定义的租户/命名空间标识。引擎不解释它，
	// 只负责在扫描类操作里原样带出，便于宿主重建上下文。
	TenantID  string
	State     []byte
	Snapshot  []byte
	CreatedAt time.Time
	UpdatedAt time.Time
}

// InstQuery 是实例列表过滤条件。
type InstQuery struct {
	Keyword   string
	Status    string
	BizType   string
	Submitter string
	Page      int
	PageSize  int
}

// InstStore 是流程实例的持久化端口。Update 必须做乐观锁：只有当前 Revision 等于
// expectedRevision 时才写入，否则返回 ErrConflict。
type InstStore interface {
	Create(ctx context.Context, inst *Instance) error
	Get(ctx context.Context, id string) (*Instance, error)
	GetMany(ctx context.Context, ids []string) ([]*Instance, error)
	Update(ctx context.Context, inst *Instance, expectedRevision int64) error
	List(ctx context.Context, q InstQuery) ([]*Instance, int64, error)
	FindRunningByBiz(ctx context.Context, bizType, businessKey string) (*Instance, error)
	ListRunning(ctx context.Context, limit int) ([]*Instance, error)
}

// Task 是一条待办任务。
type Task struct {
	ID        string
	InstID    string
	DefKey    string
	NodeKey   string
	NodeName  string
	Assignee  string // 空 = 公开任务（任意登录用户可办）
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

const (
	TaskPending = "pending"
	TaskClosed  = "closed"
)

// PendingTask 是运行时同步给存储层的一条待办。
type PendingTask struct {
	NodeKey  string
	NodeName string
	Assignee string
}

// TaskQuery 是待办列表查询条件。
type TaskQuery struct {
	Page     int
	PageSize int
}

// TaskStore 持有物化的待办任务。SyncPending 用给定集合替换实例的全部待办：
// 同一 (NodeKey, Assignee) 保留原 CreatedAt，消失的置为 closed。
// 物化待办是「我的待办」能走索引分页、而不是全表扫描的前提。
type TaskStore interface {
	SyncPending(ctx context.Context, instID, defKey string, pending []PendingTask) error
	ListPending(ctx context.Context, assignee string, q TaskQuery) ([]*Task, int64, error)
	ListByInstance(ctx context.Context, instID string) ([]*Task, error)
}

// Action 是一条不可变的流转动作记录（审计与「我已办」查询用）。
type Action struct {
	ID      string
	InstID  string
	DefKey  string
	NodeKey string
	Actor   string
	Action  string
	Comment string
	At      time.Time
}

// ActionQuery 是动作查询条件。
type ActionQuery struct {
	Actor    string
	Actions  []string // 为空 = 全部动作
	Page     int
	PageSize int
}

// ActionStore 追加保存流转动作。记录只增不改。
type ActionStore interface {
	Record(ctx context.Context, a *Action) error
	List(ctx context.Context, q ActionQuery) ([]*Action, int64, error)
	ListByInstance(ctx context.Context, instID string) ([]*Action, error)
}
