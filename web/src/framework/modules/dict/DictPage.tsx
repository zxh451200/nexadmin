import { useCallback, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { useIsMobile } from '@/framework/hooks/use-mobile'
import type { DictTypeRow } from './api'
import { DictDataPanel } from './DictDataPanel'
import { DictTypePanel } from './DictTypePanel'

export default function DictPage() {
  const { t } = useTranslation()
  const isMobile = useIsMobile()
  const [selected, setSelected] = useState<DictTypeRow | null>(null)
  // 手机上类型列表与数据二选一显示
  const [showData, setShowData] = useState(false)

  const onSelect = useCallback((row: DictTypeRow) => {
    setSelected((cur) => (cur?.id === row.id && cur.name === row.name && cur.code === row.code ? cur : row))
    setShowData(true)
  }, [])
  const onDeleted = useCallback((id: string) => {
    setSelected((cur) => (cur?.id === id ? null : cur))
    setShowData(false)
  }, [])

  const typePanel = <DictTypePanel selectedId={selected?.id ?? null} onSelect={onSelect} onDeleted={onDeleted} />

  return (
    <PageContainer>
      <PageHeader title={t('dict.title')} description={t('dict.description')} />
      {isMobile ? (
        showData && selected ? (
          <DictDataPanel type={selected} onBack={() => setShowData(false)} />
        ) : (
          typePanel
        )
      ) : (
        <div className="grid grid-cols-[minmax(280px,360px)_minmax(0,1fr)] items-start gap-6">
          {typePanel}
          <DictDataPanel type={selected} />
        </div>
      )}
    </PageContainer>
  )
}
