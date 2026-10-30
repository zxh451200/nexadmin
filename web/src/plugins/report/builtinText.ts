import type { TFunction } from 'i18next'

const PREFIX = 'plugin_report.builtin.'

/**
 * 内置报表内容存的是 i18n 键（与菜单名同一套机制），用户自建内容存的是原文。
 * 只翻译我们自己的前缀，所以用户把报表起名叫任何中文/英文都不会被误翻。
 */
export function builtinText(t: TFunction, v?: string | null): string {
  if (!v) return ''
  return v.startsWith(PREFIX) ? String(t(v)) : v
}

/**
 * 编辑内置行时可编辑字段的取值：输入框显示译文，但只要用户没改（或改回了译文），
 * 保存时仍写回原来的 i18n 键——否则一次「保存」就把译文写死进库，切换语言再也不变。
 * originals 收多个原值，是为了删行/换序后仍能按「文案相等」认回自己的键。
 */
export function revertBuiltin(
  t: TFunction,
  original: string | null | undefined | Array<string | null | undefined>,
  current: string,
): string {
  const cur = current.trim()
  for (const o of Array.isArray(original) ? original : [original]) {
    if (o && o.startsWith(PREFIX) && cur === String(t(o))) return o
  }
  return current
}

/** 数据表 / 数据源的显示名：优先字典，缺失时用元数据里的兜底文案。 */
export function sourceText(t: TFunction, table?: string | null, fallback?: string | null): string {
  if (!table) return fallback ?? ''
  const v = String(t(`plugin_report.source.${table}`, { defaultValue: '' }))
  return v || fallback || table
}

/**
 * 列名显示：先按「表+列」取（method/scene/channel 这类同名不同义的列只有带表才分得清），
 * 再退到旧的「按列名」键，最后才用后端元数据里的兜底文案。
 */
export function fieldText(t: TFunction, table?: string | null, name?: string | null, fallback?: string | null): string {
  if (!name) return fallback ?? ''
  const scoped = table ? String(t(`plugin_report.field.${table}.${name}`, { defaultValue: '' })) : ''
  if (scoped) return scoped
  const flat = String(t(`plugin_report.field.${name}`, { defaultValue: '' }))
  return flat || fallback || name
}

/** 查询结果的列标题：内置度量/维度存的是键，其余按列名取字典。 */
export function columnText(
  t: TFunction,
  source: string | undefined,
  col: { label?: string | null; field?: string | null },
): string {
  if (col.label && col.label.startsWith(PREFIX)) return builtinText(t, col.label)
  return fieldText(t, source, col.field, col.label)
}
