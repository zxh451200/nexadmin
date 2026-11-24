/** 前端贡献的统一来源与合并规则。路由、工作台卡片和设置分区都不得各自推断优先级。 */
export type ContributionOwner = 'framework' | 'plugin' | 'pages'

/** null = 后端问不到，保留插件贡献；undefined = 尚未问到，暂时过滤插件贡献。 */
export type ActivePluginIds = readonly string[] | null | undefined

export interface ContributionSource {
  file: string
  owner: ContributionOwner
  priority: number
  plugin?: string
}

export interface DiscoveredContribution<T> extends ContributionSource {
  value: T
  index: number
  requiresPlugin?: string
}

export type ResolvedContribution<T> = DiscoveredContribution<T>

export interface ContributionConflict<T> {
  kind: 'route' | 'card' | 'settings'
  key: string
  current: ResolvedContribution<T>
  incoming: ResolvedContribution<T>
  resolution: 'override' | 'keep'
  message: string
}

export interface ContributionMergeResult<T> {
  items: ResolvedContribution<T>[]
  conflicts: ContributionConflict<T>[]
}

interface ContributionHooks<T> {
  plugin?: (value: T, source: ContributionSource) => string | undefined
  requiresPlugin?: (value: T, source: ContributionSource) => string | undefined
}

const OWNER_PRIORITY: Record<ContributionOwner, number> = { framework: 0, plugin: 1, pages: 2 }

/** Vite 的 glob key 使用 POSIX 路径；同时兼容调用方传入 Windows 路径。 */
export function contributionSource(file: string): ContributionSource {
  const normalized = `/${file.replace(/\\/g, '/').replace(/^\/+/, '')}`
  const owner: ContributionOwner = normalized.startsWith('/src/pages/')
    ? 'pages'
    : normalized.startsWith('/src/plugins/')
      ? 'plugin'
      : 'framework'
  const plugin = owner === 'plugin' ? /^\/src\/plugins\/([^/]+)\//.exec(normalized)?.[1] : undefined
  return { file: normalized, owner, priority: OWNER_PRIORITY[owner], plugin }
}

/** 先识别贡献及其归属，再按调用方提供的导出名读取；此阶段不做覆盖合并。 */
export function discoverContributions<T, M>(
  files: Record<string, M>,
  read: (module: M, source: ContributionSource) => readonly T[] | undefined,
  hooks: ContributionHooks<T> = {},
): DiscoveredContribution<T>[] {
  const entries: DiscoveredContribution<T>[] = []
  for (const [file, module] of Object.entries(files)) {
    const source = contributionSource(file)
    for (const [index, value] of (read(module, source) ?? []).entries()) {
      const plugin = hooks.plugin?.(value, source) ?? source.plugin
      entries.push({
        ...source,
        value,
        index,
        plugin,
        requiresPlugin: hooks.requiresPlugin?.(value, source) ?? plugin,
      })
    }
  }
  return entries
}

export function contributionIsActive(contribution: DiscoveredContribution<unknown>, active: ActivePluginIds): boolean {
  if (!contribution.requiresPlugin || active === null) return true
  return (active ?? []).includes(contribution.requiresPlugin)
}

/**
 * 先按 requiresPlugin/active 过滤，再按 framework < plugin < pages 合并。
 * 只有更高 owner 优先级能覆盖；同 owner 重复保持先发现项并给出冲突诊断。
 * 非 pages 的获胜项继承前项的插件归属，pages 仅保留自己显式声明的依赖。
 * 路由调用方会进一步拒绝 plugin 覆盖 framework，只有 pages 业务覆盖可以获胜。
 */
export function mergeContributions<T>(
  contributions: readonly DiscoveredContribution<T>[],
  keyOf: (value: T) => string,
  kind: ContributionConflict<T>['kind'],
  active: ActivePluginIds = null,
): ContributionMergeResult<T> {
  const entries = contributions
    .filter((entry) => active === null || contributionIsActive(entry, active))
    .sort((a, b) => a.priority - b.priority || a.file.localeCompare(b.file) || a.index - b.index)
  const byKey = new Map<string, ResolvedContribution<T>>()
  const conflicts: ContributionConflict<T>[] = []

  for (const incoming of entries) {
    const key = keyOf(incoming.value)
    const current = byKey.get(key)
    if (!current) {
      byKey.set(key, incoming)
      continue
    }

    const override = incoming.priority > current.priority
    const resolution = override ? 'override' : 'keep'
    if (override) {
      byKey.set(key, {
        ...incoming,
        plugin: incoming.owner === 'pages' ? incoming.plugin : (incoming.plugin ?? current.plugin),
        requiresPlugin:
          incoming.owner === 'pages' ? incoming.requiresPlugin : (incoming.requiresPlugin ?? current.requiresPlugin),
      })
    }
    conflicts.push({
      kind,
      key,
      current,
      incoming,
      resolution,
      message:
        `${kind} "${key}" conflicts between ${current.file} (${current.owner}) and ` +
        `${incoming.file} (${incoming.owner}); ${resolution === 'override' ? 'higher priority wins' : 'first contribution kept'}`,
    })
  }

  return { items: [...byKey.values()], conflicts }
}

const reported = new Set<string>()

/** 卡片和设置允许覆盖，但冲突不可静默；同一诊断只在当前会话打印一次。 */
export function reportContributionConflicts<T>(conflicts: readonly ContributionConflict<T>[]): void {
  for (const conflict of conflicts) {
    const signature = `${conflict.kind}:${conflict.key}:${conflict.current.file}:${conflict.incoming.file}`
    if (reported.has(signature)) continue
    reported.add(signature)
    console.warn(`[nexadmin contributions] ${conflict.message}`)
  }
}
