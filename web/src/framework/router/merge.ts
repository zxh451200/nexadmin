import type { ModuleRoute } from './types'

/** 路由来源优先级：框架模块 < 插件 < 用户页面（web/src/pages）。同 path 时后者覆盖前者。 */
export function routeRank(file: string): number {
  return file.startsWith('/src/pages') ? 2 : file.startsWith('/src/plugins') ? 1 : 0
}

/**
 * 合并各 route.tsx 导出的路由：按来源优先级升序处理，同 path（忽略前导斜杠）后者覆盖前者。
 * 因此用户在 web/src/pages 里声明与内置页面相同的 path，就能整页替换框架页面，无需改框架区文件。
 * 纯函数，不依赖 Vite，便于单测。
 */
export function mergeRoutes(files: Record<string, { routes?: ModuleRoute[] }>): ModuleRoute[] {
  const map = new Map<string, ModuleRoute>()
  const entries = Object.entries(files).sort(([a], [b]) => routeRank(a) - routeRank(b) || a.localeCompare(b))
  for (const [, mod] of entries) {
    for (const r of mod.routes ?? []) {
      const path = r.path.replace(/^\/+/, '')
      map.set(path, { ...r, path })
    }
  }
  return [...map.values()]
}
