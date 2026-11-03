package plugin

// 运行期单个插件的停用/启用（操作员开关）。
//
// 停用清单 = 配置的 disabled_plugins ∪ 运行期叠加层，两条来源在 applyStatusLocked 里
// 汇成同一个判定，所以「配置停用的插件」与「页面停用的插件」的 Status/ReasonKey 形状一致。
// 配置优先：配置里停用的插件，运行期开关打不开（SetEnabled 回 ErrDisabledByConfig）——
// 否则配置与页面会各说一个真相。要恢复只能改配置后重启。
//
// 持久化边界（务必知道）：叠加层写在 <root>/data/licenses/disabled.json，是**节点本地文件**。
// 选它而不是数据库：授权文件已经在 data/licenses/ 下，插件管理器本来就直接读写这个目录，
// 不需要新建表、不需要迁移、也不需要把设置服务注入进来。
// 代价是**集群多节点不会自动同步**：在 A 节点点的开关，B 节点看不见。
// 现阶段的正确操作是逐节点各切一次开关，或者继续用配置 disabled_plugins（随配置分发，天然一致）；
// 想集中管理需要后续把它接到设置服务。
//
// 能立刻生效的：插件路由（gate/admit 按 live status 判定，停用后 403）、侧栏菜单可见性
// （MenuVisible 同源）、/plugins/active。
// 不能立刻生效的（见 docs/plugin-development.md「已知限制」）：已建立的 WebSocket/SSE 连接、
// 事件订阅、作业注册，以及需要重启才能重新拉起、且未实现 RestartableStarter 的后台 worker。

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// 停用原因的字典 key（双语见 core/i18n/locales）。
const (
	reasonDisabledByConfig   = "plugin.reason.disabled_by_config"
	reasonDisabledByOperator = "plugin.reason.disabled_by_operator"
)

// 启停开关的拒绝原因（modules/plugin 按这些哨兵映射 HTTP 状态与文案）。
var (
	// ErrDisabledByConfig 表示插件被配置 disabled_plugins 停用，运行期开关无法打开。
	ErrDisabledByConfig = errors.New("plugin: disabled by disabled_plugins config")
	// ErrNotManageable 表示插件的清单/注册本身有问题（fatal），启停开关对它不适用。
	ErrNotManageable = errors.New("plugin: plugin has fatal manifest problems")
	// ErrDisabledByOperator 表示插件正被运行期开关停用：导入授权不改变停用状态，
	// 需要先在插件管理页启用（与配置停用区分开，两者的恢复动作不一样）。
	ErrDisabledByOperator = errors.New("plugin: disabled by the runtime switch")
)

// DisabledPath 返回运行期停用清单的路径：<root>/data/licenses/disabled.json。
func DisabledPath(root string) string {
	return filepath.Join(root, "data", "licenses", "disabled.json")
}

// disabledFile 用对象包一层 ids（而不是裸数组），以后要加更新时间/操作者不用破格式。
type disabledFile struct {
	IDs []string `json:"ids"`
}

// LoadDisabledOverlay 读取节点本地的运行期停用叠加层。
// 文件不存在 = 没人点过开关，返回空集合；读不动或格式不对 = 报错，由调用方决定（Mount 里只告警，
// 宁可让插件按未停用继续跑，也不要在启动路径上因为一个可选文件炸掉整个服务）。
func LoadDisabledOverlay(root string) (map[string]bool, error) {
	out := map[string]bool{}
	raw, err := os.ReadFile(DisabledPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	var f disabledFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return out, fmt.Errorf("%s 解析失败: %w", DisabledPath(root), err)
	}
	for _, id := range f.IDs {
		if id = normalizeID(id); id != "" {
			out[id] = true
		}
	}
	return out, nil
}

// saveDisabledOverlay 原子写入叠加层（临时文件 + rename，权限与授权文件一致）。
// 调用方不得持有 m.mu：这里只有文件 IO。
func saveDisabledOverlay(root string, ids []string) error {
	p := DisabledPath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	sort.Strings(ids)
	raw, err := json.MarshalIndent(disabledFile{IDs: ids}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".disabled-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // rename 成功后临时文件已不存在
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// normalizeID 去掉首尾空白并转小写：配置里的 disabled_plugins 也是这么处理的（见 LoadDisabledPlugins），
// 两条来源要在同一个口径上取并集。
func normalizeID(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// overlayIDs 返回叠加层的有序 id 列表（写盘用）。调用方需持有 m.mu 或保证无并发。
func overlayIDs(set map[string]bool) []string {
	ids := make([]string, 0, len(set))
	for id, on := range set {
		if on {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// SetEnabled 运行期停用/启用一个插件，返回变更后的 Info（含 needRestart / stopping）。
//
// 生效范围（不要对外宣称「热卸载」）：
//   - 立刻生效：插件路由（gate/admit 按 live status 回 403）、ActiveIDs、侧栏菜单可见性；
//   - 不立刻生效：后台 worker——未实现 RestartableStarter 的插件被停用后，run.ctx 已取消且
//     无法重新拉起，重新启用会留下 NeedRestart=true，enabledLocked 仍为假，所以接口和菜单
//     也要等重启才恢复。这是刻意的诚实表达，不伪造「已经回来了」。
//
// 拒绝情形：未知 id 回 ErrNotFound；被配置 disabled_plugins 停用的插件不能用开关打开
// （ErrDisabledByConfig）；清单/注册有致命问题的插件开关不适用（ErrNotManageable）。
//
// 锁序：swMu -> m.mu，且 m.mu 绝不覆盖文件 IO。
//  1. swMu 全程持有：同一时刻只有一个操作员改开关，避免「内存用了 A、磁盘写了 B」。
//  2. 第一次进 m.mu 只做校验与算出要写的清单，随即解锁。
//  3. 锁外落盘（saveDisabledOverlay）；失败就原样返回，内存不动——磁盘是唯一真相。
//  4. 第二次进 m.mu 改叠加层 + applyStatusLocked（复用启动判定）+ syncLifecycleLocked 让 worker
//     立刻取消/重新拉起，然后取 Info 副本。
//     请求路径（admit/MenuVisible/ActiveIDs）只取 m.mu，不会排在 swMu 后面。
func (m *Manager) SetEnabled(id string, on bool) (Info, error) {
	m.swMu.Lock()
	defer m.swMu.Unlock()

	m.mu.Lock()
	e, ok := m.entries[id]
	switch {
	case !ok || e.p.ID != id:
		m.mu.Unlock()
		return Info{}, ErrNotFound
	case e.fatal:
		info := e.info
		m.mu.Unlock()
		return info, ErrNotManageable
	case on && m.disabledReasonLocked(id) == reasonDisabledByConfig:
		info := e.info
		m.mu.Unlock()
		return info, ErrDisabledByConfig
	case m.opDisabled[id] == !on:
		// 叠加层已经是目标状态：不写盘、不改状态，直接回当前 Info。
		// （注意口径：map 里「有记录」= 被停用，所以期望值是 !on。）
		info := e.info
		m.mu.Unlock()
		return info, nil
	}
	// 锁外要用的两样东西先在锁内取好：插件 edition（决定是否重读授权）和落盘的目标清单。
	edition := e.p.Edition
	target := make(map[string]bool, len(m.opDisabled)+1)
	for k, v := range m.opDisabled {
		target[k] = v
	}
	if on {
		delete(target, id)
	} else {
		target[id] = true
	}
	m.mu.Unlock()

	// 锁外 IO：启用 pro 插件要重读授权（停用期间可能已过期或被吊销），停用清单要落盘。
	var l *License
	var lerr error
	if on && edition == Pro {
		l, lerr = checkFile(m.opts.Root, id, m.opts.Now(), m.opts.Keys) // m.opts 构造后只读，可锁外访问
	}
	if err := saveDisabledOverlay(m.opts.Root, overlayIDs(target)); err != nil {
		return m.infoOf(id), fmt.Errorf("保存插件停用清单失败: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.opDisabled = target
	if e, ok := m.entries[id]; ok {
		m.applyStatusLocked(e, l, lerr)
		m.syncLifecycleLocked(e)
		return e.info, nil
	}
	return Info{}, ErrNotFound
}

// infoOf 取当前 Info 副本（用于 IO 失败时把未变化的状态回给调用方）。
func (m *Manager) infoOf(id string) Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.entries[id]; ok {
		return e.info
	}
	return Info{}
}

// disabledOverlayIDs 返回运行期叠加层里的插件 id（已排序）。只读，供同包用例与排障断点观察：
// 配置里的 disabled_plugins 不在此列，完整停用清单 = 两者并集（对外看状态请用 Info.Status
// 与 ReasonKey，那才是两条来源汇合后的结果）。
func (m *Manager) disabledOverlayIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return overlayIDs(m.opDisabled)
}
