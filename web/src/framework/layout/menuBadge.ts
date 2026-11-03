import { useMemo } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'

/**
 * 菜单徽标：业务模块按菜单 path 注册一个计数源，Sidebar 只对当前用户可见的
 * 徽标位轮询并渲染。框架不关心计数从哪来——插件/模块在自己的 route.tsx
 * （启动时被 eager 加载）里调 registerMenuBadge 即可，不用改框架代码。
 */
export interface MenuBadgeSource {
  /** 菜单 path（与 MenuNode.path 一致，带前导 /） */
  path: string
  /** 返回当前计数；无权限/插件未启用时应走 silent 请求，由这里的 catch 归零兜底 */
  load: () => Promise<number>
}

const sources: MenuBadgeSource[] = []

/** 注册徽标数据源；同 path 重复注册时覆盖（测试与热重载安全） */
export function registerMenuBadge(source: MenuBadgeSource) {
  const i = sources.findIndex((s) => s.path === source.path)
  if (i >= 0) sources[i] = source
  else sources.push(source)
}

export const MENU_BADGES_KEY = ['menu-badges'] as const

/** 操作影响了某个徽标的计数后在页面里手动刷新（如留言收件箱批量处理后）。 */
export function useInvalidateMenuBadges() {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: MENU_BADGES_KEY })
}

/** Sidebar 用：只对当前菜单里可见的徽标位轮询（60s）。单个源失败按 0 兜底，别让一个坏源拖垮整个侧边栏。 */
export function useMenuBadges(paths: string[]): Record<string, number> {
  const list = useMemo(() => sources.filter((s) => paths.includes(s.path)), [paths])
  const { data } = useQuery({
    queryKey: [...MENU_BADGES_KEY, list.map((s) => s.path)],
    queryFn: async () => {
      const entries = await Promise.all(
        list.map(async (s) => {
          try {
            return [s.path, await s.load()] as const
          } catch {
            return [s.path, 0] as const
          }
        }),
      )
      return Object.fromEntries(entries)
    },
    refetchInterval: 60_000,
    staleTime: 30_000,
  })
  return data ?? {}
}
