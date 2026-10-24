import { useTranslation } from 'react-i18next'
import { Input } from '@/framework/ui/input'
import { DictSelect } from './DictSelect'
import { FilterBar } from './FilterBar'
import type { ClientFilter } from './table/use-client-filter'

interface Props {
  filter: ClientFilter
  placeholder?: string
  /** 需要「类型」下拉时给字典编码，如 menu_type */
  typeDict?: string
  typePlaceholder?: string
}

/** 不分页列表的通用筛选栏：关键词 + 可选类型 + 状态 */
export function SimpleFilterBar({ filter, placeholder, typeDict, typePlaceholder }: Props) {
  const { t } = useTranslation()
  const { draft, setDraft } = filter
  return (
    <FilterBar onSearch={filter.search} onReset={filter.reset}>
      <Input
        className="md:w-64"
        value={draft.keyword}
        onChange={(e) => setDraft((d) => ({ ...d, keyword: e.target.value }))}
        placeholder={placeholder ?? t('common.keyword')}
        aria-label={t('common.keyword')}
      />
      {typeDict ? (
        <DictSelect
          className="md:w-40"
          dict={typeDict}
          placeholder={typePlaceholder}
          value={draft.type}
          onChange={(v) => setDraft((d) => ({ ...d, type: v }))}
        />
      ) : null}
      <DictSelect
        className="md:w-40"
        dict="sys_status"
        placeholder={t('common.status')}
        value={draft.status}
        onChange={(v) => setDraft((d) => ({ ...d, status: v }))}
      />
    </FilterBar>
  )
}
