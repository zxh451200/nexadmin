import { createCrudMock } from '@/framework/request/mock'
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

createCrudMock<DictTypeRow>({
  base: '/dict/types',
  seed: types,
  searchFields: ['name', 'code'],
  filterFields: ['status'],
  sortField: 'sort',
})

createCrudMock<DictDataRow>({
  base: '/dict/data',
  seed: data,
  searchFields: ['label', 'value'],
  filterFields: ['typeId', 'status'],
  sortField: 'sort',
})
