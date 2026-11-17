import { Check, Monitor, Moon, Sun } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/framework/ui/dropdown-menu'
import { useTheme, type ThemeMode, type ThemePreset } from './ThemeProvider'


/** 各预设的样板色：与 tokens.css / [data-preset] 里的 --primary 对齐 */
const PRESET_SWATCH: Array<{ value: ThemePreset; label: string; color: string }> = [
  { value: 'default', label: 'theme.presetDefault', color: 'hsl(238 82% 55%)' },
  { value: 'tangerine', label: 'theme.presetTangerine', color: 'hsl(24 74% 54%)' },
  { value: 'soft-pop', label: 'theme.presetSoftPop', color: 'hsl(262 80% 54%)' },
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
          <DropdownMenuRadioItem value="light">
            <Sun className="me-2 h-4 w-4" />
            {t('theme.light')}
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="dark">
            <Moon className="me-2 h-4 w-4" />
            {t('theme.dark')}
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="system">
            <Monitor className="me-2 h-4 w-4" />
            {t('theme.system')}
          </DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>
        <DropdownMenuSeparator />
        <DropdownMenuLabel>{t('theme.preset')}</DropdownMenuLabel>
        {PRESET_SWATCH.map((s) => (
          <DropdownMenuItem key={s.value} className="justify-between" onSelect={() => setPreset(s.value)}>
            <span className="flex items-center gap-2">
              <span className="size-3.5 rounded-full ring-1 ring-foreground/20" style={{ background: s.color }} aria-hidden="true" />
              {t(s.label)}
            </span>
            {preset === s.value && <Check className="size-4 text-foreground" aria-hidden="true" />}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
