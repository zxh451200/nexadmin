import { useEffect, useState } from 'react'
import { ChevronDown, CircleAlert, CircleCheck, Download } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { FileDropField } from '@/framework/components/FileDropField'
import { RequiredMark } from '@/framework/components/RequiredMark'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { blobExt, saveBlob } from '@/framework/components/export/csv'
import { Button } from '@/framework/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/framework/ui/dropdown-menu'
import { Label } from '@/framework/ui/label'
import {
  fetchCustomerImportErrors,
  fetchCustomerImportTemplate,
  useImportCustomers,
  type CrmImportJob,
} from './api'

export interface CustomerImportDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

/** 客户 Excel / CSV 导入：入口在客户列表，执行与台账仍走通用导入中心（source=crm_customers）。 */
export function CustomerImportDialog({ open, onOpenChange }: CustomerImportDialogProps) {
  const { t } = useTranslation()
  const importCustomers = useImportCustomers()
  const [file, setFile] = useState<File | null>(null)
  const [job, setJob] = useState<CrmImportJob | null>(null)
  const [downloading, setDownloading] = useState(false)

  useEffect(() => {
    if (open) {
      setFile(null)
      setJob(null)
    }
  }, [open])

  const onTemplate = async (format: 'xlsx' | 'csv') => {
    setDownloading(true)
    try {
      const blob = await fetchCustomerImportTemplate(format)
      saveBlob(`${t('plugin_crm.import.templateFilename')}.${blobExt(blob)}`, blob)
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    } finally {
      setDownloading(false)
    }
  }

  const onErrors = async () => {
    if (!job) return
    setDownloading(true)
    try {
      saveBlob(`${t('plugin_crm.import.errorsFilename')}-${job.id}.csv`, await fetchCustomerImportErrors(job.id))
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    } finally {
      setDownloading(false)
    }
  }

  const start = async () => {
    if (!file) {
      toast.error(t('plugin_crm.import.needFile'))
      return
    }
    try {
      setJob(await importCustomers.mutateAsync(file))
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t('common.operateFailed'))
    }
  }

  const errors = job?.errors ?? []

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      size="md"
      title={t('plugin_crm.import.title')}
      description={t('plugin_crm.import.description')}
      footer={
        <>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.close')}
          </Button>
          <Button type="button" onClick={() => void start()} disabled={importCustomers.isPending || !file}>
            {importCustomers.isPending ? t('plugin_crm.import.starting') : t('plugin_crm.import.start')}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-5">
        <div className="flex flex-col gap-1.5">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <Label id="crm-import-file-label">
              {t('plugin_crm.import.file')}
              <RequiredMark />
            </Label>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button type="button" variant="ghost" size="sm" className="-me-2 text-muted-foreground" disabled={downloading}>
                  <Download aria-hidden="true" />
                  {t('plugin_crm.import.template')}
                  <ChevronDown aria-hidden="true" className="opacity-60" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onSelect={() => void onTemplate('xlsx')}>{t('plugin_crm.import.templateXlsx')}</DropdownMenuItem>
                <DropdownMenuItem onSelect={() => void onTemplate('csv')}>{t('plugin_crm.import.templateCsv')}</DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
          <FileDropField
            accept=".xlsx,.csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv"
            labelledBy="crm-import-file-label"
            files={file ? [file] : []}
            onChange={(fs) => {
              setFile(fs[0] ?? null)
              setJob(null)
            }}
          />
          <p className="text-xs leading-relaxed text-muted-foreground">{t('plugin_crm.import.fileHint')}</p>
        </div>
        {job ? (
          <div className="flex flex-col gap-2.5 rounded-lg border border-border bg-muted/50 p-3" role="status">
            <p className="flex items-center gap-2 text-sm font-medium text-foreground">
              {job.failRows > 0 ? (
                <CircleAlert className="size-4 shrink-0 text-warning" aria-hidden="true" />
              ) : (
                <CircleCheck className="size-4 shrink-0 text-success" aria-hidden="true" />
              )}
              {t('plugin_crm.import.result', { total: job.totalRows, ok: job.okRows, fail: job.failRows })}
            </p>
            {errors.length ? (
              <>
                <ul className="flex max-h-40 flex-col gap-1 overflow-y-auto text-sm text-destructive">
                  {errors.slice(0, 5).map((e) => (
                    <li key={e.row}>{t('plugin_crm.import.errorRow', { row: e.row, reason: e.message })}</li>
                  ))}
                </ul>
                {errors.length > 5 ? (
                  <p className="text-xs text-muted-foreground">{t('plugin_crm.import.moreErrors', { count: errors.length - 5 })}</p>
                ) : null}
                <div>
                  <Button type="button" variant="outline" size="sm" disabled={downloading} onClick={() => void onErrors()}>
                    <Download aria-hidden="true" />
                    {t('plugin_crm.import.downloadErrors')}
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
