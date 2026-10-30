import type { ReactNode } from 'react'
import { Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'

/** 已安装 / 市场两个标签页共用的筛选行：关键词 + 分类芯片；trailing 放页级操作。 */
export function PluginFilterBar({
  keyword,
  onKeyword,
  placeholder,
  category,
  onCategory,
  categories,
  trailing,
}: {
  keyword: string
  onKeyword: (value: string) => void
  placeholder: string
  category: string
  onCategory: (value: string) => void
  categories: string[]
  trailing?: ReactNode
}) {
  const { t } = useTranslation()
  return (
    <div className="flex flex-wrap items-center gap-2">
      <div className="relative w-64 max-w-full">
        <Search className="absolute start-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
        <Input className="ps-8" placeholder={placeholder} value={keyword} onChange={(e) => onKeyword(e.target.value)} />
      </div>
      {categories.map((c) => (
        <Button
          key={c}
          size="sm"
          variant={category === c ? 'default' : 'outline'}
          onClick={() => onCategory(category === c ? '' : c)}
        >
          {t(`pluginMgr.market.category.${c}`, c)}
        </Button>
      ))}
      {trailing ? <div className="ms-auto flex items-center gap-2">{trailing}</div> : null}
    </div>
  )
}
