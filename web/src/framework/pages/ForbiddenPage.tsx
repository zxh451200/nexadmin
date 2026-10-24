import { ShieldAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { ErrorPage } from './ErrorPage'

export default function ForbiddenPage() {
  const { t } = useTranslation()
  return <ErrorPage code="403" title={t('error.403.title')} description={t('error.403.desc')} icon={<ShieldAlert className="size-10" aria-hidden="true" />} />
}
