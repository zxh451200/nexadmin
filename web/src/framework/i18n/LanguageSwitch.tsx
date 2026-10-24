import { Check, Languages } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/framework/ui/dropdown-menu'
import { useLanguages } from './useLanguages'

export function LanguageSwitch() {
  const { t } = useTranslation()
  const { languages, current, change } = useLanguages()

  if (languages.length <= 1) return null

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" className="size-11 md:size-9" aria-label={t('layout.language')}>
          <Languages className="size-5 md:size-4" aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {languages.map((l) => (
          <DropdownMenuItem key={l.code} className="min-h-11 justify-between gap-4 md:min-h-8" onSelect={() => change(l.code)}>
            <span>{l.name}</span>
            {l.code === current && <Check className="size-4 text-primary" aria-hidden="true" />}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
