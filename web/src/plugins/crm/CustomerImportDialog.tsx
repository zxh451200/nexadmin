import { useEffect, useRef, useState } from 'react'
import { ChevronDown, Download } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { RequiredMark } from '@/framework/components/RequiredMark'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { blobExt, saveBlob } from '@/framework/components/export/csv'
import { Button } from '@/framework/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/framework/ui/dropdown-menu'
import { Input } from '@/framework/ui/input'
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
  const fileInput = useRef<HTMLInputElement>(null)
  const [file, setFile] = useState<File | null>(null)
  const [job, setJob] = useState<CrmImportJob | null>(null)
  const [downloading, setDownloading] = useState(false)

  useEffect(() => {
    if (open) {
      setFile(null)
      setJob(null)
      if (fileInput.current) fileInput.current.value = ''
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
      <div className="flex flex-col gap-4">
        <div>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button type="button" variant="outline" disabled={downloading}>
                <Download aria-hidden="true" className="me-2 size-4" />
                {t('plugin_crm.import.template')}
                <ChevronDown aria-hidden="true" className="opacity-60" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start">
              <DropdownMenuItem onSelect={() => void onTemplate('xlsx')}>{t('plugin_crm.import.templateXlsx')}</DropdownMenuItem>
              <DropdownMenuItem onSelect={() => void onTemplate('csv')}>{t('plugin_crm.import.templateCsv')}</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="crm-import-file">
            {t('plugin_crm.import.file')}
            <RequiredMark />
          </Label>
          <Input
            id="crm-import-file"
            ref={fileInput}
            type="file"
            accept=".xlsx,.csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv"
            onChange={(e) => {
              setFile(e.target.files?.[0] ?? null)
              setJob(null)
            }}
          />
          <p className="text-xs text-muted-foreground">{t('plugin_crm.import.fileHint')}</p>
        </div>
        {job ? (
          <div className="flex flex-col gap-2 rounded-lg border border-border bg-muted p-3" role="status">
            <p className="text-sm font-medium text-foreground">
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
                    <Download aria-hidden="true" className="me-2 size-4" />
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
