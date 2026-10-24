import { createCrudMock, defineMock } from '@/framework/request/mock'
import type { TenantPackageRow } from './api'

const pid = (n: number) => `74000000000${String(10000000 + n)}`
const iso = (daysAgo: number) => new Date(Date.now() - daysAgo * 86400000).toISOString()

const M = [
  '1850000000000000001',
  '1850000000000000002',
  '1850000000000000003',
  '1850000000000000004',
  '1850000000000000005',
  '1850000000000000006',
]

const pkg = (
  n: number,
  name: string,
  menus: number,
  maxUsers: number,
  maxStorageMb: number,
  status: string,
  remark?: string,
): TenantPackageRow => ({
  id: pid(n),
  name,
  menuIds: M.slice(0, menus),
  maxUsers,
  // 角色上限：用户上限的 1/5（至少 3），不限制的套餐也不限角色
  maxRoles: maxUsers === 0 ? 0 : Math.max(3, Math.ceil(maxUsers / 5)),
  maxStorageMb,
  // AI 每日调用次数：体验版很少，大套餐更多，0 = 不限
  aiDailyQuota: maxUsers === 0 ? 0 : maxUsers <= 5 ? 20 : maxUsers <= 50 ? 200 : 1000,
  status,
  remark,
  createdAt: iso(200 - n * 12),
})

const seed: TenantPackageRow[] = [
  pkg(1, '体验版 Trial', 2, 5, 512, '1', '14 天试用，功能受限'),
  pkg(2, '基础版 Basic', 3, 20, 2048, '1', '小团队入门'),
  pkg(3, '标准版 Standard', 4, 50, 10240, '1'),
  pkg(4, '专业版 Professional', 5, 200, 51200, '1', '含全部系统管理功能'),
  pkg(5, '企业版 Enterprise', 6, 1000, 204800, '1', '含全部功能与优先支持'),
  pkg(6, '旗舰版 Ultimate', 6, 0, 0, '1', '用户数与存储不限'),
  pkg(7, '教育版 Education', 4, 500, 20480, '1', '面向学校与培训机构'),
  pkg(8, '非营利 NGO', 3, 30, 5120, '1'),
  pkg(9, '合作伙伴 Partner', 5, 100, 30720, '1', '渠道合作伙伴专用'),
  pkg(10, '定制版 Custom', 6, 300, 102400, '1', '按合同单独配置'),
  pkg(11, '旧版 Legacy 2024', 3, 10, 1024, '0', '已下架，仅存量租户使用'),
  pkg(12, '内测版 Beta', 4, 15, 2048, '0', '内部测试'),
]

// 必须先于 createCrudMock 注册，避免被 /tenant-packages/:id 抢先匹配
defineMock([
  {
    method: 'GET',
    url: '/tenant-packages/options',
    handler: ({ query }) => {
      const kw = String(query.keyword ?? '').trim().toLowerCase()
      return all()
        .filter((p) => p.status === '1' && (!kw || p.name.toLowerCase().includes(kw)))
        .map((p) => ({ id: p.id, name: p.name }))
    },
  },
])

const store = createCrudMock<TenantPackageRow>({
  base: '/tenant-packages',
  seed,
  searchFields: ['name', 'remark'],
  filterFields: ['status'],
})

function all() {
  return store.all()
}

/** 供租户 mock 计算有效额度 */
export const allPackages = () => all()
