import { defineMock, genId, mockError } from '@/framework/request/mock'
import type { RecycleBatchResult, RecycleItem, RecycleType } from './api'

interface Entry extends RecycleItem {
  /** 被删除的原始数据，还原时交给对应模块 */
  row: unknown
}

const DAY = 86400000
const restorers = new Map<RecycleType, (row: unknown) => void>()

const sample = (type: RecycleType, name: string, daysAgo: number): Entry => ({
  id: genId(),
  type,
  name,
  deletedBy: 'admin',
  deletedAt: new Date(Date.now() - daysAgo * DAY).toISOString(),
  row: null,
})

// 类型清单与后端 recbin.Tables 一致：只有 user / notice 会进回收站。
let entries: Entry[] = [
  sample('user', '临时审批员', 1),
  sample('notice', '十一值班安排（已撤回）', 2),
  sample('notice', '旧版本发布说明', 5),
]

/** 各模块注册自己的还原方法（只有注册过的类型才会真正写回原模块） */
export function registerRestorer(type: RecycleType, fn: (row: unknown) => void) {
  restorers.set(type, fn)
}

/** 各模块删除时调用：把原始数据放进回收站 */
export function moveToRecycle(type: RecycleType, name: string, row: unknown) {
  entries = [
    { id: genId(), type, name, deletedBy: 'admin', deletedAt: new Date().toISOString(), row },
    ...entries,
  ]
}

const pub = ({ row: _row, ...rest }: Entry): RecycleItem => rest

defineMock([
  {
    method: 'GET',
    url: '/recycle',
    handler: ({ query }) => {
      const page = Math.max(1, Number(query.page) || 1)
      const pageSize = Math.max(1, Number(query.pageSize) || 10)
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      let rows = entries.slice()
      if (query.type) rows = rows.filter((e) => e.type === query.type)
      if (kw) rows = rows.filter((e) => e.name.toLowerCase().includes(kw))
      rows.sort((a, b) => b.deletedAt.localeCompare(a.deletedAt))
      return { list: rows.slice((page - 1) * pageSize, page * pageSize).map(pub), total: rows.length, page, pageSize }
    },
  },
  {
    method: 'POST',
    url: '/recycle/restore',
    handler: ({ body }) => {
      const ids = new Set<string>((body.ids ?? []).map(String))
      const res: RecycleBatchResult = {}
      for (const hit of entries.filter((e) => ids.has(e.id))) {
        restorers.get(hit.type)?.(hit.row)
        res.restored = (res.restored ?? 0) + 1
      }
      entries = entries.filter((e) => !ids.has(e.id))
      return res
    },
  },
  {
    method: 'POST',
    url: '/recycle/:id/restore',
    handler: ({ params }) => {
      const hit = entries.find((e) => e.id === params.id)
      if (!hit) mockError(404, 'Not found')
      restorers.get(hit.type)?.(hit.row)
      entries = entries.filter((e) => e.id !== hit.id)
      return null
    },
  },
  {
    method: 'DELETE',
    url: '/recycle',
    handler: ({ query }) => {
      const ids = new Set(String(query.ids ?? '').split(',').map((s) => s.trim()).filter(Boolean))
      if (ids.size === 0) mockError(400, 'Bad request')
      const res: RecycleBatchResult = { purged: entries.filter((e) => ids.has(e.id)).length }
      entries = entries.filter((e) => !ids.has(e.id))
      return res
    },
  },
  {
    method: 'DELETE',
    url: '/recycle/:id',
    handler: ({ params }) => {
      if (!entries.some((e) => e.id === params.id)) mockError(404, 'Not found')
      entries = entries.filter((e) => e.id !== params.id)
      return null
    },
  },
])
