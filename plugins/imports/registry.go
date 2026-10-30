package imports

// registry.go 是导入源的注册表。业务模块在自己的 Register(d) 里调用 RegisterSource,
// 把「可导入的表 + 逐行校验/落库闭包」交给本插件;插件负责文件解析、模板下载、
// 执行、错误报告与导入台账,业务方只写一行行的业务逻辑。

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"nexadmin/pkg/importspec"
)

// 类型别名：契约本体在 pkg/importspec（提供方只依赖中立包，不反向依赖本插件）。
type (
	Column         = importspec.Column
	Source         = importspec.Source
	SourceProvider = importspec.Provider
)

// RowError 是一行导入失败的记录。
type RowError struct {
	Row     int    `json:"row"` // 文件中的数据行号(1 起,不含表头)
	Key     string `json:"key"` // 相关列标识(可空)
	Message string `json:"message"`
}

var (
	mu      sync.RWMutex
	sources = map[string]Source{}
)

// RegisterSource 注册一个导入源;重名返回错误(启动期注册,冲突即配置错误)。
func RegisterSource(s Source) error {
	if s.Name == "" || s.Title == "" || s.Handler == nil || len(s.Columns) == 0 {
		return errors.New("imports: source name/title/columns/handler are required")
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := sources[s.Name]; dup {
		return fmt.Errorf("imports: source %q already registered", s.Name)
	}
	sources[s.Name] = s
	return nil
}

// MustRegisterSource 同 RegisterSource,冲突时 panic(用于 init 期注册)。
func MustRegisterSource(s Source) {
	if err := RegisterSource(s); err != nil {
		panic(err)
	}
}

// getSource 取一个已注册导入源。
func getSource(name string) (Source, error) {
	mu.RLock()
	defer mu.RUnlock()
	s, ok := sources[name]
	if !ok {
		return Source{}, fmt.Errorf("imports: unknown source %q", name)
	}
	return s, nil
}

// SourceMeta 是导入源列表视图(给前端做来源下拉与列说明)。
type SourceMeta struct {
	Name    string   `json:"name"`
	Title   string   `json:"title"`
	Columns []Column `json:"columns"`
}

// ListSources 列出全部已注册导入源(按名称排序),标题与列名按请求语言渲染。
func ListSources(ctx context.Context) []SourceMeta {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]SourceMeta, 0, len(sources))
	for _, s := range sources {
		s = localize(ctx, s)
		out = append(out, SourceMeta{Name: s.Name, Title: s.Title, Columns: append([]Column(nil), s.Columns...)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
