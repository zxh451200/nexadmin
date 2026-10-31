package engine

import "errors"

// 引擎返回的哨兵错误。宿主可据此映射 HTTP 状态码，不要依赖错误文案。
var (
	// ErrInvalidDefinition 表示流程定义未通过 Validate。
	ErrInvalidDefinition = errors.New("flowlite: invalid definition")
	// ErrNotRunning 表示实例已结束，不能再推进。
	ErrNotRunning = errors.New("flowlite: instance is not running")
	// ErrNoActiveTask 表示当前没有待审批节点（通常在实例刚被并发推进后出现）。
	ErrNoActiveTask = errors.New("flowlite: no active approval task")
	// ErrMultipleActiveTasks 表示当前有多个待审批节点，无法确定操作目标。
	ErrMultipleActiveTasks = errors.New("flowlite: multiple active approval tasks")
	// ErrNotApprover 表示 actor 不是该节点的审批人。
	ErrNotApprover = errors.New("flowlite: actor is not an approver")
	// ErrUnknownNode 表示定义里不存在该节点。
	ErrUnknownNode = errors.New("flowlite: unknown node")
	// ErrNoEscalateTarget 表示节点未配置升级目标。
	ErrNoEscalateTarget = errors.New("flowlite: node has no escalate target")
	// ErrNoBranch 表示排他网关没有命中任何分支。
	ErrNoBranch = errors.New("flowlite: exclusive gateway has no matching branch")
)
