import { useTranslation } from 'react-i18next'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Button } from '@/framework/ui/button'
import { Skeleton } from '@/framework/ui/skeleton'
import { useSubmissionDetail } from './api'
import { formatTime } from './format'
import { displayValue, hasValue } from './schema'

interface Props {
  formId: string
  submissionId: string | null
  onClose: () => void
}

/** 提交详情：按该提交所用版本的定义展示（改版后历史数据仍按当时的字段与选项名称） */
export function SubmissionDetailDialog({ formId, submissionId, onClose }: Props) {
  const { t, i18n } = useTranslation()
  const { data, isLoading } = useSubmissionDetail(formId, submissionId ?? undefined)
  const yes = t('common.yes')
  const no = t('common.no')

  return (
    <ResponsiveDialog
      open={!!submissionId}
      onOpenChange={(o) => !o && onClose()}
      size="lg"
      title={t('plugin_formbuilder.submissions.detail')}
      footer={
        <Button type="button" variant="outline" onClick={onClose}>
          {t('common.close')}
        </Button>
      }
    >
      {isLoading || !data ? (
        <Skeleton className="h-40 w-full" />
      ) : (
        <div className="flex flex-col gap-4">
          <dl className="grid grid-cols-1 gap-2 rounded-md bg-muted/50 p-3 text-sm sm:grid-cols-3">
            <Meta label={t('plugin_formbuilder.submissions.submitter')} value={data.username} />
            <Meta label={t('plugin_formbuilder.submissions.submittedAt')} value={formatTime(data.createdAt, i18n.language)} />
            <Meta label={t('plugin_formbuilder.fields.version')} value={`v${data.version}`} />
          </dl>
          <dl className="flex flex-col gap-3">
            {data.definition.fields.map((f) => {
              if (f.type === 'section') {
                return (
                  <div key={f.key} className="border-b border-border pb-1 pt-2 text-sm font-semibold">
                    {f.title}
                  </div>
                )
              }
              if (!hasValue(f) || !(f.key in data.data)) return null
              const v = data.data[f.key]
              return (
                <div key={f.key} className="flex flex-col gap-0.5">
                  <dt className="text-xs text-muted-foreground">{f.title}</dt>
                  <dd className="break-words text-sm">
                    {f.type === 'file' && Array.isArray(v) ? <FileLinks files={v} /> : f.type === 'rating' ? `${String(v)} / ${f.max ?? 5}` : displayValue(f, v, yes, no) || '-'}
                  </dd>
                </div>
              )
            })}
          </dl>
        </div>
      )}
    </ResponsiveDialog>
  )
}

function Meta({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="num font-medium">{value}</dd>
    </div>
  )
}

/** 文件只渲染成普通链接；服务端已限定为站内路径或 http(s) 地址 */
function FileLinks({ files }: { files: unknown[] }) {
  return (
    <ul className="flex flex-col gap-1">
      {(files as Array<{ name?: string; url?: string }>).map((f, i) => (
        <li key={i}>
          <a href={f.url} target="_blank" rel="noopener noreferrer" className="text-primary underline-offset-2 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
            {f.name || f.url}
          </a>
        </li>
      ))}
    </ul>
  )
}
