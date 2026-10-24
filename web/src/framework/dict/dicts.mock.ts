import { defineMock } from '@/framework/request/mock'
import type { DictItem } from '@/framework/request/types'
import { dictSeed } from './seed'

defineMock([
  {
    method: 'GET',
    url: '/dicts/:code/items',
    handler: ({ params }): DictItem[] => {
      const en = (localStorage.getItem('nex-lang') ?? '').startsWith('en')
      return (dictSeed[params.code]?.items ?? []).map(({ labelEn, ...item }) => ({
        ...item,
        label: en && labelEn ? labelEn : item.label,
      }))
    },
  },
])
