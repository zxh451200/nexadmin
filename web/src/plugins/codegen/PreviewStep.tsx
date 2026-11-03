import { useMemo, useState } from 'react'
import { Check, Copy, Download, FileCode2, Files } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
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

  // 按目录分组：同一目录只写一遍路径前缀，窄栏里也能一眼扫完。
  const groups = useMemo(() => {
    const map = new Map<string, GenFile[]>()
    for (const f of output.files) {
      const cut = f.path.lastIndexOf('/')
      const dir = cut < 0 ? '' : f.path.slice(0, cut)
      const list = map.get(dir)
      if (list) list.push(f)
      else map.set(dir, [f])
    }
    return [...map]
  }, [output.files])

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

  return (
    <div className="flex flex-col gap-4">
      <ul className={cn(cardSurface, 'flex flex-col gap-1 p-3 text-sm text-muted-foreground')}>
        {output.notes.map((n) => {
          const [head, tail] = t(`plugin_codegen.notes.${n.code}`).split('{{value}}')
          return (
            <li key={n.code}>
              {head}
              {n.value ? <code className="mx-1 rounded bg-muted px-1.5 py-0.5 font-mono text-xs text-foreground">{n.value}</code> : null}
              {tail}
            </li>
          )
        })}
      </ul>
      <div className="grid gap-4 lg:grid-cols-[minmax(0,22rem)_minmax(0,1fr)]">
        <nav className={cn(cardSurface, 'flex min-w-0 flex-col overflow-hidden')} aria-label={t('plugin_codegen.preview.files')}>
          <header className="flex items-center gap-2 border-b border-border bg-muted/50 px-3 py-2">
            <Files className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
            <span className="text-xs font-medium">{t('plugin_codegen.preview.files')}</span>
            <span className="ms-auto font-mono text-xs text-muted-foreground">{output.files.length}</span>
          </header>
          <div className="max-h-[40dvh] overflow-y-auto overscroll-contain p-1.5 lg:max-h-[60dvh]">
            {output.files.length === 0 ? (
              <p className="px-2 py-6 text-center text-xs text-muted-foreground">{t('plugin_codegen.preview.empty')}</p>
            ) : null}
            {groups.map(([dir, files]) => (
              <div key={dir || '_'} className="mb-1 last:mb-0">
                {dir ? (
                  <p className="sticky top-0 z-10 truncate bg-card px-2 py-1 font-mono text-[11px] text-muted-foreground">{dir}</p>
                ) : null}
                <ul>
                  {files.map((f) => {
                    const active = f.path === file?.path
                    const name = f.path.slice(f.path.lastIndexOf('/') + 1)
                    return (
                      <li key={f.path}>
                        <button
                          type="button"
                          onClick={() => setCurrent(f.path)}
                          aria-current={active ? 'true' : undefined}
                          className={cn(
                            'flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-start transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                            active ? 'bg-primary/10 text-primary' : 'text-foreground hover:bg-muted',
                          )}
                        >
                          <FileCode2 className={cn('size-3.5 shrink-0', active ? 'text-primary' : 'text-muted-foreground')} aria-hidden="true" />
                          <span className="min-w-0 flex-1 truncate font-mono text-xs">{name}</span>
                          <span className="shrink-0 text-[10px] uppercase tracking-wide text-muted-foreground">{f.lang}</span>
                        </button>
                      </li>
                    )
                  })}
                </ul>
              </div>
            ))}
          </div>
        </nav>
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
