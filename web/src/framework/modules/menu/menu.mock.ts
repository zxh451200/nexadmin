import { defineMock, genId, mockError } from '@/framework/request/mock'
import type { MenuType, SysMenu, SysMenuInput } from './api'

let seq = 0
const nid = () => '1850000000000' + String(++seq).padStart(6, '0')
const NOW = '2025-01-15T08:00:00.000Z'

let sortSeq = 0
const base = (p: Partial<SysMenu> & { type: MenuType; title: string; parentId: string | null }): SysMenu => ({
  id: nid(),
  sort: (++sortSeq % 100) * 10,
  keepAlive: false,
  hidden: false,
  external: false,
  status: '1',
  createdAt: NOW,
  updatedAt: NOW,
  ...p,
})

const items: SysMenu[] = []
const add = (m: SysMenu) => {
  items.push(m)
  return m.id
}
const dir = (title: string, icon: string, path: string, sort: number) =>
  add(base({ parentId: null, type: 'dir', title, icon, path, sort }))
const menu = (
  parentId: string | null,
  title: string,
  path: string,
  component: string,
  perm: string,
  icon: string | undefined,
  sort: number,
  extra: Partial<SysMenu> = {},
) => add(base({ parentId, type: 'menu', title, path, component, perm, icon, sort, keepAlive: true, ...extra }))
const btns = (parentId: string, prefix: string, actions: string[]) =>
  actions.forEach((a, i) =>
    add(base({ parentId, type: 'button', title: `sysMenu.btn.${a}`, perm: `${prefix}:${a}`, sort: (i + 1) * 10 })),
  )

const CRUD = ['create', 'update', 'delete']

// 工作台
menu(null, 'dashboard.menu', '/dashboard', 'dashboard/DashboardPage', 'dashboard:view', 'LayoutDashboard', 10, {
  keepAlive: false,
})

// 应用中心（顶级，无需权限）
menu(null, 'sso.portal.menu', '/portal', 'sso/PortalPage', '', 'LayoutGrid', 15, { keepAlive: false })

// 系统管理
const system = dir('sysMenu.dir.system', 'Settings', '/system', 20)
const user = menu(system, 'user.menu', '/system/user', 'user/UserPage', 'user:list', 'Users', 10)
btns(user, 'user', [...CRUD, 'export', 'import', 'resetPwd', 'unlock'])
const role = menu(system, 'role.menu', '/system/role', 'role/RolePage', 'role:list', 'ShieldCheck', 20)
btns(role, 'role', [...CRUD, 'assign'])
const sysMenu = menu(system, 'sysMenu.menu', '/system/menu', 'menu/MenuPage', 'menu:list', 'ListTree', 30)
btns(sysMenu, 'menu', CRUD)
const dept = menu(system, 'dept.menu', '/system/dept', 'dept/DeptPage', 'dept:list', 'Network', 40)
btns(dept, 'dept', CRUD)
const post = menu(system, 'post.menu', '/system/post', 'post/PostPage', 'post:list', 'BriefcaseBusiness', 50)
btns(post, 'post', CRUD)
const dict = menu(system, 'dict.menu', '/system/dict', 'dict/DictPage', 'dict:list', 'BookA', 60)
btns(dict, 'dict', CRUD)
const config = menu(system, 'sysConfig.menu', '/system/config', 'config/ConfigPage', 'config:list', 'Settings', 70)
btns(config, 'config', CRUD)
const settings = menu(system, 'settings.menu', '/system/settings', 'settings/SettingsPage', 'settings:list', 'SlidersHorizontal', 80)
btns(settings, 'settings', ['update'])
const recycle = menu(system, 'recycle.menu', '/system/recycle', 'recycle/RecyclePage', 'recycle:list', 'Trash2', 90)
btns(recycle, 'recycle', ['restore', 'delete'])
const pluginMgr = menu(system, 'pluginMgr.menu', '/system/plugin', 'plugin/PluginPage', 'plugin:list', 'Puzzle', 100)
btns(pluginMgr, 'plugin', ['license'])
const ssoApps = menu(system, 'sso.menu', '/system/sso-apps', 'sso/SsoAppsPage', 'sso:list', 'AppWindow', 110)
add(base({ parentId: ssoApps, type: 'button', title: 'sso.btn.edit', perm: 'sso:edit', sort: 10 }))
const openApi = menu(system, 'openapi.menu', '/system/openapi', 'openapi/OpenApiPage', 'openapi:list', 'Webhook', 120)
add(base({ parentId: openApi, type: 'button', title: 'openapi.btn.edit', perm: 'openapi:edit', sort: 10 }))
const backup = menu(system, 'backup.menu', '/system/backup', 'backup/BackupPage', 'backup:list', 'DatabaseBackup', 140, { keepAlive: false })
add(base({ parentId: backup, type: 'button', title: 'backup.btn.edit', perm: 'backup:edit', sort: 10 }))
add(base({ parentId: backup, type: 'button', title: 'backup.btn.download', perm: 'backup:download', sort: 20 }))

// 内容管理
const content = dir('sysMenu.dir.content', 'FileText', '/content', 30)
const notice = menu(content, 'notice.menu', '/content/notice', 'notice/NoticePage', 'notice:list', 'Megaphone', 10)
btns(notice, 'notice', [...CRUD, 'publish', 'platform'])
const message = menu(content, 'message.menu', '/message', 'message/MessagePage', 'message:list', 'Bell', 20)
btns(message, 'message', ['delete'])
const file = menu(content, 'fileMgr.menu', '/content/file', 'file/FilePage', 'file:list', 'FolderOpen', 30)
btns(file, 'file', ['upload', 'delete'])

// 系统监控
const monitor = dir('sysMenu.dir.monitor', 'Activity', '/monitor', 40)
const operLog = menu(monitor, 'operLog.menu', '/monitor/oper-log', 'operLog/OperLogPage', 'operLog:list', 'ScrollText', 10)
btns(operLog, 'operLog', ['delete', 'export'])
const loginLog = menu(monitor, 'loginLog.menu', '/monitor/login-log', 'loginLog/LoginLogPage', 'loginLog:list', 'LogIn', 20)
btns(loginLog, 'loginLog', ['delete', 'export'])
const online = menu(monitor, 'online.menu', '/monitor/online', 'online/OnlinePage', 'online:list', 'MonitorSmartphone', 30, {
  keepAlive: false,
})
btns(online, 'online', ['forceLogout'])
menu(monitor, 'serverMonitor.menu', '/monitor/server', 'serverMonitor/ServerMonitorPage', 'serverMonitor:list', 'Cpu', 40)
menu(monitor, 'cacheMonitor.menu', '/monitor/cache', 'cacheMonitor/CacheMonitorPage', 'cacheMonitor:list', 'Database', 50)
menu(monitor, 'apiMonitor.menu', '/monitor/api', 'apiMonitor/ApiMonitorPage', 'apiMonitor:list', 'Gauge', 60)

// 租户管理
const tenantDir = dir('sysMenu.dir.tenant', 'Building2', '/tenant', 50)
const tenant = menu(tenantDir, 'tenant.menu', '/tenant/list', 'tenant/TenantPage', 'tenant:list', 'Building2', 10)
btns(tenant, 'tenant', CRUD)
const tenantPkg = menu(tenantDir, 'tenantPkg.menu', '/tenant/package', 'tenantPkg/TenantPkgPage', 'tenantPkg:list', 'Package', 20)
btns(tenantPkg, 'tenantPkg', CRUD)

// 个人中心（隐藏菜单）
menu(null, 'profile.menu', '/profile', 'profile/ProfilePage', 'profile:view', 'User', 60, {
  hidden: true,
  keepAlive: false,
})

// 关于（版本与开源许可）
menu(null, 'about.menu', '/about', 'about/AboutPage', 'about:view', 'Info', 65, { keepAlive: false })

// 外链示例
menu(null, 'sysMenu.docs', 'https://example.com/docs', '', 'docs:view', 'BookOpen', 70, {
  external: true,
  status: '0',
  keepAlive: false,
})

// 导出中心（顶级，登录即可；管理员「全部任务」用按钮权限 job:all）
const jobsMenu = menu(null, 'jobs.menu', '/jobs', 'exportcenter/ExportCenterPage', '', 'Download', 16, { keepAlive: false })
add(base({ parentId: jobsMenu, type: 'button', title: 'jobs.btn.all', perm: 'job:all', sort: 10 }))

// 发送日志（系统监控下；追加在末尾，不挪动已有出厂 ID）
const sendlog = menu(monitor, 'sendlog.menu', '/monitor/send-log', 'sendlog/SendLogPage', 'sendlog:list', 'Send', 70)
btns(sendlog, 'sendlog', ['delete'])

const bySort = (a: SysMenu, b: SysMenu) => a.sort - b.sort || a.id.localeCompare(b.id)

function buildTree(list: SysMenu[], parentId: string | null): SysMenu[] {
  return list
    .filter((m) => (m.parentId ?? null) === parentId)
    .sort(bySort)
    .map((m) => {
      const children = buildTree(list, m.id)
      return children.length ? { ...m, children } : { ...m }
    })
}

function descendantIds(id: string): string[] {
  const kids = items.filter((m) => m.parentId === id).map((m) => m.id)
  return kids.flatMap((k) => [k, ...descendantIds(k)])
}

// 固定路径必须先于 /:id 注册
defineMock([
  { method: 'GET', url: '/menus/tree', handler: () => buildTree(items, null) },
  // 侧边栏用：只要目录和菜单，按钮不返回（权限过滤在前端按用户权限做）
  { method: 'GET', url: '/menus/mine', handler: () => buildTree(items.filter((m) => m.type !== 'button'), null) },
  {
    method: 'PUT',
    url: '/menus/sort',
    handler: ({ body }) => {
      const parentId: string | null = body?.parentId || null
      const ids: string[] = body?.ids ?? []
      const group = items.filter((m) => (m.parentId ?? null) === parentId)
      if (group.length !== ids.length || !group.every((m) => ids.includes(m.id))) {
        mockError(409, '列表已变化，请刷新')
      }
      ids.forEach((id, i) => {
        const it = items.find((m) => m.id === id)
        if (it) it.sort = (i + 1) * 10
      })
      return null
    },
  },
  {
    method: 'PUT',
    url: '/menus/:id/move',
    handler: ({ params, body }) => {
      const item = items.find((m) => m.id === params.id)
      if (!item) mockError(404, 'Not found')
      const parentId: string | null = body?.parentId || null
      if (parentId === params.id || (parentId && descendantIds(params.id).includes(parentId))) mockError(400, '上级菜单不能是自己或其下级')
      const parent = parentId ? items.find((m) => m.id === parentId) : undefined
      if (parentId && !parent) mockError(400, '上级菜单不存在')
      if (parent?.type === 'button') mockError(400, '按钮下不能再建子菜单')
      const siblings = items.filter((m) => (m.parentId ?? null) === parentId && m.id !== params.id)
      item.parentId = parentId
      item.sort = (Math.max(0, ...siblings.map((m) => m.sort)) || 0) + 10
      item.updatedAt = new Date().toISOString()
      return null
    },
  },
  {
    method: 'GET',
    url: '/menus/:id',
    handler: ({ params }) => items.find((m) => m.id === params.id) ?? mockError(404, 'Not found'),
  },
  {
    method: 'POST',
    url: '/menus',
    handler: ({ body }) => {
      const input = body as SysMenuInput
      const now = new Date().toISOString()
      const row: SysMenu = { ...input, parentId: input.parentId || null, id: genId(), createdAt: now, updatedAt: now }
      items.push(row)
      return row
    },
  },
  {
    method: 'PUT',
    url: '/menus/:id',
    handler: ({ params, body }) => {
      const idx = items.findIndex((m) => m.id === params.id)
      if (idx < 0) mockError(404, 'Not found')
      const parentId: string | null = body?.parentId || null
      if (parentId && (parentId === params.id || descendantIds(params.id).includes(parentId))) {
        mockError(400, '上级菜单不能是自己或其下级')
      }
      items[idx] = { ...items[idx], ...body, parentId, id: params.id, updatedAt: new Date().toISOString() }
      return items[idx]
    },
  },
  {
    method: 'DELETE',
    url: '/menus/:id',
    handler: ({ params }) => {
      const gone = new Set([params.id, ...descendantIds(params.id)])
      for (let i = items.length - 1; i >= 0; i--) if (gone.has(items[i].id)) items.splice(i, 1)
      return null
    },
  },
])
