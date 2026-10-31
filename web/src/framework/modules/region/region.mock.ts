import { defineMock } from '@/framework/request/mock'
import type { RegionStatus, RegionSyncResult } from './api'

const builtin: RegionStatus = {
  builtin: true,
  sourceUrl: '',
  dataDate: '',
  license: '',
  sha256: '',
  provinces: 31,
  prefectures: 333,
  counties: 2847,
  syncedBy: '',
  syncedAt: '',
  defaultSource: 'https://raw.githubusercontent.com/mumuy/data_location/master/list.json',
}

let status: RegionStatus = { ...builtin }

defineMock([
  { method: 'GET', url: '/regions/status', handler: () => status },
  {
    method: 'POST',
    url: '/regions/sync',
    handler: ({ body }): RegionSyncResult => {
      const source = String((body as { source?: string } | undefined)?.source ?? status.defaultSource)
      status = {
        ...builtin,
        builtin: false,
        sourceUrl: source,
        dataDate: new Date().toISOString().slice(0, 10),
        license: source === builtin.defaultSource ? 'MIT' : '',
        syncedBy: 'mock',
        syncedAt: new Date().toISOString(),
      }
      return { info: status, added: 0, removed: 0, renamed: 0 }
    },
  },
  {
    method: 'POST',
    url: '/regions/reset',
    handler: () => {
      status = { ...builtin }
      return status
    },
  },
])
