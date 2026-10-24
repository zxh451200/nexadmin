import { useEffect, useRef, useState } from 'react'
import { Download } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { blobExt, fetchImportTemplate, useImportUsers, type ImportResult } from './api'
import { downloadCsv, saveBlob } from './csv'

export interface ImportDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function ImportDialog({ open, onOpenChange }: ImportDialogProps) {
  const { t } = useTranslation()
  const importUsers = useImportUsers()
  const fileInput = useRef<HTMLInputElement>(null)
  const [file, setFile] = useState<File | null>(null)
  const [result, setResult] = useState<ImportResult | null>(null)

  useEffect(() => {
    if (open) {
      setFile(null)
      setResult(null)
      if (fileInput.current) fileInput.current.value = ''
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
      <div className="flex flex-col gap-4">
        <div>
          <Button type="button" variant="outline" onClick={downloadTemplate}>
            <Download className="me-2 h-4 w-4" aria-hidden="true" />
            {t('user.import.template')}
          </Button>
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="user-import-file">{t('user.import.file')}</Label>
          <Input
            id="user-import-file"
            ref={fileInput}
            type="file"
            accept=".xlsx,.csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv"
            onChange={(e) => {
              setFile(e.target.files?.[0] ?? null)
              setResult(null)
            }}
          />
          <p className="text-xs text-muted-foreground">{t('user.import.fileHint')}</p>
        </div>
        {result ? (
          <div className="flex flex-col gap-2 rounded-lg border border-border bg-muted p-3" role="status">
            <p className="text-sm font-medium text-foreground">
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
                    <Download className="me-2 h-4 w-4" aria-hidden="true" />
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
