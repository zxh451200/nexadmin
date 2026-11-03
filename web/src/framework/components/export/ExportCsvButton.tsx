import { useState } from 'react'
import { Download } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Can } from '@/framework/auth/Can'
import { Button, type ButtonProps } from '@/framework/ui/button'
import { downloadCsv, stamp } from './csv'

/** 一列：表头走调用方所在模块的字典，取值函数负责把枚举/时间翻成人读得到的文本 */
export interface CsvColumn<T> {
  header: string
  value: (row: T) => string | number
}

export interface ExportCsvButtonProps<T> {
  /** 文件名主干，不含时间戳与扩展名 */
  filename: string
  columns: CsvColumn<T>[]
  /** 取「当前筛选条件下的全量」，不是当前页；由调用方保证与列表同口径 */
  getRows: () => Promise<T[]>
  /** 给了就在按钮外面套 Can；不给则假定调用方已控权 */
  perm?: string
  variant?: ButtonProps['variant']
  size?: ButtonProps['size']
}

/**
 * 分页列表专用：按页把所有行捞齐（沿用列表同一套筛选与数据范围）。
 * 只在后端没有「不分页取全量」入口时用；有入口就直接取，别在前端翻几十页。
 */
export async function collectAllPages<T>(
  fetchPage: (page: number, pageSize: number) => Promise<{ list?: T[]; total: number } | undefined>,
  pageSize = 200,
  maxRows = 20000,
): Promise<T[]> {
  const out: T[] = []
  for (let page = 1; ; page++) {
    const res = await fetchPage(page, pageSize)
    const rows = res?.list ?? []
    out.push(...rows)
    if (rows.length === 0 || out.length >= (res?.total ?? 0) || out.length >= maxRows) return out
  }
}

/**
 * 导出当前筛选结果为 CSV：表头与取值都留在前端，因此列名天然跟字典、跟语言走。
 * 数据量可能超过同步上限的列表请改走导出中心（后端 job），不要用这个按钮兜大表。
 */
export function ExportCsvButton<T>({ filename, columns, getRows, perm, variant = 'outline', size }: ExportCsvButtonProps<T>) {
  const { t } = useTranslation()
  const [busy, setBusy] = useState(false)

  const run = async () => {
    setBusy(true)
    try {
      const rows = await getRows()
      if (rows.length === 0) {
        toast.info(t('exportCsv.noRows'))
        return
      }
      downloadCsv(`${filename}-${stamp()}.csv`, [columns.map((c) => c.header), ...rows.map((r) => columns.map((c) => c.value(r)))])
      toast.success(t('exportCsv.done', { count: rows.length }))
    } catch {
      toast.error(t('common.operateFailed'))
    } finally {
      setBusy(false)
    }
  }

  const button = (
    <Button variant={variant} size={size} disabled={busy} onClick={() => void run()}>
      <Download aria-hidden="true" />
      {t('common.export')}
    </Button>
  )
  return perm ? <Can perm={perm}>{button}</Can> : button
}
