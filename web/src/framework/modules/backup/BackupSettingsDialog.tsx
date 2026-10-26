import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ResponsiveDialog } from '@/framework/components/ResponsiveDialog'
import { Button } from '@/framework/ui/button'
import { Label } from '@/framework/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { useSaveBackupConfig, type BackupConfig } from './api'

const OFF = 'off'
const DAY_OPTIONS = [0, 7, 14, 30, 60, 90, 180, 365]
const COUNT_OPTIONS = [0, 3, 5, 10, 20, 50]
// 每半小时一档；当前值不在其中（如 yaml 里配了 03:15）时临时补上
const TIME_OPTIONS = Array.from({ length: 48 }, (_, i) => `${String(Math.floor(i / 2)).padStart(2, '0')}:${i % 2 ? '30' : '00'}`)

function withCurrent<T extends string | number>(list: T[], cur: T): T[] {
  return list.includes(cur) ? list : [...list, cur].sort((a, b) => (a < b ? -1 : a > b ? 1 : 0))
}

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  value: BackupConfig | undefined
}

export function BackupSettingsDialog({ open, onOpenChange, value }: Props) {
  const { t } = useTranslation()
  const save = useSaveBackupConfig()
  const [time, setTime] = useState(OFF)
  const [days, setDays] = useState('30')
  const [count, setCount] = useState('10')

  useEffect(() => {
    if (!open || !value) return
    setTime(value.schedule || OFF)
    setDays(String(value.keepDays))
    setCount(String(value.keepCount))
  }, [open, value])

  const times = withCurrent(TIME_OPTIONS, time === OFF ? '' : time).filter(Boolean)
  const dayOpts = withCurrent(DAY_OPTIONS, Number(days))
  const countOpts = withCurrent(COUNT_OPTIONS, Number(count))

  const submit = async () => {
    try {
      await save.mutateAsync({ schedule: time === OFF ? '' : time, keepDays: Number(days), keepCount: Number(count) })
      toast.success(t('common.operateSuccess'))
      onOpenChange(false)
    } catch {
      // 错误 toast 由 http 拦截器统一弹出
    }
  }

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('backup.settings.title')}
      description={t('backup.settings.description')}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button onClick={() => void submit()} disabled={save.isPending}>
            {t('common.save')}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <div className="space-y-2">
          <Label htmlFor="backup-time">{t('backup.settings.schedule')}</Label>
          <Select value={time} onValueChange={setTime}>
            <SelectTrigger id="backup-time">
              <SelectValue />
            </SelectTrigger>
            <SelectContent className="max-h-72">
              <SelectItem value={OFF}>{t('backup.settings.scheduleOff')}</SelectItem>
              {times.map((v) => (
                <SelectItem key={v} value={v}>
                  {t('backup.settings.daily', { time: v })}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">{t('backup.settings.scheduleHint')}</p>
        </div>
        <div className="space-y-2">
          <Label htmlFor="backup-days">{t('backup.settings.keepDays')}</Label>
          <Select value={days} onValueChange={setDays}>
            <SelectTrigger id="backup-days">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {dayOpts.map((v) => (
                <SelectItem key={v} value={String(v)}>
                  {v === 0 ? t('backup.settings.unlimited') : t('backup.settings.days', { count: v })}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-2">
          <Label htmlFor="backup-count">{t('backup.settings.keepCount')}</Label>
          <Select value={count} onValueChange={setCount}>
            <SelectTrigger id="backup-count">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {countOpts.map((v) => (
                <SelectItem key={v} value={String(v)}>
                  {v === 0 ? t('backup.settings.unlimited') : t('backup.settings.copies', { count: v })}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">{t('backup.settings.keepHint')}</p>
        </div>
      </div>
    </ResponsiveDialog>
  )
}
