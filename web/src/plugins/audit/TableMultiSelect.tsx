import { useState } from 'react'
import { Check, ChevronsUpDown, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from '@/framework/ui/command'
import { Popover, PopoverContent, PopoverTrigger } from '@/framework/ui/popover'

interface Props {
  options: string[]
  value: string[]
  onChange: (value: string[]) => void
  disabled?: boolean
  'aria-labelledby'?: string
}

/** 表名多选：可搜索的下拉列表 + 已选标签（点 x 移除）。 */
export function TableMultiSelect({ options, value, onChange, disabled, 'aria-labelledby': labelledBy }: Props) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const selected = new Set(value)

  const toggle = (name: string) => {
    const next = new Set(selected)
    if (next.has(name)) next.delete(name)
    else next.add(name)
    onChange(options.filter((o) => next.has(o)))
  }

  return (
    <div className="flex flex-col gap-3">
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button
            type="button"
            variant="outline"
            role="combobox"
            aria-expanded={open}
            aria-labelledby={labelledBy}
            disabled={disabled}
            className="w-full justify-between font-normal md:w-96"
          >
            <span className={value.length ? 'text-foreground' : 'text-muted-foreground'}>
              {value.length ? t('plugin_audit.scope.selectedCount', { count: value.length }) : t('plugin_audit.scope.placeholder')}
            </span>
            <ChevronsUpDown className="ms-2 h-4 w-4 shrink-0 opacity-50" aria-hidden="true" />
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-[--radix-popover-trigger-width] min-w-64 p-0" align="start">
          <Command>
            <CommandInput placeholder={t('plugin_audit.scope.search')} />
            <CommandList>
              <CommandEmpty>{t('plugin_audit.scope.noTables')}</CommandEmpty>
              <CommandGroup>
                {options.map((name) => (
                  <CommandItem key={name} value={name} onSelect={() => toggle(name)}>
                    <Check className={selected.has(name) ? 'h-4 w-4 opacity-100' : 'h-4 w-4 opacity-0'} aria-hidden="true" />
                    <span className="font-mono text-xs">{name}</span>
                  </CommandItem>
                ))}
              </CommandGroup>
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>

      {value.length ? (
        <div className="flex flex-wrap gap-2">
          {value.map((name) => (
            <Badge key={name} variant="secondary" className="gap-1 font-mono">
              {name}
              {disabled ? null : (
                <button
                  type="button"
                  className="inline-flex h-4 w-4 items-center justify-center rounded-sm hover:bg-muted"
                  aria-label={t('plugin_audit.scope.remove', { name })}
                  onClick={() => toggle(name)}
                >
                  <X className="h-3 w-3" aria-hidden="true" />
                </button>
              )}
            </Badge>
          ))}
        </div>
      ) : null}
    </div>
  )
}
