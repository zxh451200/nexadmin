export type ResolverResult = { errors?: Record<string, unknown> }
export type ResolverLike = (values: unknown, ctx: unknown, options: unknown) => ResolverResult | Promise<ResolverResult>

const RESOLVER_OPTIONS = { fields: {}, shouldUseNativeValidation: false, criteriaMode: 'firstError' }

/** 空值＝「用户还没填」：字符串看内容、数组看长度；数字与布尔本身就是值，不算空 */
export function isEmptyValue(value: unknown): boolean {
  if (typeof value === 'string') return value.trim() === ''
  if (Array.isArray(value)) return value.length === 0
  return value === undefined || value === null
}

/** 把所有字符串与数组清空，其余（数字、布尔）保持原值：用来问 schema「这些字段能空吗」 */
export function clearedCopy(values: Record<string, unknown>): Record<string, unknown> {
  return Object.fromEntries(
    Object.entries(values).map(([key, value]) => [
      key,
      typeof value === 'string' ? '' : Array.isArray(value) ? [] : value,
    ]),
  )
}

/**
 * 结构签名，用于「值没变就不必重算」。刻意不碰数组元素与嵌套对象：
 * 那些内容不影响必填集合（条件必填只看开关字段与数组长度），换来一个永不抛错的比较。
 */
export function valuesSignature(values: Record<string, unknown>): string {
  return Object.entries(values)
    .map(([key, value]) => {
      if (Array.isArray(value)) return `${key}[${value.length}]`
      if (value && typeof value === 'object') return `${key}{}`
      return `${key}=${String(value)}`
    })
    .join('|')
}

/**
 * 推出必填字段，判据是「这个字段能不能空着」，而不是「它现在有没有错」：
 * 1. 全部清空后仍报错 → schema 无条件要求它（即使已经填对，星号也要保留）；
 * 2. 当前为空且正在报错 → 条件必填（如类型选「菜单」时路径才必填）。
 * 只看「当前报错」会把选填字段误标成必填：参数（JSON）一旦写进非法内容就冒出星号，
 * 而它明明允许留空。
 */
export async function computeRequiredKeys(
  resolver: ResolverLike,
  values: Record<string, unknown>,
): Promise<Set<string>> {
  const current = await resolver(values, undefined, RESOLVER_OPTIONS)
  const cleared = await resolver(clearedCopy(values), undefined, RESOLVER_OPTIONS)
  const currentErrors = new Set(Object.keys(current?.errors ?? {}))
  const clearedErrors = new Set(Object.keys(cleared?.errors ?? {}))
  const required = new Set<string>()
  for (const [key, value] of Object.entries(values)) {
    if (clearedErrors.has(key) || (currentErrors.has(key) && isEmptyValue(value))) required.add(key)
  }
  return required
}
