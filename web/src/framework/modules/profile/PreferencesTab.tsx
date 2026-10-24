import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useLanguages } from '@/framework/i18n/useLanguages'
import { Label } from '@/framework/ui/label'
import { RadioGroup, RadioGroupItem } from '@/framework/ui/radio-group'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { TimezoneSelect } from '@/framework/components/TimezoneSelect'
import { useTheme, type ThemeMode } from '@/framework/theme/ThemeProvider'

const TZ_KEY = 'nex-timezone'
const THEMES: ThemeMode[] = ['light', 'dark', 'system']

function initialTimezone(): string {
  try {
    return localStorage.getItem(TZ_KEY) || Intl.DateTimeFormat().resolvedOptions().timeZone || 'Asia/Shanghai'
  } catch {
    return 'Asia/Shanghai'
  }
}

export function PreferencesTab() {
  const { t } = useTranslation()
  const { languages, current, change } = useLanguages()
  const { mode, setMode } = useTheme()
  const [tz, setTz] = useState(initialTimezone)

  const onTz = (v: string) => {
    setTz(v)
    try {
      localStorage.setItem(TZ_KEY, v)
    } catch {
      // 存储不可用时仅本次生效
    }
    toast.success(t('profile.pref.saved'))
  }

  return (
    <div className="flex max-w-md flex-col gap-6">
      <div className="flex flex-col gap-2">
        <Label>{t('profile.pref.language')}</Label>
        <Select value={current} onValueChange={(v) => void change(v)}>
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {languages.map((l) => (
              <SelectItem key={l.code} value={l.code}>
                {l.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="flex flex-col gap-3">
        <Label id="pref-theme">{t('profile.pref.theme')}</Label>
        <RadioGroup
          aria-labelledby="pref-theme"
          value={mode}
          onValueChange={(v) => setMode(v as ThemeMode)}
          className="grid-cols-1 gap-1 sm:grid-cols-3"
        >
          {THEMES.map((m) => (
            <Label
              key={m}
              htmlFor={`pref-theme-${m}`}
              className="flex min-h-11 cursor-pointer items-center gap-2 rounded-md border border-border bg-card px-3 text-sm font-normal md:min-h-9"
            >
              <RadioGroupItem id={`pref-theme-${m}`} value={m} />
              {t(`profile.pref.${m}`)}
            </Label>
          ))}
        </RadioGroup>
      </div>
      <div className="flex flex-col gap-2">
        <Label>{t('profile.pref.timezone')}</Label>
        <TimezoneSelect value={tz} onChange={onTz} />
      </div>
    </div>
  )
}
