import { Monitor, Moon, Sun } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/framework/ui/dropdown-menu'
import { useTheme, type ThemeMode, type ThemePreset } from './ThemeProvider'


/** 各预设的样板色：与 tokens.css 里该预设亮/深两套 --primary 对齐。
 *  深色下「默认」是白底主按钮（模板原形态 90% 灰白），色块必须跟着变，
 *  否则「蓝圆点 + 白勾」一行里自相矛盾。 */
const PRESET_SWATCH: Array<{ value: ThemePreset; label: string; light: string; dark: string }> = [
  { value: 'default', label: 'theme.presetDefault', light: 'hsl(238 82% 55%)', dark: 'hsl(0 0% 90%)' },
  { value: 'tangerine', label: 'theme.presetTangerine', light: 'hsl(24 74% 54%)', dark: 'hsl(24 74% 54%)' },
  { value: 'soft-pop', label: 'theme.presetSoftPop', light: 'hsl(262 80% 54%)', dark: 'hsl(262 83% 58%)' },
]

export function ThemeToggle() {
  const { t } = useTranslation()
  const { mode, setMode, resolved, preset, setPreset } = useTheme()

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" aria-label={t('theme.toggle')}>
          {resolved === 'dark' ? <Moon className="h-4 w-4" /> : <Sun className="h-4 w-4" />}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-44">
        <DropdownMenuRadioGroup value={mode} onValueChange={(v) => setMode(v as ThemeMode)}>
          <DropdownMenuRadioItem value="light" indicator="end">
            <Sun className="h-4 w-4" />
            {t('theme.light')}
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="dark" indicator="end">
            <Moon className="h-4 w-4" />
            {t('theme.dark')}
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="system" indicator="end">
            <Monitor className="h-4 w-4" />
            {t('theme.system')}
          </DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>
        <DropdownMenuSeparator />
        <DropdownMenuLabel>{t('theme.preset')}</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={preset} onValueChange={(v) => setPreset(v as ThemePreset)}>
          {PRESET_SWATCH.map((s) => (
            <DropdownMenuRadioItem key={s.value} value={s.value} indicator="end">
              <span
                className="size-4 rounded-full ring-1 ring-foreground/20"
                style={{ background: resolved === 'dark' ? s.dark : s.light }}
                aria-hidden="true"
              />
              {t(s.label)}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
