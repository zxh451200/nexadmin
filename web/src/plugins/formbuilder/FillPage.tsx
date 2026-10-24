import { ClipboardList, Clock, Users } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { EmptyState } from '@/framework/components/EmptyState'
import { PageContainer, PageHeader } from '@/framework/components/PageHeader'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/framework/ui/card'
import { Skeleton } from '@/framework/ui/skeleton'
import { useFillList, type FillItem } from './api'
import { FillView } from './FillView'
import { closedVariant, formatTime } from './format'

/** 填写入口：没有 ?id= 时列出我能填写的已发布表单；有 ?id= 时进入该表单的填写页 */
export default function FillPage() {
  const [params] = useSearchParams()
  const id = params.get('id') ?? undefined
  return id ? <FillView id={id} /> : <FillList />
}

function FillList() {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const { data, isLoading, isError } = useFillList()
  const open = (f: FillItem) => navigate(`/system/formbuilder/fill?id=${f.id}`)

  return (
    <PageContainer>
      <PageHeader title={t('plugin_formbuilder.fillTitle')} description={t('plugin_formbuilder.fillDescription')} />
      {isLoading ? (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-40 w-full" />
          ))}
        </div>
      ) : isError ? (
        <p role="alert" className="py-8 text-center text-sm text-destructive">
          {t('plugin_formbuilder.loadFailed')}
        </p>
      ) : !data || data.length === 0 ? (
        <EmptyState icon={<ClipboardList className="size-6" aria-hidden="true" />} title={t('plugin_formbuilder.fillEmpty')} />
      ) : (
        <ul className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
          {data.map((f) => (
            <li key={f.id}>
              <Card className="flex h-full flex-col">
                <CardHeader className="pb-2">
                  <CardTitle className="flex items-start justify-between gap-2 text-base">
                    <span className="min-w-0 break-words">{f.name}</span>
                    {f.closed ? <Badge variant={closedVariant(f.closed)}>{t(`plugin_formbuilder.closed.${f.closed}`)}</Badge> : null}
                  </CardTitle>
                </CardHeader>
                <CardContent className="flex flex-1 flex-col gap-3">
                  {f.description ? <p className="line-clamp-3 text-sm text-muted-foreground">{f.description}</p> : null}
                  <dl className="flex flex-col gap-1 text-xs text-muted-foreground">
                    {f.deadlineAt ? (
                      <div className="flex items-center gap-1.5">
                        <Clock className="size-3.5" aria-hidden="true" />
                        <dt className="sr-only">{t('plugin_formbuilder.fields.deadline')}</dt>
                        <dd className="num">{formatTime(f.deadlineAt, i18n.language)}</dd>
                      </div>
                    ) : null}
                    {f.maxSubmissions > 0 ? (
                      <div className="flex items-center gap-1.5">
                        <Users className="size-3.5" aria-hidden="true" />
                        <dt className="sr-only">{t('plugin_formbuilder.fields.quota')}</dt>
                        <dd className="num">
                          {f.submissionCount} / {f.maxSubmissions}
                        </dd>
                      </div>
                    ) : null}
                  </dl>
                  <div className="mt-auto pt-1">
                    <Button variant={f.closed ? 'outline' : 'default'} onClick={() => open(f)}>
                      {f.closed ? t('plugin_formbuilder.fillView') : t('plugin_formbuilder.fillNow')}
                    </Button>
                  </div>
                </CardContent>
              </Card>
            </li>
          ))}
        </ul>
      )}
    </PageContainer>
  )
}
