import { useEffect, useState } from 'react'
import { CircleAlert, CircleCheck, Download } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { FileDropField } from '@/framework/components/FileDropField'
import { RequiredMark } from '@/framework/components/RequiredMark'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Button } from '@/framework/ui/button'
import { Label } from '@/framework/ui/label'
import { blobExt, downloadCsv, saveBlob } from '@/framework/components/export/csv'
import { fetchImportTemplate, useImportUsers, type ImportResult } from './api'

export interface ImportDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function ImportDialog({ open, onOpenChange }: ImportDialogProps) {
  const { t } = useTranslation()
  const importUsers = useImportUsers()
  const [file, setFile] = useState<File | null>(null)
  const [result, setResult] = useState<ImportResult | null>(null)

  useEffect(() => {
    if (open) {
      setFile(null)
      setResult(null)
    }
  }, [open])

  const downloadTemplate = async () => {
    try {
      const blob = await fetchImportTemplate()
      saveBlob(`${t('user.import.templateFilename')}.${blobExt(blob)}`, blob)
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  }

  const downloadFailed = () => {
    if (!result) return
    downloadCsv(`${t('user.import.failedFilename')}.csv`, [
      [t('user.import.col.row'), t('user.import.col.reason')],
      ...result.failed.map((f) => [String(f.row), f.reason]),
    ])
  }

  const start = async () => {
    if (!file) {
      toast.error(t('user.import.chooseFile'))
      return
    }
    try {
      setResult(await importUsers.mutateAsync(file))
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  }

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="md"
      title={t('user.import.title')}
      description={t('user.import.description')}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.close')}
          </Button>
          <Button type="button" onClick={start} disabled={importUsers.isPending || !file}>
            {importUsers.isPending ? t('common.loading') : t('user.import.start')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-5">
        <div className="flex flex-col gap-1.5">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <Label id="user-import-file-label">
              {t('user.import.file')}
              <RequiredMark />
            </Label>
            <Button type="button" variant="ghost" size="sm" className="-me-2 text-muted-foreground" onClick={downloadTemplate}>
              <Download aria-hidden="true" />
              {t('user.import.template')}
            </Button>
          </div>
          <FileDropField
            accept=".xlsx,.csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv"
            labelledBy="user-import-file-label"
            files={file ? [file] : []}
            onChange={(fs) => {
              setFile(fs[0] ?? null)
              setResult(null)
            }}
          />
          <p className="text-xs leading-relaxed text-muted-foreground">{t('user.import.fileHint')}</p>
        </div>
        {result ? (
          <div className="flex flex-col gap-2.5 rounded-lg border border-border bg-muted/50 p-3" role="status">
            <p className="flex items-center gap-2 text-sm font-medium text-foreground">
              {result.failed.length ? (
                <CircleAlert className="size-4 shrink-0 text-warning" aria-hidden="true" />
              ) : (
                <CircleCheck className="size-4 shrink-0 text-success" aria-hidden="true" />
              )}
              {t('user.import.result', { success: result.success, failed: result.failed.length })}
            </p>
            {result.failed.length ? (
              <>
                <ul className="flex max-h-48 flex-col gap-1 overflow-y-auto text-sm text-destructive">
                  {result.failed.map((f) => (
                    <li key={f.row}>{t('user.import.errorRow', { row: f.row, reason: f.reason })}</li>
                  ))}
                </ul>
                <div>
                  <Button type="button" variant="outline" size="sm" onClick={downloadFailed}>
                    <Download aria-hidden="true" />
                    {t('user.import.downloadFailed')}
                  </Button>
                </div>
              </>
            ) : null}
          </div>
        ) : null}
      </div>
    </ResponsiveDialog>
  )
}
