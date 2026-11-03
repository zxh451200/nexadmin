import { defineMock, mockError } from '@/framework/request/mock'
import type { RegionPreviewResult, RegionStatus, RegionSyncResult, RegionSyncStatus, RegionVersion, RegionVersionDetail } from './api'

/** mock 是后端契约的第二份实现：版本历史的规则要和 modules/region 一致——
 * 覆盖前把当时生效的那份留成一个版本，摘要没变不重复存；列表不含整棵树正文，正文只在单版本详情里给。 */

const PROVINCES = ['北京市', '天津市', '河北省', '山西省', '内蒙古自治区', '辽宁省', '吉林省', '黑龙江省']

// 内置数据没有「行」，所以它不进历史版本表（与 migrations/framework 里 region_002 的说明同一套说法）。
const treeOf = (mark: string) => JSON.stringify(PROVINCES.map((name, i) => ({ code: `${(i + 1) * 10}0000`, name: name + mark })))

const builtin: RegionStatus = {
  builtin: true,
  sourceUrl: '',
  dataDate: '',
  license: '',
  sha256: '',
  provinces: 34,
  prefectures: 333,
  counties: 2874,
  syncedBy: '',
  syncedAt: '',
  defaultSource: 'https://raw.githubusercontent.com/mumuy/data_location/master/list.json',
}

let status: RegionStatus = { ...builtin }

/** 当前生效的那一份快照；null = 还在用内置数据（相当于库里没有行） */
interface Current {
  payload: string
  sha256: string
  dataDate: string
  sourceUrl: string
  createdBy: string
  provinces: number
  prefectures: number
  counties: number
}
let current: Current | null = null
let store: RegionVersionDetail[] = []
let seq = 0
let shaSeq = 0

const nowISO = () => new Date().toISOString()
const nextSha = () => `mock${String(++shaSeq).padStart(60, '0')}`

/** 覆盖前留档：与 service.go 的 archiveCurrent 同一组规则（没有旧内容 / 内容没变都不写） */
function archive(nextShaValue: string) {
  if (!current || current.sha256 === nextShaValue) return
  seq += 1
  store = [{ ...current, version: seq, createdAt: nowISO() }, ...store].slice(0, 50)
}

function applySnapshot(source: string, mark: string) {
  const sha = nextSha()
  archive(sha)
  const at = nowISO()
  current = {
    payload: treeOf(mark),
    sha256: sha,
    dataDate: at.slice(0, 10),
    sourceUrl: source,
    createdBy: 'mock',
    provinces: status.provinces,
    prefectures: status.prefectures,
    counties: status.counties,
  }
  status = {
    ...builtin,
    builtin: false,
    sourceUrl: source,
    dataDate: current.dataDate,
    license: source === builtin.defaultSource ? 'MIT' : '',
    sha256: sha,
    syncedBy: 'mock',
    syncedAt: at,
  }
}

// 后台同步在 mock 里也保持「POST 只代表受理、结果走轮询」这层契约，否则页面上的轮询分支永远跑不到。
let syncJob: RegionSyncStatus = { running: false }

defineMock([
  { method: 'GET', url: '/regions/status', handler: () => status },
  {
    method: 'POST',
    url: '/regions/sync',
    handler: ({ body }): { started: boolean } => {
      const source = String((body as { source?: string } | undefined)?.source ?? status.defaultSource)
      syncJob = { running: true, stage: 'fetching' }
      setTimeout(() => {
        applySnapshot(source, `同步${shaSeq + 1}`)
        syncJob = { running: false, result: { info: status, added: 0, removed: 0, renamed: 0 } }
      }, 400)
      return { started: true }
    },
  },
  { method: 'GET', url: '/regions/sync/status', handler: () => syncJob },
  {
    method: 'POST',
    url: '/regions/preview',
    handler: (): RegionPreviewResult => ({ preview: true, added: 0, removed: 0, renamed: 0 }),
  },
  {
    method: 'POST',
    url: '/regions/reset',
    handler: () => {
      // 恢复内置也留档：删掉「当前这一份」之前先存成版本，之后还能从版本历史回滚回来。
      archive('builtin')
      current = null
      status = { ...builtin }
      return status
    },
  },
  {
    // 列表只给摘要字段：整棵树由 /regions/versions/:version 单独给。
    method: 'GET',
    url: '/regions/versions',
    handler: (): RegionVersion[] =>
      store.map((v) => ({
        version: v.version,
        sha256: v.sha256,
        dataDate: v.dataDate,
        sourceUrl: v.sourceUrl,
        provinces: v.provinces,
        prefectures: v.prefectures,
        counties: v.counties,
        createdBy: v.createdBy,
        createdAt: v.createdAt,
      })),
  },
  {
    method: 'GET',
    url: '/regions/versions/:version',
    handler: ({ params }): RegionVersionDetail =>
      store.find((v) => String(v.version) === params.version) ?? mockError(404, 'Not found'),
  },
  {
    method: 'POST',
    url: '/regions/versions/:version/rollback',
    handler: ({ params }): RegionSyncResult => {
      const target = store.find((v) => String(v.version) === params.version)
      if (!target) mockError(404, 'Not found')
      const sha = nextSha()
      archive(sha)
      current = {
        payload: target.payload,
        sha256: sha,
        dataDate: target.dataDate,
        sourceUrl: target.sourceUrl,
        createdBy: 'mock',
        provinces: target.provinces,
        prefectures: target.prefectures,
        counties: target.counties,
      }
      status = {
        ...builtin,
        builtin: false,
        sourceUrl: target.sourceUrl,
        dataDate: target.dataDate,
        sha256: sha,
        provinces: target.provinces,
        prefectures: target.prefectures,
        counties: target.counties,
        syncedBy: 'mock',
        syncedAt: nowISO(),
      }
      return { info: status, added: 0, removed: 0, renamed: 0 }
    },
  },
])
