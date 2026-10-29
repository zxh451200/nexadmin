// tree-expand.ts 是「全部展开 / 全部收起」合并成一个按钮后的联动判定：
// 只有当前一个节点都没展开时，按钮才给「全部展开」；其余情况（全展开或部分展开）都给「全部收起」。
// 与侧栏底部按钮同一套逻辑，纯函数便于单测。

/** ids 为所有可展开节点（有子节点的行）；expanded 为当前已展开集合。ids 为空时返回 false（没有可操作对象）。 */
export function allCollapsed(ids: readonly string[], expanded: ReadonlySet<string>): boolean {
  return ids.length > 0 && ids.every((id) => !expanded.has(id))
}

/** 按联动判定给出下一次点击的展开集合：该展开时全展开，否则全收起。 */
export function nextExpandedAfterBulkToggle(ids: readonly string[], expanded: ReadonlySet<string>): Set<string> {
  return allCollapsed(ids, expanded) ? new Set(ids) : new Set<string>()
}
