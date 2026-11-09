import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { BrandMark } from '@/framework/components/BrandMark'
import { usePublicSettings } from '@/framework/layout/Watermark'

// 路由 chunk / 插件状态加载兜底：页面正中一枚呼吸的 logo。
// 延迟 150ms 才出现，加载快时不闪；假骨架会在深色下和背景同色、看着像空白页，已弃用。
export function PageLoading() {
  const { t } = useTranslation()
  const { data } = usePublicSettings()
  const [visible, setVisible] = useState(false)
  useEffect(() => {
    const id = window.setTimeout(() => setVisible(true), 150)
    return () => window.clearTimeout(id)
  }, [])
  return (
    <div
      role="status"
      className="grid min-h-[calc(100dvh-6.5rem)] place-items-center p-4 max-md:min-h-[calc(100dvh-3.5rem)] md:p-6"
    >
      {visible ? <BrandMark logoUrl={data?.logoUrl} name={data?.siteName} className="logo-breathe size-12 rounded-xl" /> : null}
      <span className="sr-only">{t('common.loading')}</span>
    </div>
  )
}
