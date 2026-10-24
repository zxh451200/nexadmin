import { createCrudMock, defineMock, mockError } from '@/framework/request/mock'
import type { Role } from './api'

const ids = {
  admin: '2100000000000000001',
  operator: '2100000000000000002',
  auditor: '2100000000000000003',
  guest: '2100000000000000004',
  editor: '2100000000000000005',
  secadmin: '2100000000000000006',
}

const at = (d: string) => `${d}T08:30:00.000Z`

export const roleSeed: Role[] = [
  {
    id: ids.admin,
    name: '超级管理员',
    code: 'admin',
    sort: 10,
    status: '1',
    dataScope: 'all',
    deptIds: [],
    remark: '拥有系统全部权限，不可删除',
    builtin: true,
    assignable: false,
    createdAt: at('2026-01-05'),
    updatedAt: at('2026-01-05'),
  },
  {
    id: ids.operator,
    name: '运营',
    code: 'operator',
    sort: 20,
    status: '1',
    dataScope: 'deptAndChild',
    deptIds: [],
    remark: '负责内容与公告运营',
    assignable: true,
    createdAt: at('2026-01-12'),
    updatedAt: at('2026-03-02'),
  },
  {
    id: ids.auditor,
    name: '审计 Auditor',
    code: 'auditor',
    sort: 30,
    status: '1',
    dataScope: 'custom',
    deptIds: ['1860000000000000008', '1860000000000000004'],
    remark: '只读查看日志与在线用户',
    assignable: true,
    createdAt: at('2026-01-20'),
    updatedAt: at('2026-04-18'),
  },
  {
    id: ids.editor,
    name: '内容编辑 Editor',
    code: 'editor',
    sort: 40,
    status: '0',
    dataScope: 'self',
    deptIds: [],
    remark: '',
    assignable: true,
    createdAt: at('2026-02-08'),
    updatedAt: at('2026-02-08'),
  },
  {
    id: ids.guest,
    name: '访客',
    code: 'guest',
    sort: 50,
    status: '1',
    dataScope: 'self',
    deptIds: [],
    remark: '仅可浏览工作台',
    assignable: true,
    createdAt: at('2026-02-15'),
    updatedAt: at('2026-02-15'),
  },
  {
    id: ids.secadmin,
    name: '安全管理员（不可分配）',
    code: 'secadmin',
    sort: 60,
    status: '1',
    dataScope: 'all',
    deptIds: [],
    remark: '演示：不允许被下级管理员分配的角色',
    assignable: false,
    createdAt: at('2026-02-20'),
    updatedAt: at('2026-02-20'),
  },
]

export const roleIds = ids

// 角色 -> 菜单/按钮权限 id 集合（id 来自 B2 的 /menus/tree，种子里先为空）
const roleMenus = new Map<string, string[]>(roleSeed.map((r) => [r.id, []]))

// 固定路径必须先于 createCrudMock 的 /roles/:id 注册
defineMock([
  {
    method: 'GET',
    url: '/roles/options',
    // 超管（mock 里的 admin）返回全部；其他人只返回可分配的（后端另外还校验权限与数据范围不超出操作者）
    handler: ({ headers, query }) => {
      // 只有「分配角色」场景（用户表单，scene=assign）才按 assignable 过滤；公告目标、SSO/开放平台可见角色等仍返回全部
      const isSuper = headers.authorization.split('.')[2] === 'admin' || query.scene !== 'assign'
      return store
        .all()
        .filter((r) => r.status === '1' && (isSuper || (r.assignable && !r.builtin)))
        .sort((a, b) => a.sort - b.sort)
        .map((r) => ({ id: r.id, name: r.name }))
    },
  },
  {
    method: 'GET',
    url: '/roles/:id/menus',
    handler: ({ params }) => {
      if (!store.get(params.id)) mockError(404, 'Not found')
      return roleMenus.get(params.id) ?? []
    },
  },
  {
    method: 'PUT',
    url: '/roles/:id/menus',
    handler: ({ params, body }) => {
      if (!store.get(params.id)) mockError(404, 'Not found')
      roleMenus.set(params.id, Array.isArray(body?.menuIds) ? body.menuIds.map(String) : [])
      return null
    },
  },
])

const store = createCrudMock<Role>({
  base: '/roles',
  seed: roleSeed,
  searchFields: ['name', 'code'],
  filterFields: ['status'],
  sortField: 'sort',
})
