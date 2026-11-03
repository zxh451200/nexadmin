import { createCrudMock, defineMock, type CrudStore } from '@/framework/request/mock'
import { dictSeed } from '@/framework/dict/seed'
import type { DictDataRow, DictTypeRow, TagVariant } from './api'

const typeId = (i: number) => `71000000000000${String(10000 + i)}`
const dataId = (n: number) => `72000000000${String(10000000 + n)}`
const day = 86400000
const iso = (daysAgo: number) => new Date(Date.now() - daysAgo * day).toISOString()

const remarks: Record<string, string> = {
  sys_status: '通用启用/停用状态',
  sys_gender: '用户性别',
  yes_no: '布尔类取值',
  notice_type: '通知公告分类',
  tenant_status: '租户生命周期状态',
}

// 后端 builtin 由 seedTypes 派生；mock 里给两个内置类型打标
const builtinCodes = new Set(['sys_status', 'sys_gender'])

const types: DictTypeRow[] = []
const data: DictDataRow[] = []
let counter = 0

Object.entries(dictSeed).forEach(([code, def], i) => {
  const id = typeId(i)
  types.push({
    id,
    name: def.name,
    code,
    sort: (i + 1) * 10,
    status: i === 12 ? '0' : '1',
    remark: remarks[code],
    createdAt: iso(60 - i * 3),
    builtin: builtinCodes.has(code),
  })
  def.items.forEach((item, j) => {
    counter += 1
    data.push({
      id: dataId(counter),
      typeId: id,
      label: item.label,
      value: item.value,
      tag: (item.tag ?? 'default') as TagVariant,
      sort: (j + 1) * 10,
      status: '1',
      remark: item.labelEn,
    })
  })
})

// 导出口径与后端一致：不分页、复用列表那套 keyword / status / typeId 过滤、按 sort 排序、返回裸数组。
// 必须抢在 createCrudMock 注册 GET /dict/types/:id 之前 defineMock：mock 按注册顺序取第一条命中规则，
// 晚一步 'export' 就被当成行 ID 匹配到 /:id 上，返回 404。
// 取数走 store.all()（createCrudMock 内部的当前数据），批量删掉的行不会又「导出回来了」。
const stores: { types: CrudStore<DictTypeRow> | null; data: CrudStore<DictDataRow> | null } = { types: null, data: null }

/** 导出 handler 取数用：store 在 defineMock 之后才被 createCrudMock 填进来，取不到就是接线错了，大声失败而不是返回空表 */
function allOf<T extends { id: string }>(store: CrudStore<T> | null): T[] {
  if (!store) throw new Error('mock store 尚未就绪：导出规则必须晚于 createCrudMock 被调用，但取数要等它填好')
  return store.all()
}

const keywordOf = (query: Record<string, unknown>) => String(query.keyword ?? '').trim().toLowerCase()
const statusOf = (query: Record<string, unknown>) => String(query.status ?? '').split(',').filter(Boolean)

defineMock([
  {
    method: 'GET',
    url: '/dict/types/export',
    handler: ({ query }): DictTypeRow[] => {
      const kw = keywordOf(query)
      const statuses = statusOf(query)
      return allOf(stores.types)
        .filter((r) => !kw || [r.name, r.code].some((v) => v.toLowerCase().includes(kw)))
        .filter((r) => statuses.length === 0 || statuses.includes(r.status))
        .sort((a, b) => a.sort - b.sort)
    },
  },
  {
    method: 'GET',
    url: '/dict/data/export',
    handler: ({ query }): DictDataRow[] => {
      const kw = keywordOf(query)
      const statuses = statusOf(query)
      const typeId = String(query.typeId ?? '')
      return allOf(stores.data)
        .filter((r) => !typeId || r.typeId === typeId)
        .filter((r) => !kw || [r.label, r.value].some((v) => v.toLowerCase().includes(kw)))
        .filter((r) => statuses.length === 0 || statuses.includes(r.status))
        .sort((a, b) => a.sort - b.sort)
    },
  },
])

stores.types = createCrudMock<DictTypeRow>({
  base: '/dict/types',
  seed: types,
  searchFields: ['name', 'code'],
  filterFields: ['status'],
  sortField: 'sort',
})

stores.data = createCrudMock<DictDataRow>({
  base: '/dict/data',
  seed: data,
  searchFields: ['label', 'value'],
  filterFields: ['typeId', 'status'],
  sortField: 'sort',
})
