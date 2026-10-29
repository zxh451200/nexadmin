/**
 * 侧栏节点的「退化目录」整形。单独成文件、零依赖：discover.ts 里带 import.meta.glob 和路由表，
 * 没法在 node --test 里加载，而这条规则值得有单测。
 */

export interface MenuNodeShape {
  key: string
  title: string
  icon?: string
  path?: string
  children?: MenuNodeShape[]
}

/**
 * 目录里只剩一个可见子项时，把那一项提到一级，不再套一层目录：
 * 插件被停用/未授权、或角色只分到一个页面，都会让目录退化成「一个组里孤零零一项」。
 * 多子项保持原样（分组本身有意义），空目录由调用方丢弃。
 * 已经是一级单项的合成节点（title 为空）不再处理，否则会被套成 top-top-*。
 */
export function promoteLoneGroups<T extends MenuNodeShape>(groups: T[]): T[] {
  return groups.map((g) => {
    const kids = g.children ?? []
    if (kids.length !== 1 || !g.title) return g
    return { ...g, key: `top-${g.key}`, title: '', icon: undefined, children: kids } as T
  })
}
