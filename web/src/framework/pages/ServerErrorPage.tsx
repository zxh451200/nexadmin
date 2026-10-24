import { ServerCrash } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { ErrorPage } from './ErrorPage'

export default function ServerErrorPage() {
  const { t } = useTranslation()
  return <ErrorPage code="500" title={t('error.500.title')} description={t('error.500.desc')} icon={<ServerCrash className="size-10" aria-hidden="true" />} />
}
