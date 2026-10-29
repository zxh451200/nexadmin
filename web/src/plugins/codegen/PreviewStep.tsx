import { useState } from 'react'
import { FileCode2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { EmptyState } from '@/framework/components/EmptyState'
import { Badge } from '@/framework/ui/badge'
import { cardSurface } from '@/framework/ui/card'
import { cn } from '@/lib/utils'
import type { GenFile, GenOutput } from './api'

interface Props {
  output: GenOutput
}

/** 只读代码查看：左侧文件列表，右侧内容。代码恒为从左到右，不随界面 RTL 翻转。 */
export function PreviewStep({ output }: Props) {
  const { t } = useTranslation()
  const [current, setCurrent] = useState<string | undefined>()
  const file: GenFile | undefined = output.files.find((f) => f.path === current) ?? output.files[0]

  const columns: Column<GenFile>[] = [
    {
      key: 'path',
      title: t('plugin_codegen.preview.path'),
      mobile: 'title',
      render: (f) => (
        <span className={cn('font-mono text-xs', f.path === file?.path && 'font-semibold text-primary')}>{f.path}</span>
      ),
    },
    { key: 'lang', title: t('plugin_codegen.preview.lang'), render: (f) => <Badge variant="outline">{f.lang}</Badge> },
  ]

  return (
    <div className="flex flex-col gap-4">
      <ul className={cn(cardSurface, 'flex flex-col gap-1 p-3 text-sm text-muted-foreground')}>
        {output.notes.map((n) => (
          <li key={n.code}>
            {t(`plugin_codegen.notes.${n.code}`)}
            {n.value ? <code className="ms-2 rounded bg-muted px-1.5 py-0.5 font-mono text-xs text-foreground">{n.value}</code> : null}
          </li>
        ))}
      </ul>
      <div className="grid gap-4 lg:grid-cols-[minmax(0,22rem)_minmax(0,1fr)]">
        <DataTable
          columns={columns}
          data={output.files}
          rowKey="path"
          pagination={false}
          onRowClick={(f) => setCurrent(f.path)}
          emptyText={t('plugin_codegen.preview.empty')}
        />
        <section className={cn(cardSurface, 'min-w-0 overflow-hidden')} aria-label={t('plugin_codegen.preview.viewer')}>
          {file ? (
            <>
              <header className="flex items-center gap-2 border-b border-border bg-muted/50 px-3 py-2">
                <FileCode2 className="size-4 text-muted-foreground" aria-hidden="true" />
                <span className="truncate font-mono text-xs">{file.path}</span>
                <Badge variant="secondary" className="ms-auto">{t('plugin_codegen.preview.readonly')}</Badge>
              </header>
              <pre dir="ltr" tabIndex={0} className="max-h-[60dvh] overflow-auto p-3 text-start font-mono text-xs leading-relaxed text-foreground">
                <code>{file.content}</code>
              </pre>
            </>
          ) : (
            <EmptyState title={t('plugin_codegen.preview.pick')} />
          )}
        </section>
      </div>
    </div>
  )
}
