import { defineMock, genId, mockError } from '@/framework/request/mock'
import type { RecycleItem, RecycleType } from './api'

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

// 角色 / 字典 / 岗位没有接入回收站，这里放几条演示数据；还原时仅从回收站移除
let entries: Entry[] = [
  sample('role', '临时审批员', 1),
  sample('dict', 'sys_priority', 2),
  sample('post', '实习生', 4),
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
    url: '/recycle/:id',
    handler: ({ params }) => {
      if (!entries.some((e) => e.id === params.id)) mockError(404, 'Not found')
      entries = entries.filter((e) => e.id !== params.id)
      return null
    },
  },
])
