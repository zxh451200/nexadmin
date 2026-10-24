import { defineMock, genId, mockError } from '@/framework/request/mock'
import type { SysDept, SysDeptInput } from './api'

const did = (n: number) => '186000000000000' + String(n).padStart(4, '0')
const T = '2025-02-10T09:30:00.000Z'

const d = (
  n: number,
  parent: number | null,
  name: string,
  sort: number,
  leaderName: string | undefined,
  phone: string,
  email: string,
  status: '1' | '0' = '1',
): SysDept => ({
  id: did(n),
  name,
  parentId: parent === null ? null : did(parent),
  sort,
  leaderName,
  leaderId: leaderName ? '19000000000000' + String(n).padStart(5, '0') : undefined,
  phone,
  email,
  status,
  createdAt: T,
  updatedAt: T,
})

let items: SysDept[] = [
  d(1, null, '总公司 Headquarters', 10, 'Admin', '010-88886666', 'hq@nexadmin.io'),
  d(2, 1, '研发中心', 10, '张伟', '010-88886601', 'rd@nexadmin.io'),
  d(3, 1, '市场中心', 20, 'Emily Chen', '010-88886602', 'marketing@nexadmin.io'),
  d(4, 1, '运营中心', 30, '李娜', '010-88886603', 'ops@nexadmin.io'),
  d(5, 1, '综合管理部', 40, '王芳', '010-88886604', 'admin@nexadmin.io'),
  d(6, 2, '前端组 Frontend', 10, '刘洋', '010-88886611', 'fe@nexadmin.io'),
  d(7, 2, '后端组 Backend', 20, 'Kevin Zhao', '010-88886612', 'be@nexadmin.io'),
  d(8, 2, '测试组 QA', 30, '陈静', '010-88886613', 'qa@nexadmin.io'),
  d(9, 3, '品牌部', 10, '周杰', '010-88886621', 'brand@nexadmin.io'),
  d(10, 3, '销售部 Sales', 20, 'Linda Wu', '010-88886622', 'sales@nexadmin.io', '0'),
  d(11, 4, '客服部', 10, '赵敏', '010-88886631', 'cs@nexadmin.io'),
  d(12, 5, '人力资源组 HR', 10, '孙丽', '010-88886641', 'hr@nexadmin.io'),
]

const bySort = (a: SysDept, b: SysDept) => a.sort - b.sort || a.id.localeCompare(b.id)

function buildTree(parentId: string | null): SysDept[] {
  return items
    .filter((x) => (x.parentId ?? null) === parentId)
    .sort(bySort)
    .map((x) => {
      const children = buildTree(x.id)
      return children.length ? { ...x, children } : { ...x }
    })
}

function descendantIds(id: string): string[] {
  const kids = items.filter((x) => x.parentId === id).map((x) => x.id)
  return kids.flatMap((k) => [k, ...descendantIds(k)])
}

// 模拟受限操作者（非 admin 令牌）在 scene=manage 下的裁剪：范围 = 研发中心(2)及其下级，保留到根的祖先并置只读。
function scopedTree(): SysDept[] {
  const inScope = new Set([did(2), ...descendantIds(did(2))])
  const trim = (nodes: SysDept[]): SysDept[] =>
    nodes.flatMap((n) => {
      const children = trim(n.children ?? [])
      if (!inScope.has(n.id) && children.length === 0) return []
      const out: SysDept = { ...n, children: children.length ? children : undefined }
      if (!inScope.has(n.id)) out.readonly = true
      return [out]
    })
  return trim(buildTree(null))
}

defineMock([
  {
    method: 'GET',
    url: '/depts/tree',
    handler: ({ headers, query }) =>
      query.scene === 'manage' && headers.authorization.split('.')[2] !== 'admin' ? scopedTree() : buildTree(null),
  },
  {
    method: 'PUT',
    url: '/depts/sort',
    handler: ({ body }) => {
      const parentId: string | null = body?.parentId || null
      const ids: string[] = body?.ids ?? []
      const group = items.filter((x) => (x.parentId ?? null) === parentId)
      if (group.length !== ids.length || !group.every((x) => ids.includes(x.id))) {
        mockError(409, '列表已变化，请刷新')
      }
      ids.forEach((id, i) => {
        const it = items.find((x) => x.id === id)
        if (it) it.sort = (i + 1) * 10
      })
      return null
    },
  },
  {
    method: 'GET',
    url: '/depts/:id',
    handler: ({ params, headers, query }) => {
      if (query.scene === 'manage' && headers.authorization.split('.')[2] !== 'admin') {
        const inScope = new Set([did(2), ...descendantIds(did(2))])
        if (!inScope.has(params.id)) mockError(403, '无权操作该部门（超出数据范围）')
      }
      return items.find((x) => x.id === params.id) ?? mockError(404, 'Not found')
    },
  },
  {
    method: 'POST',
    url: '/depts',
    handler: ({ body }) => {
      const input = body as SysDeptInput
      const now = new Date().toISOString()
      const row: SysDept = { ...input, parentId: input.parentId || null, id: genId(), createdAt: now, updatedAt: now }
      items = [...items, row]
      return row
    },
  },
  {
    method: 'PUT',
    url: '/depts/:id',
    handler: ({ params, body }) => {
      const idx = items.findIndex((x) => x.id === params.id)
      if (idx < 0) mockError(404, 'Not found')
      const parentId: string | null = body?.parentId || null
      if (parentId && (parentId === params.id || descendantIds(params.id).includes(parentId))) {
        mockError(400, '上级部门不能是自己或其下级')
      }
      const prev = items[idx]
      // 负责人变更后名称由后端回填，mock 里清空
      const leaderName = body?.leaderId === prev.leaderId ? prev.leaderName : undefined
      items[idx] = { ...prev, ...body, leaderName, parentId, id: params.id, updatedAt: new Date().toISOString() }
      return items[idx]
    },
  },
  {
    method: 'DELETE',
    url: '/depts/:id',
    handler: ({ params }) => {
      const gone = new Set([params.id, ...descendantIds(params.id)])
      items = items.filter((x) => !gone.has(x.id))
      return null
    },
  },
])
