import { defineMock } from '@/framework/request/mock'
import type { ImportJob, ImportSourceMeta } from './api'

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()

const SOURCES: ImportSourceMeta[] = [
  {
    name: 'products',
    title: '商品导入',
    columns: [
      { key: 'title', label: '商品标题', required: true, example: '云南小粒咖啡' },
      { key: 'price', label: '价格(元)', required: true, example: '29.90' },
      { key: 'currency', label: '币种', required: false, example: 'CNY' },
      { key: 'stock', label: '库存', required: false, example: '100' },
      { key: 'sku_code', label: 'SKU编码(重复则更新)', required: false, example: 'SKU-001' },
    ],
  },
]

let jobs: ImportJob[] = [
  {
    id: '7450000000000000001',
    source: 'products',
    fileName: 'products-2025Q3.csv',
    totalRows: 120,
    okRows: 116,
    failRows: 4,
    status: 'done',
    durationMs: 860,
    errors: [
      { row: 23, key: 'title', message: '必填列为空' },
      { row: 57, key: 'price', message: '价格格式非法' },
    ],
    createdBy: 'admin',
    createdAt: ago(60 * 5),
  },
  {
    id: '7450000000000000002',
    source: 'products',
    fileName: 'products-suppliers.csv',
    totalRows: 30,
    okRows: 30,
    failRows: 0,
    status: 'done',
    durationMs: 210,
    createdBy: 'zhangwei',
    createdAt: ago(60 * 24),
  },
]

const paginate = <T,>(rows: T[], query: Record<string, unknown>) => {
  const page = Math.max(1, Number(query.page) || 1)
  const pageSize = Math.max(1, Number(query.pageSize) || 10)
  return { list: rows.slice((page - 1) * pageSize, page * pageSize), total: rows.length, page, pageSize }
}

defineMock([
  { method: 'GET', url: '/plugins/imports/sources', handler: () => SOURCES },
  {
    method: 'GET',
    url: '/plugins/imports',
    handler: ({ query }) => {
      let rows = jobs
      if (query.source) rows = rows.filter((j) => j.source === query.source)
      if (query.status) rows = rows.filter((j) => j.status === query.status)
      return paginate(rows, query)
    },
  },
  // mock 环境只给 CSV 文本桩；真实 XLSX 由后端生成。排在 /:id 之前，避免被通配路由吃掉。
  {
    method: 'GET',
    url: '/plugins/imports/template',
    handler: () => '\uFEFF商品标题(必填),价格(元)(必填),币种,库存,SKU编码(重复则更新)\r\n云南小粒咖啡,29.90,CNY,100,SKU-001\r\n',
  },
  {
    method: 'GET',
    url: '/plugins/imports/:id',
    handler: ({ params }) => jobs.find((j) => j.id === params.id) ?? { error: 'not found' },
  },
  {
    method: 'POST',
    url: '/plugins/imports',
    handler: () => {
      const job: ImportJob = {
        id: String(7450000000000010000n + BigInt(jobs.length + 1)),
        source: 'products',
        fileName: 'upload.csv',
        totalRows: 3,
        okRows: 2,
        failRows: 1,
        status: 'done',
        durationMs: 95,
        errors: [{ row: 2, key: 'title', message: '必填列为空' }],
        createdBy: 'admin',
        createdAt: new Date().toISOString(),
      }
      jobs = [job, ...jobs]
      return job
    },
  },
  {
    method: 'DELETE',
    url: '/plugins/imports/:id',
    handler: ({ params }) => {
      jobs = jobs.filter((j) => j.id !== params.id)
      return { ok: true }
    },
  },
  {
    method: 'GET',
    url: '/plugins/imports/:id/errors.csv',
    handler: ({ params }) => {
      const j = jobs.find((x) => x.id === params.id)
      return `row,key,message\n${(j?.errors ?? []).map((e) => `${e.row},${e.key},${e.message}`).join('\n')}`
    },
  },
])
