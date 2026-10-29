import { useTranslation } from 'react-i18next'
import { ChevronDown, Globe } from 'lucide-react'
import { Button } from '@/framework/ui/button'
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/framework/ui/dropdown-menu'
import { cn } from '@/lib/utils'
import { useLocaleForm } from './LocaleFormProvider'

/**
 * 完成度：只在缺的时候说缺几项，填满了什么都不显示。
 * 不打勾——菜单项右侧的勾会被读成「这一项被选中」，而下面发布开关用的也是勾（意思是已发布）；
 * 也不写「3/3」，那要读者先猜分母是什么。每一行没标记就是填完了。
 */
function Progress({ done, total }: { done: number; total: number }) {
  const { t } = useTranslation()
  if (done >= total) return null
  return (
    <span className="text-xs text-muted-foreground">
      {t('localeForm.fieldsMissing', { count: total - done })}
    </span>
  )
}

/**
 * 语言切换器：整表换语言形态的唯一入口。
 * 用下拉而不是 tab —— 语言再加十种也只是菜单多几行，表单高度一点不变。
 */
export function LocaleSwitcher({ className }: { className?: string }) {
  const { t } = useTranslation()
  const {
    locales,
    defaultLocale,
    active,
    setActive,
    completion,
    allowPublish,
    publishable,
    publishedOf,
    setPublishedOf,
    fieldLabel,
  } = useLocaleForm()

  const source = locales.find((l) => l.code === defaultLocale)
  const rest = locales.filter((l) => l.code !== defaultLocale)
  // 只有原文一种语言时没有可切换的对象，整个控件消失（与改造前的隐藏条件一致）。
  if (!rest.length) return null

  const labelOf = (code: string) => (code === defaultLocale ? t('localeForm.sourceLang') : (locales.find((l) => l.code === code)?.label ?? code))
  const masterOn = publishedOf(defaultLocale)
  const activeCount = completion(active)

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          variant="outline"
          className={cn('min-h-11 gap-2 font-normal md:h-9 md:min-h-0', className)}
          aria-label={t('localeForm.switcherAria')}
        >
          <Globe className="size-4 text-muted-foreground" aria-hidden="true" />
          <span className="text-xs text-muted-foreground">{t('localeForm.editingLanguage')}</span>
          <span className="font-medium">{labelOf(active)}</span>
          {active !== defaultLocale ? <Progress done={activeCount.done} total={activeCount.total} /> : null}
          <ChevronDown className="size-3.5 text-muted-foreground" aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>

      <DropdownMenuContent align="start" className="w-64">
        <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">
          {t('localeForm.pickHint')}
        </DropdownMenuLabel>
        <DropdownMenuRadioGroup value={active} onValueChange={setActive}>
          {source ? (
            <DropdownMenuRadioItem value={source.code}>
              {labelOf(source.code)}
            </DropdownMenuRadioItem>
          ) : null}
          {rest.map((l) => {
            const c = completion(l.code)
            return (
              <DropdownMenuRadioItem key={l.code} value={l.code} className="gap-2">
                <span className="truncate">{l.label}</span>
                <span className="ms-auto flex justify-end">
                  <Progress done={c.done} total={c.total} />
                </span>
              </DropdownMenuRadioItem>
            )
          })}
        </DropdownMenuRadioGroup>

        {/* 整篇还没发布时，这里没有任何可操作的东西（勾是禁用的），整块不出现；
            发布之后再自己长出来。 */}
        {allowPublish && masterOn ? (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">
              {t('localeForm.publishGroup')}
            </DropdownMenuLabel>
            {rest.map((l) => {
              const { ok, missing } = publishable(l.code)
              const on = publishedOf(l.code)
              // 已经发布过、后来被清空的译文要能关掉，不能一起锁死。
              const blocked = !ok && !on
              return (
                <DropdownMenuCheckboxItem
                  key={l.code}
                  checked={on}
                  disabled={blocked}
                  onCheckedChange={(next) => setPublishedOf(l.code, next)}
                  onSelect={(e) => e.preventDefault()}
                  className="gap-2"
                >
                  <span className="truncate">{l.label}</span>
                  {blocked ? (
                    <span className="ms-auto max-w-36 truncate text-[10px] text-muted-foreground">
                      {t('localeForm.publishMissing', { fields: missing.map(fieldLabel).join('、') })}
                    </span>
                  ) : null}
                </DropdownMenuCheckboxItem>
              )
            })}
          </>
        ) : null}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
