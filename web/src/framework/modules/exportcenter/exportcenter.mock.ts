import { defineMock, genId, mockError } from '@/framework/request/mock'
import type { Job } from './api'

// 与后端 /jobs 同一契约。mock 用定时器模拟：排队 → 执行（进度递增）→ 完成；取消会停掉定时器。
const MOCK_OWNER = { id: '1', name: 'admin' }
const TICK_MS = 600

interface Entry extends Job {
  /** 结果文件内容（mock 阶段是 CSV 文本） */
  content: string
  /** 失败重试时重新开始的参数 */
  total: number
  timer?: ReturnType<typeof setInterval>
}

const pub = ({ content: _c, total: _t, timer: _timer, ...rest }: Entry): Job => rest

let entries: Entry[] = []

function stop(e: Entry) {
  if (e.timer) clearInterval(e.timer)
  e.timer = undefined
}

function run(e: Entry) {
  stop(e)
  e.status = 'queued'
  e.progress = 0
  e.message = ''
  e.error = ''
  e.startedAt = null
  e.finishedAt = null
  e.downloadable = false
  setTimeout(() => {
    if (e.status !== 'queued') return
    e.status = 'running'
    e.startedAt = new Date().toISOString()
    const step = Math.max(6, Math.round(100 / Math.max(3, Math.min(20, e.total / 50 + 4))))
    e.timer = setInterval(() => {
      e.progress = Math.min(100, e.progress + step)
      e.message = `${Math.round((e.progress / 100) * e.total)} / ${e.total}`
      if (e.progress >= 100) {
        stop(e)
        e.status = 'success'
        e.message = ''
        e.finishedAt = new Date().toISOString()
        e.fileSize = new Blob([e.content]).size
        e.downloadable = true
      }
    }, TICK_MS)
  }, 400)
}

/** 其它模块的 mock 用它创建一个模拟异步任务（content 是下载到的文件内容） */
export function createMockJob(type: string, fileName: string, total: number, content: string): Job {
  const e: Entry = {
    id: genId(), type, status: 'queued', progress: 0, message: '', error: '', ownerId: MOCK_OWNER.id, ownerName: MOCK_OWNER.name,
    fileName, fileSize: 0, downloadable: false, createdAt: new Date().toISOString(), startedAt: null, finishedAt: null,
    content, total,
  }
  entries = [e, ...entries]
  run(e)
  return pub(e)
}

const ago = (min: number) => new Date(Date.now() - min * 60000).toISOString()
const doneSeed = (type: string, fileName: string, minAgo: number): Entry => ({
  id: genId(), type, status: 'success', progress: 100, message: '', error: '', ownerId: MOCK_OWNER.id, ownerName: MOCK_OWNER.name,
  fileName, fileSize: 2048, downloadable: true, createdAt: ago(minAgo), startedAt: ago(minAgo - 0.1), finishedAt: ago(minAgo - 0.5),
  content: '\uFEFF账号,姓名\r\ndemo,演示\r\n', total: 100,
})
entries = [
  doneSeed('user.export', 'users-20260116.csv', 60 * 26),
  {
    ...doneSeed('user.export', 'users-20260115.csv', 60 * 50),
    status: 'failed', progress: 40, error: '没有符合条件的数据', downloadable: false, fileName: '', fileSize: 0,
  },
]

function find(id: string): Entry {
  const e = entries.find((x) => x.id === id)
  if (!e) mockError(404, '任务不存在')
  return e
}

defineMock([
  {
    method: 'GET',
    url: '/jobs',
    handler: ({ query }) => {
      const page = Math.max(1, Number(query.page) || 1)
      const pageSize = Math.max(1, Number(query.pageSize) || 10)
      let rows = entries.slice()
      if (query.type) rows = rows.filter((e) => e.type === query.type)
      if (query.status === 'active') rows = rows.filter((e) => e.status === 'queued' || e.status === 'running')
      else if (query.status) rows = rows.filter((e) => e.status === query.status)
      rows.sort((a, b) => b.createdAt.localeCompare(a.createdAt))
      return { list: rows.slice((page - 1) * pageSize, page * pageSize).map(pub), total: rows.length, page, pageSize }
    },
  },
  { method: 'GET', url: '/jobs/:id', handler: ({ params }) => pub(find(params.id)) },
  {
    method: 'POST',
    url: '/jobs/:id/cancel',
    handler: ({ params }) => {
      const e = find(params.id)
      if (e.status !== 'queued' && e.status !== 'running') mockError(409, '当前状态不允许该操作')
      stop(e)
      e.status = 'canceled'
      e.finishedAt = new Date().toISOString()
      return null
    },
  },
  {
    method: 'POST',
    url: '/jobs/:id/retry',
    handler: ({ params }) => {
      const e = find(params.id)
      if (e.status !== 'failed') mockError(409, '当前状态不允许该操作')
      run(e)
      return null
    },
  },
  {
    method: 'DELETE',
    url: '/jobs/:id',
    handler: ({ params }) => {
      const e = find(params.id)
      if (e.status === 'queued' || e.status === 'running') mockError(409, '当前状态不允许该操作')
      entries = entries.filter((x) => x !== e)
      return null
    },
  },
  {
    method: 'GET',
    url: '/jobs/:id/download',
    handler: ({ params }) => {
      const e = find(params.id)
      if (!e.downloadable) mockError(409, '当前状态不允许该操作')
      return e.content
    },
  },
])
