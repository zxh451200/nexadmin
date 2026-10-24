import { FileQuestion } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { ErrorPage } from './ErrorPage'

export default function NotFoundPage({ embedded = true }: { embedded?: boolean }) {
  const { t } = useTranslation()
  return (
    <ErrorPage
      code="404"
      title={t('error.404.title')}
      description={t('error.404.desc')}
      icon={<FileQuestion className="size-10" aria-hidden="true" />}
      embedded={embedded}
    />
  )
}
