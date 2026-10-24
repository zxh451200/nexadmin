import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { Languages, LogOut, SunMoon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '@/framework/auth/useAuth'
import { useLanguages } from '@/framework/i18n/useLanguages'
import type { MenuNode } from '@/framework/router/types'
import { useTheme } from '@/framework/theme/ThemeProvider'
import { CommandDialog, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList, CommandSeparator } from '@/framework/ui/command'
import { DialogDescription, DialogTitle } from '@/framework/ui/dialog'
import { DynamicIcon } from './DynamicIcon'

interface CommandMenuProps {
  menu: MenuNode[]
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function CommandMenu({ menu, open, onOpenChange }: CommandMenuProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const logout = useAuth((s) => s.logout)
  const { resolved, setMode } = useTheme()
  const { languages, current, change } = useLanguages()

  // Cmd/Ctrl + K 开关
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        onOpenChange(!open)
      }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open, onOpenChange])

  const run = (fn: () => void | Promise<void>) => {
    onOpenChange(false)
    void fn()
  }

  const nextLang = () => {
    if (languages.length <= 1) return
    const i = languages.findIndex((l) => l.code === current)
    void change(languages[(i + 1) % languages.length].code)
  }

  return (
    <CommandDialog open={open} onOpenChange={onOpenChange}>
      <DialogTitle className="sr-only">{t('cmdk.title')}</DialogTitle>
      <DialogDescription className="sr-only">{t('cmdk.description')}</DialogDescription>
      <CommandInput placeholder={t('cmdk.placeholder')} aria-label={t('cmdk.inputLabel')} />
      <CommandList>
        <CommandEmpty>{t('cmdk.empty')}</CommandEmpty>
        {menu.map((group) => {
          const leaves = (group.children ?? []).filter((c) => c.path)
          if (leaves.length === 0) return null
          const groupTitle = group.title ? t(group.title) : ''
          return (
            <CommandGroup key={group.key} heading={groupTitle || undefined}>
              {leaves.map((item) => {
                const title = t(item.title)
                return (
                  <CommandItem
                    key={item.key}
                    value={`${groupTitle} ${title} ${item.path}`}
                    onSelect={() => run(() => navigate(item.path!))}
                  >
                    <DynamicIcon name={item.icon} aria-hidden="true" />
                    <span className="truncate">{title}</span>
                  </CommandItem>
                )
              })}
            </CommandGroup>
          )
        })}
        <CommandSeparator />
        <CommandGroup heading={t('cmdk.actions')}>
          <CommandItem value={t('cmdk.toggleTheme')} onSelect={() => run(() => setMode(resolved === 'dark' ? 'light' : 'dark'))}>
            <SunMoon aria-hidden="true" />
            <span>{t('cmdk.toggleTheme')}</span>
          </CommandItem>
          {languages.length > 1 && (
            <CommandItem value={t('cmdk.toggleLanguage')} onSelect={() => run(nextLang)}>
              <Languages aria-hidden="true" />
              <span>{t('cmdk.toggleLanguage')}</span>
            </CommandItem>
          )}
          <CommandItem
            value={t('cmdk.logout')}
            onSelect={() =>
              run(async () => {
                await logout()
                navigate('/login', { replace: true })
              })
            }
          >
            <LogOut aria-hidden="true" />
            <span>{t('cmdk.logout')}</span>
          </CommandItem>
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  )
}
