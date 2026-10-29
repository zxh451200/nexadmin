import { checkPerm } from '@/framework/auth/usePermission'
import { menuGroups } from './groups'
import { mergeRoutes } from './merge'
import { promoteLoneGroups } from './menu-shape'
import type { MenuNode, ModuleRoute } from './types'

const modules = import.meta.glob<{ routes?: ModuleRoute[] }>(
  ['/src/framework/modules/*/route.tsx', '/src/plugins/*/route.tsx', '/src/pages/**/route.tsx'],
  { eager: true },
)

/** 所有已发现的路由（去重）：框架模块 → 插件 → 用户页面，同 path 后者覆盖前者（规则见 merge.ts）。
 *  用户在 web/src/pages 里声明与内置页面相同的 path 即可整页替换。 */
export const allRoutes: ModuleRoute[] = mergeRoutes(modules)

/** 按权限过滤出可访问的路由 */
export function filterRoutes(perms: string[], routes: ModuleRoute[] = allRoutes) {
  return routes.filter((r) => checkPerm(perms, r.perm))
}

/** 插件未启用时其菜单项也不显示；active 为 null/undefined 表示未知（mock、旧后端），不拦 */
function pluginUsable(route: ModuleRoute | undefined, active?: Set<string> | null): boolean {
  return !route?.plugin || !active || active.has(route.plugin)
}

/** 生成菜单树：分组（按 groups 顺序）→ 菜单项（按 order）；空分组不出现 */
export function buildMenuTree(perms: string[], routes: ModuleRoute[] = allRoutes, active?: Set<string> | null): MenuNode[] {
  const visible = filterRoutes(perms, routes).filter((r) => !r.hidden && r.menu && pluginUsable(r, active))
  const tree: MenuNode[] = []
  for (const group of [...menuGroups].sort((a, b) => a.order - b.order)) {
    const items = visible
      .filter((r) => (r.menu!.group ?? 'system') === group.key)
      .sort((a, b) => (a.menu!.order ?? 100) - (b.menu!.order ?? 100))
    if (items.length === 0) continue
    tree.push({
      key: group.key,
      title: group.title,
      icon: group.icon,
      children: items.map((r) => ({
        key: r.path,
        title: r.menu!.title,
        icon: r.menu!.icon,
        path: `/${r.path}`,
      })),
    })
  }
  // 目录只剩一个可见页面时（角色只分到一个页面）不摆空壳目录，直接提成一级单项。
  return promoteLoneGroups(tree)
}

export interface ServerMenu {
  id: string
  type: 'dir' | 'menu' | 'button'
  title: string
  icon?: string
  path?: string
  perm?: string
  hidden?: boolean
  external?: boolean
  status?: '1' | '0'
  children?: ServerMenu[]
}

/** 用后端菜单树生成侧边栏（菜单管理里改的顺序、名称、显示隐藏都会生效）：
 *  按钮、隐藏、停用、外链、无权限、前端没有对应页面的节点都不出现 */
export function buildMenuFromServer(
  records: ServerMenu[],
  perms: string[],
  routes: ModuleRoute[] = allRoutes,
  active?: Set<string> | null,
): MenuNode[] {
  const byPath = new Map(routes.map((r) => [r.path, r]))
  const visible = (m: ServerMenu) => m.type !== 'button' && !m.hidden && !m.external && m.status !== '0' && checkPerm(perms, m.perm)
  const leaf = (m: ServerMenu): MenuNode | null => {
    const p = (m.path ?? '').replace(/^\/+/, '')
    if (m.type !== 'menu' || !visible(m)) return null
    const route = byPath.get(p)
    if (!route || !pluginUsable(route, active)) return null
    return { key: m.id, title: m.title, icon: m.icon, path: `/${p}` }
  }
  const tree: MenuNode[] = []
  for (const top of records) {
    if (top.type === 'dir' && visible(top)) {
      const children = (top.children ?? []).map(leaf).filter((x): x is MenuNode => !!x)
      if (children.length) tree.push({ key: top.id, title: top.title, icon: top.icon, children })
    } else {
      const item = leaf(top)
      if (item) tree.push({ key: `top-${top.id}`, title: '', children: [item] }) // 顶级单项（如工作台）不显示分组标题
    }
  }
  // 授权关掉某个插件后，分组可能只剩一个入口（如「导入导出」只剩导出中心）——
  // 这种退化目录直接提成一级单项，和「目录只包一个页面」的既有口径保持一致。
  return promoteLoneGroups(tree)
}

/** 在菜单树中按 pathname 找到当前项及其所属分组 */
export function findMenuTrail(tree: MenuNode[], pathname: string): MenuNode[] {
  for (const group of tree) {
    const hit = group.children?.find((c) => c.path && (pathname === c.path || pathname.startsWith(c.path + '/')))
    if (hit) return [group, hit]
  }
  return []
}
