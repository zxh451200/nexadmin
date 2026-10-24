import { useState } from 'react'
import { X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/framework/ui/badge'
import { Button } from '@/framework/ui/button'
import { Checkbox } from '@/framework/ui/checkbox'
import { Input } from '@/framework/ui/input'
import { useDebounce } from './useDebounce'
import { useRoleOptions, useUserOptions, type Audience } from './api'

interface Props {
  value: Audience
  onChange: (a: Audience) => void
}

const MAX_AUDIENCE = 200

/** 可填写范围：角色与用户的并集；都不选 = 所有拥有填写权限的登录用户 */
export function AudiencePicker({ value, onChange }: Props) {
  const { t } = useTranslation()
  const { data: roles = [] } = useRoleOptions()
  const [search, setSearch] = useState('')
  const keyword = useDebounce(search, 300)
  const { data: found = [] } = useUserOptions(keyword.trim(), keyword.trim() !== '')
  const [names, setNames] = useState<Record<string, string>>({})

  const toggleRole = (id: string, on: boolean) =>
    onChange({ ...value, roles: on ? [...value.roles, id] : value.roles.filter((x) => x !== id) })
  const addUser = (id: string, label: string) => {
    if (value.users.includes(id) || value.users.length >= MAX_AUDIENCE) return
    setNames((n) => ({ ...n, [id]: label }))
    onChange({ ...value, users: [...value.users, id] })
  }

  return (
    <div className="flex flex-col gap-4">
      <p className="text-xs text-muted-foreground">{value.roles.length + value.users.length === 0 ? t('plugin_formbuilder.audience.everyone') : t('plugin_formbuilder.audience.limited')}</p>

      <fieldset className="flex flex-col gap-1">
        <legend className="mb-1 text-sm font-medium">{t('plugin_formbuilder.audience.roles')}</legend>
        <div className="grid grid-cols-1 gap-x-4 sm:grid-cols-2">
          {roles.map((r) => (
            <label key={r.id} className="flex min-h-10 items-center gap-2 text-sm md:min-h-8">
              <Checkbox checked={value.roles.includes(r.id)} onCheckedChange={(c) => toggleRole(r.id, c === true)} />
              <span className="truncate">{r.name}</span>
            </label>
          ))}
        </div>
      </fieldset>

      <div className="flex flex-col gap-2">
        <label htmlFor="fb-aud-user" className="text-sm font-medium">
          {t('plugin_formbuilder.audience.users')}
        </label>
        <Input id="fb-aud-user" value={search} placeholder={t('plugin_formbuilder.audience.searchUser')} onChange={(e) => setSearch(e.target.value)} />
        {found.length > 0 ? (
          <ul className="flex flex-wrap gap-2" aria-label={t('plugin_formbuilder.audience.searchResult')}>
            {found.map((u) => (
              <li key={u.id}>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={value.users.includes(u.id)}
                  onClick={() => addUser(u.id, u.nickname || u.username)}
                >
                  {u.nickname || u.username}
                  <span className="ms-1 text-xs text-muted-foreground">{u.username}</span>
                </Button>
              </li>
            ))}
          </ul>
        ) : null}
        {value.users.length > 0 ? (
          <ul className="flex flex-wrap gap-2">
            {value.users.map((id) => (
              <li key={id}>
                <Badge variant="secondary" className="gap-1 ps-2.5 pe-1">
                  {names[id] ?? id}
                  <button
                    type="button"
                    className="flex size-6 items-center justify-center rounded-sm hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    aria-label={t('plugin_formbuilder.audience.removeUser', { name: names[id] ?? id })}
                    onClick={() => onChange({ ...value, users: value.users.filter((x) => x !== id) })}
                  >
                    <X className="size-3" aria-hidden="true" />
                  </button>
                </Badge>
              </li>
            ))}
          </ul>
        ) : null}
      </div>
    </div>
  )
}
