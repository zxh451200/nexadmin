import { defineMock } from '@/framework/request/mock'
import type { ImportJob, ImportSourceMeta } from './api'

const ago = (min: number) => new Date(Date.now() - min * 60_000).toISOString()

const SOURCES: ImportSourceMeta[] = [
  {
    name: 'crm_customers',
    title: '客户导入',
    columns: [
      { key: 'name', label: '客户名称', required: true, example: '张三' },
      { key: 'phone', label: '手机号', required: false, example: '13800138000' },
      { key: 'email', label: '邮箱', required: false, example: 'zhangsan@example.com' },
      { key: 'level', label: '客户等级', required: false, example: 'VIP' },
      { key: 'owner', label: '负责人', required: false, example: '王五' },
      { key: 'remark', label: '备注', required: false, example: '' },
    ],
  },
  {
    name: 'dict_data',
    title: '字典数据导入',
    columns: [
      { key: 'type_code', label: '字典类型编码', required: true, example: 'sys_common_status' },
      { key: 'label', label: '标签', required: true, example: '成功' },
      { key: 'value', label: '值', required: true, example: '1' },
      { key: 'tag', label: '标签样式', required: false, example: 'success' },
      { key: 'sort', label: '排序', required: false, example: '10' },
      { key: 'status', label: '状态(1启用 0停用)', required: false, example: '1' },
      { key: 'remark', label: '备注', required: false, example: '' },
    ],
  },
]

let jobs: ImportJob[] = [
  {
    id: '7450000000000000001',
    source: 'crm_customers',
    fileName: 'customers-2025Q3.csv',
    totalRows: 120,
    okRows: 116,
    failRows: 4,
    status: 'done',
    durationMs: 860,
    errors: [
      { row: 23, key: 'name', message: '必填列为空' },
      { row: 57, key: 'email', message: '邮箱格式非法' },
    ],
    createdBy: 'admin',
    createdAt: ago(60 * 5),
  },
  {
    id: '7450000000000000002',
    source: 'crm_customers',
    fileName: 'customers-hotels.csv',
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
        source: 'crm_customers',
        fileName: 'upload.csv',
        totalRows: 3,
        okRows: 2,
        failRows: 1,
        status: 'done',
        durationMs: 95,
        errors: [{ row: 2, key: 'name', message: '必填列为空' }],
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
