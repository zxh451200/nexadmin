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
  // 外链菜单（如官网插件的「打开官网」）不经过前端路由：放行渲染，由 Sidebar 负责新标签页打开
  const visible = (m: ServerMenu) => m.type !== 'button' && !m.hidden && m.status !== '0' && checkPerm(perms, m.perm)
  const leaf = (m: ServerMenu): MenuNode | null => {
    const p = (m.path ?? '').replace(/^\/+/, '')
    if (m.type !== 'menu' || !visible(m)) return null
    if (m.external) {
      return { key: m.id, title: m.title, icon: m.icon, path: m.path ?? '/', external: true }
    }
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

/** 在菜单树中按 pathname 找到当前项及其所属分组；外链项不参与匹配（不产生标签页与激活态） */
export function findMenuTrail(tree: MenuNode[], pathname: string): MenuNode[] {
  for (const group of tree) {
    const hit = group.children?.find((c) => !c.external && c.path && (pathname === c.path || pathname.startsWith(c.path + '/')))
    if (hit) return [group, hit]
  }
  return []
}

/** 目录节点没有自己的页面，取它下面第一个有路径的页面；外链项不是站内页面，跳过 */
export function firstLeafPath(node: MenuNode): string | undefined {
  if (node.path && !node.external) return node.path
  for (const c of node.children ?? []) {
    const p = firstLeafPath(c)
    if (p) return p
  }
  return undefined
}

/** 菜单里第一个能进的页面：Logo 与面包屑「首页」直接指向它，
 *  别再经 index 路由（那条只是重定向，会让标签条多出一个 "/" 空标签、内容区闪一帧空）。 */
export function homePathOf(tree: MenuNode[], fallback: string): string {
  for (const group of tree) {
    const p = firstLeafPath(group)
    if (p) return p
  }
  return fallback
}
