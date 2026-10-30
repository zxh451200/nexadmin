import { Eye } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'

/**
 * 表格行「查看详情」的统一入口：全站同一个外观（幽灵图标按钮 + 悬停提示 + 可读名称）。
 * 行点击仍是快捷方式，但每个有详情的列表都要有这颗按钮，避免有的页面只有「点行才看得到」。
 */
export function DetailAction({ onClick, label }: { onClick: () => void; label?: string }) {
  const { t } = useTranslation()
  const text = label ?? t('common.detail')
  return (
    <Button type="button" variant="ghost" size="icon" aria-label={text} title={text} onClick={onClick}>
      <Eye aria-hidden="true" />
    </Button>
  )
}
