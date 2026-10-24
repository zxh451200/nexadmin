import { defineMock, genId, mockError } from '@/framework/request/mock'
import type { SysPost, SysPostInput } from './api'

const pid = (n: number) => '187000000000000' + String(n).padStart(4, '0')
const did = (n: number) => '186000000000000' + String(n).padStart(4, '0')
const T = '2025-03-01T10:00:00.000Z'

const p = (
  n: number,
  name: string,
  code: string,
  dept: number | undefined,
  status: '1' | '0' = '1',
  remark?: string,
): SysPost => ({
  id: pid(n),
  name,
  code,
  deptId: dept ? did(dept) : undefined,
  sort: n * 10,
  status,
  remark,
  createdAt: T,
  updatedAt: T,
})

let items: SysPost[] = [
  p(1, '董事长', 'chairman', 1, '1', '公司最高负责人'),
  p(2, '技术总监 CTO', 'cto', 2, '1', '统筹研发中心技术路线'),
  p(3, '前端工程师', 'frontend', 6, '1', 'React / TypeScript'),
  p(4, '后端工程师 Backend Engineer', 'backend', 7, '1', 'Go / PostgreSQL'),
  p(5, '测试工程师', 'qa', 8),
  p(6, '市场经理', 'marketing_manager', 3, '1', '负责品牌与市场活动'),
  p(7, '客服专员 Support', 'support', 11, '1'),
  p(8, '人事专员', 'hr', 12, '0', '暂停招聘，岗位冻结'),
]

const bySort = (a: SysPost, b: SysPost) => a.sort - b.sort || a.id.localeCompare(b.id)

// 固定路径必须先于 /:id 注册
defineMock([
  { method: 'GET', url: '/posts/options', handler: () => items.filter((x) => x.status === '1').sort(bySort).map((x) => ({ id: x.id, name: x.name })) },
  {
    method: 'PUT',
    url: '/posts/sort',
    handler: ({ body }) => {
      const ids: string[] = body?.ids ?? []
      if (ids.length !== items.length || !items.every((x) => ids.includes(x.id))) {
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
    url: '/posts',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      return items
        .filter((x) => !kw || x.name.toLowerCase().includes(kw) || x.code.toLowerCase().includes(kw))
        .sort(bySort)
    },
  },
  {
    method: 'GET',
    url: '/posts/:id',
    handler: ({ params }) => items.find((x) => x.id === params.id) ?? mockError(404, 'Not found'),
  },
  {
    method: 'POST',
    url: '/posts',
    handler: ({ body }) => {
      const input = body as SysPostInput
      if (items.some((x) => x.code === input.code)) mockError(400, '岗位编码已存在')
      const now = new Date().toISOString()
      const row: SysPost = { ...input, id: genId(), createdAt: now, updatedAt: now }
      items = [...items, row]
      return row
    },
  },
  {
    method: 'PUT',
    url: '/posts/:id',
    handler: ({ params, body }) => {
      const idx = items.findIndex((x) => x.id === params.id)
      if (idx < 0) mockError(404, 'Not found')
      if (body?.code && items.some((x) => x.code === body.code && x.id !== params.id)) mockError(400, '岗位编码已存在')
      items[idx] = { ...items[idx], ...body, id: params.id, updatedAt: new Date().toISOString() }
      return items[idx]
    },
  },
  {
    method: 'DELETE',
    url: '/posts/:id',
    handler: ({ params }) => {
      items = items.filter((x) => x.id !== params.id)
      return null
    },
  },
])
