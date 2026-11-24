import {
  contributionSource,
  discoverContributions,
  mergeContributions,
  reportContributionConflicts,
  type ActivePluginIds,
} from '@/framework/contributions'
import type { ModuleRoute, SettingsContribution } from './types'

/** 路由来源优先级：框架模块 < 插件 < 用户页面（web/src/pages）。 */
export function routeRank(file: string): number {
  return contributionSource(file).priority
}

function discoverRouteContributions(files: Record<string, { routes?: ModuleRoute[] }>) {
  return discoverContributions(files, (module) => module.routes, {
    plugin: (route) => route.plugin,
    requiresPlugin: (route) => route.requiresPlugin ?? route.plugin,
  })
}

function routeKey(route: ModuleRoute): string {
  return route.path.replace(/^\/+/, '')
}

/**
 * 合并各 route.tsx 导出的路由。插件可用性先过滤再合并，避免停用的高优先级贡献遮住框架页。
 * 重复路径在构建期报错；只有 web/src/pages 对单个其他 owner 的显式覆盖可以获胜。
 */
export function mergeRoutes(files: Record<string, { routes?: ModuleRoute[] }>, active: ActivePluginIds = null): ModuleRoute[] {
  const { items, conflicts } = mergeContributions(discoverRouteContributions(files), routeKey, 'route', active)
  const fatal = conflicts.filter((conflict) => !(conflict.incoming.owner === 'pages' && conflict.current.owner !== 'pages'))
  if (fatal.length > 0) {
    throw new Error(
      `Duplicate route contributions:\n${fatal.map((conflict) => `- ${conflict.message}`).join('\n')}\n` +
        'Only one explicit web/src/pages route may override another owner; remove or rename conflicting routes.',
    )
  }
  reportContributionConflicts(conflicts)
  return items.map((item) => ({
    ...item.value,
    path: routeKey(item.value),
    plugin: item.value.plugin ?? item.plugin,
    requiresPlugin: item.requiresPlugin,
  }))
}

/** 设置分区与路由卡片共用同一贡献合并器；内置分区作为 framework 贡献参与同 key 裁决。 */
export function mergeSettings(
  files: Record<string, { routes?: ModuleRoute[] }>,
  active: ActivePluginIds = null,
  builtins: readonly SettingsContribution[] = [],
): SettingsContribution[] {
  const source = contributionSource('/src/framework/modules/settings/route.tsx')
  const builtinEntries = builtins.map((value, index) => ({ ...source, value, index }))
  const extra = discoverContributions(
    files,
    (module) =>
      module.routes?.flatMap((route) =>
        route.settings
          ? [{ ...route.settings, perm: route.perm, requiresPlugin: route.requiresPlugin ?? route.plugin }]
          : [],
      ),
    { requiresPlugin: (section) => section.requiresPlugin },
  )
  const { items, conflicts } = mergeContributions([...builtinEntries, ...extra], (section) => section.key, 'settings', active)
  reportContributionConflicts(conflicts)
  return items.map((item) => item.value)
}
