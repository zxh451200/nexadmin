import { useState } from 'react'
import { Check, Copy, Download, FileCode2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { DataTable, type Column } from '@/framework/components/DataTable'
import { EmptyState } from '@/framework/components/EmptyState'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { cardSurface } from '@/framework/ui/card'
import { cn } from '@/lib/utils'
import { saveBlob, type GenFile, type GenOutput } from './api'
import { CodeView } from './CodeView'

interface Props {
  output: GenOutput
}

/** 只读代码查看：左侧文件列表，右侧内容。代码恒为从左到右，不随界面 RTL 翻转。 */
export function PreviewStep({ output }: Props) {
  const { t } = useTranslation()
  const [current, setCurrent] = useState<string | undefined>()
  const [copied, setCopied] = useState(false)
  const file: GenFile | undefined = output.files.find((f) => f.path === current) ?? output.files[0]

  const copy = async () => {
    if (!file) return
    try {
      await navigator.clipboard.writeText(file.content)
      setCopied(true)
      toast.success(t('plugin_codegen.preview.copied'))
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      toast.error(t('plugin_codegen.preview.copyFailed'))
    }
  }

  const downloadFile = () => {
    if (!file) return
    saveBlob(new Blob([file.content], { type: 'text/plain;charset=utf-8' }), file.path.split('/').pop() ?? file.path)
    toast.success(t('plugin_codegen.preview.downloaded'))
  }

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
                <FileCode2 className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
                <span className="min-w-0 truncate font-mono text-xs">{file.path}</span>
                <div className="ms-auto flex shrink-0 items-center gap-0.5">
                  <Badge variant="secondary" className="me-1 hidden sm:inline-flex">
                    {t('plugin_codegen.preview.readonly')}
                  </Badge>
                  <Button type="button" variant="ghost" size="icon" className="size-8" title={t('plugin_codegen.preview.copy')} aria-label={t('plugin_codegen.preview.copy')} onClick={() => void copy()}>
                    {copied ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
                  </Button>
                  <Button type="button" variant="ghost" size="icon" className="size-8" title={t('plugin_codegen.preview.download')} aria-label={t('plugin_codegen.preview.download')} onClick={downloadFile}>
                    <Download aria-hidden="true" />
                  </Button>
                </div>
              </header>
              <CodeView content={file.content} lang={file.lang} />
            </>
          ) : (
            <EmptyState title={t('plugin_codegen.preview.pick')} />
          )}
        </section>
      </div>
    </div>
  )
}
