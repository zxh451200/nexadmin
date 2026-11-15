import { useMemo, useState } from 'react'
import { Plus, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/framework/ui/button'
import { Input } from '@/framework/ui/input'
import { Label } from '@/framework/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/framework/ui/select'
import { Textarea } from '@/framework/ui/textarea'
import { emptyAliTrade, type AliTrade } from './api'

/** 物流属性选项：官方 3.6.5 的 option value（普货与带电属性不能同时选，由阿里侧校验）。 */
const LOGISTICS_OPTIONS = [
  { value: 'battery.battery.oneLessHundredWh', label: '纯电池 ≤100Wh' },
  { value: 'battery.battery.oneMoreHundredWH', label: '纯电池 >100Wh' },
  { value: 'battery.builtinBatteryLithium.oneLessHundredWh', label: '内置锂电池 ≤100Wh' },
  { value: 'battery.builtinBatteryLithium.oneMoreHundredWH', label: '内置锂电池 >100Wh' },
  { value: 'battery.attachBatteryLithium.oneLessHundredWh', label: '配套锂电池 ≤100Wh' },
  { value: 'battery.attachBatteryLithium.oneMoreHundredWH', label: '配套锂电池 >100Wh' },
  { value: 'liquid.oilMakeupNonAlcohol.oneLessHundredMl', label: '油状化妆品 ≤100ml' },
  { value: 'liquid.creamMakeupNonAlcohol.oneLessHundredMl', label: '膏状化妆品 ≤100ml' },
  { value: 'liquid.waterMakeupNonAlcohol.oneLessHundredMl', label: '水状化妆品 ≤100ml' },
  { value: 'special.foodAndBeverage.inflatedSnackBag', label: '食品：非充气零食' },
  { value: 'special.dinnerKnives.unedged', label: '餐刀未开刃' },
]

/** 解析库里存的 aliTradeJson（坏数据当空，不让一条脏 JSON 挡住编辑）。 */
export function safeParseAliTrade(raw?: string | null): AliTrade {
  const base = emptyAliTrade()
  if (!raw) return base
  try {
    const parsed = JSON.parse(raw) as Partial<AliTrade>
    return {
      ...base,
      ...parsed,
      package: { ...base.package, ...(parsed.package ?? {}) },
      supply: { ...base.supply, ...(parsed.supply ?? {}) },
      sample: { ...base.sample, ...(parsed.sample ?? {}) },
      ladder: parsed.ladder ?? [],
      logistics: parsed.logistics ?? [],
      custom: parsed.custom ?? [],
      group: parsed.group ?? [],
      skuProps: parsed.skuProps ?? [],
      skus: parsed.skus ?? [],
    }
  } catch {
    return base
  }
}

function Row({ children }: { children: React.ReactNode }) {
  return <div className="flex flex-wrap items-end gap-2">{children}</div>
}

function Num({
  label,
  value,
  onChange,
  disabled,
  width = 'w-24',
}: {
  label: string
  value: number
  onChange: (v: number) => void
  disabled?: boolean
  width?: string
}) {
  return (
    <div className={`flex flex-col gap-1 ${width}`}>
      <Label className="text-xs text-muted-foreground">{label}</Label>
      <Input
        type="number"
        inputMode="decimal"
        value={value || ''}
        disabled={disabled}
        onChange={(e) => onChange(Number(e.target.value) || 0)}
        aria-label={label}
      />
    </div>
  )
}

/**
 * 阿里发品用的交易与规格：阶梯价、包装、供货能力、物流属性、样品、定制、分组、SKU。
 * 这些字段只在同步/预览/阿里模板导出时生效，不影响官网详情页；与阿里后台发品表单的分区一一对应。
 */
export function AliTradeFields({
  value,
  onChange,
  disabled,
}: {
  value: AliTrade
  onChange: (v: AliTrade) => void
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const patch = (part: Partial<AliTrade>) => onChange({ ...value, ...part })
  // 注意：SKU 文本框的初值只在挂载时取一次；父组件用 key={商品 id} 重挂本组件来切换商品。
  // SKU 是「属性 × 值」的表格，表单里塞不下：用 JSON 编辑，失焦时校验并回写
  const skuJSON = useMemo(
    () => JSON.stringify({ skuProps: value.skuProps, skus: value.skus }, null, 2),
    [value.skuProps, value.skus],
  )
  const [skuText, setSkuText] = useState(skuJSON)
  const [skuError, setSkuError] = useState('')
  const applySku = (text: string) => {
    setSkuText(text)
    try {
      const parsed = JSON.parse(text || '{}') as Partial<AliTrade>
      setSkuError('')
      onChange({ ...value, skuProps: parsed.skuProps ?? [], skus: parsed.skus ?? [] })
    } catch {
      setSkuError(t('plugin_website.ali.trade.skuJsonInvalid'))
    }
  }
  const toggleLogistics = (v: string) => {
    const has = value.logistics.includes(v)
    patch({ logistics: has ? value.logistics.filter((x) => x !== v) : [...value.logistics, v] })
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-1.5">
        <Label>{t('plugin_website.ali.trade.priceType')}</Label>
        <Select
          value={value.priceType || 'auto'}
          onValueChange={(v) => patch({ priceType: v === 'auto' ? '' : (v as AliTrade['priceType']) })}
          disabled={disabled}
        >
          <SelectTrigger className="w-64" aria-label={t('plugin_website.ali.trade.priceType')}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="auto">{t('plugin_website.ali.trade.priceAuto')}</SelectItem>
            <SelectItem value="fob">{t('plugin_website.ali.trade.priceFob')}</SelectItem>
            <SelectItem value="ladder">{t('plugin_website.ali.trade.priceLadder')}</SelectItem>
            <SelectItem value="sku">{t('plugin_website.ali.trade.priceSku')}</SelectItem>
          </SelectContent>
        </Select>
        <p className="text-xs text-muted-foreground">{t('plugin_website.ali.trade.priceTypeHint')}</p>
      </div>

      <div className="flex flex-col gap-2">
        <Label>{t('plugin_website.ali.trade.ladder')}</Label>
        {value.ladder.map((l, i) => (
          <Row key={i}>
            <Num
              label={t('plugin_website.ali.trade.ladderQty')}
              value={l.qty}
              disabled={disabled}
              onChange={(v) => patch({ ladder: value.ladder.map((x, j) => (j === i ? { ...x, qty: v } : x)) })}
            />
            <Num
              label={t('plugin_website.ali.trade.ladderPrice')}
              value={l.price}
              width="w-32"
              disabled={disabled}
              onChange={(v) => patch({ ladder: value.ladder.map((x, j) => (j === i ? { ...x, price: v } : x)) })}
            />
            <Button
              variant="ghost"
              size="icon"
              className="text-destructive"
              disabled={disabled}
              aria-label={t('common.delete')}
              onClick={() => patch({ ladder: value.ladder.filter((_, j) => j !== i) })}
            >
              <X aria-hidden="true" />
            </Button>
          </Row>
        ))}
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="self-start text-muted-foreground"
          disabled={disabled || value.ladder.length >= 4}
          onClick={() => patch({ ladder: [...value.ladder, { qty: 0, price: 0 }] })}
        >
          <Plus aria-hidden="true" />
          {t('plugin_website.ali.trade.ladderAdd')}
        </Button>
      </div>

      <div className="flex flex-col gap-2">
        <Label>{t('plugin_website.ali.trade.package')}</Label>
        <Row>
          <Num label={t('plugin_website.ali.trade.pkgLength')} value={value.package.length} disabled={disabled} onChange={(v) => patch({ package: { ...value.package, length: v } })} />
          <Num label={t('plugin_website.ali.trade.pkgWidth')} value={value.package.width} disabled={disabled} onChange={(v) => patch({ package: { ...value.package, width: v } })} />
          <Num label={t('plugin_website.ali.trade.pkgHeight')} value={value.package.height} disabled={disabled} onChange={(v) => patch({ package: { ...value.package, height: v } })} />
          <Num label={t('plugin_website.ali.trade.pkgWeight')} value={value.package.weight} disabled={disabled} onChange={(v) => patch({ package: { ...value.package, weight: v } })} />
          <div className="flex min-w-40 flex-1 flex-col gap-1">
            <Label className="text-xs text-muted-foreground">{t('plugin_website.ali.trade.shippingTemplate')}</Label>
            <Input
              value={value.package.shippingTemplate}
              disabled={disabled}
              onChange={(e) => patch({ package: { ...value.package, shippingTemplate: e.target.value } })}
              placeholder="2008472002"
              aria-label={t('plugin_website.ali.trade.shippingTemplate')}
            />
          </div>
        </Row>
      </div>

      <div className="flex flex-col gap-2">
        <Label>{t('plugin_website.ali.trade.supply')}</Label>
        <Row>
          <Num label={t('plugin_website.ali.trade.supplyNumber')} value={value.supply.number} width="w-32" disabled={disabled} onChange={(v) => patch({ supply: { ...value.supply, number: v } })} />
          <div className="flex w-40 flex-col gap-1">
            <Label className="text-xs text-muted-foreground">{t('plugin_website.ali.trade.supplyUnit')}</Label>
            <Input
              value={value.supply.quantityUnit}
              disabled={disabled}
              onChange={(e) => patch({ supply: { ...value.supply, quantityUnit: e.target.value } })}
              placeholder="Set/Sets"
              aria-label={t('plugin_website.ali.trade.supplyUnit')}
            />
          </div>
          <div className="flex w-32 flex-col gap-1">
            <Label className="text-xs text-muted-foreground">{t('plugin_website.ali.trade.supplyPeriod')}</Label>
            <Input
              value={value.supply.timeUnit}
              disabled={disabled}
              onChange={(e) => patch({ supply: { ...value.supply, timeUnit: e.target.value } })}
              placeholder="Month"
              aria-label={t('plugin_website.ali.trade.supplyPeriod')}
            />
          </div>
        </Row>
      </div>

      <div className="flex flex-col gap-2">
        <Label>{t('plugin_website.ali.trade.logistics')}</Label>
        <div className="flex flex-wrap gap-1.5">
          {LOGISTICS_OPTIONS.map((o) => {
            const on = value.logistics.includes(o.value)
            return (
              <Button
                key={o.value}
                type="button"
                size="sm"
                variant={on ? 'default' : 'outline'}
                className="h-7 px-2 text-xs"
                disabled={disabled}
                aria-pressed={on}
                onClick={() => toggleLogistics(o.value)}
              >
                {o.label}
              </Button>
            )
          })}
        </div>
        <p className="text-xs text-muted-foreground">{t('plugin_website.ali.trade.logisticsHint')}</p>
      </div>

      <div className="flex flex-col gap-2">
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            className="size-4 accent-[hsl(var(--primary))]"
            checked={value.sample.enabled}
            disabled={disabled}
            onChange={(e) => patch({ sample: { ...value.sample, enabled: e.target.checked } })}
          />
          {t('plugin_website.ali.trade.sample')}
        </label>
        {value.sample.enabled ? (
          <Row>
            <Num label={t('plugin_website.ali.trade.sampleQty')} value={value.sample.maxQty} disabled={disabled} onChange={(v) => patch({ sample: { ...value.sample, maxQty: v } })} />
            <Num label={t('plugin_website.ali.trade.samplePrice')} value={value.sample.price} disabled={disabled} onChange={(v) => patch({ sample: { ...value.sample, price: v } })} />
            <div className="flex min-w-40 flex-1 flex-col gap-1">
              <Label className="text-xs text-muted-foreground">{t('plugin_website.ali.trade.sampleDesc')}</Label>
              <Input
                value={value.sample.description}
                disabled={disabled}
                onChange={(e) => patch({ sample: { ...value.sample, description: e.target.value } })}
                aria-label={t('plugin_website.ali.trade.sampleDesc')}
              />
            </div>
          </Row>
        ) : null}
      </div>

      <div className="flex flex-col gap-2">
        <Label>{t('plugin_website.ali.trade.custom')}</Label>
        {value.custom.map((c, i) => (
          <Row key={i}>
            <div className="flex min-w-40 flex-1 flex-col gap-1">
              <Input
                value={c.type}
                disabled={disabled}
                onChange={(e) => patch({ custom: value.custom.map((x, j) => (j === i ? { ...x, type: e.target.value } : x)) })}
                placeholder="Customized logo"
                aria-label={t('plugin_website.ali.trade.customType')}
              />
            </div>
            <Num
              label={t('plugin_website.ali.trade.customQty')}
              value={c.quantity}
              disabled={disabled}
              onChange={(v) => patch({ custom: value.custom.map((x, j) => (j === i ? { ...x, quantity: v } : x)) })}
            />
            <Button
              variant="ghost"
              size="icon"
              className="text-destructive"
              disabled={disabled}
              aria-label={t('common.delete')}
              onClick={() => patch({ custom: value.custom.filter((_, j) => j !== i) })}
            >
              <X aria-hidden="true" />
            </Button>
          </Row>
        ))}
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="self-start text-muted-foreground"
          disabled={disabled}
          onClick={() => patch({ custom: [...value.custom, { type: '', quantity: 0 }] })}
        >
          <Plus aria-hidden="true" />
          {t('plugin_website.ali.trade.customAdd')}
        </Button>
      </div>

      <div className="flex flex-col gap-2">
        <Label>{t('plugin_website.ali.trade.group')}</Label>
        <Row>
          {[0, 1, 2].map((i) => (
            <div key={i} className="flex min-w-32 flex-1 flex-col gap-1">
              <Input
                value={value.group[i] ?? ''}
                disabled={disabled}
                onChange={(e) => {
                  const g = [...value.group]
                  g[i] = e.target.value
                  // 上级为空时下级没有意义：裁掉后面的空位
                  patch({ group: g.slice(0, i + 1).filter((v, idx) => idx < i || v.trim() !== '') })
                }}
                placeholder={t('plugin_website.ali.trade.groupLevel', { level: i + 1 })}
                aria-label={t('plugin_website.ali.trade.groupLevel', { level: i + 1 })}
              />
            </div>
          ))}
        </Row>
      </div>

      <div className="flex flex-col gap-1.5">
        <Label>{t('plugin_website.ali.trade.sku')}</Label>
        <Textarea
          value={skuText}
          rows={6}
          disabled={disabled}
          spellCheck={false}
          className="font-mono text-xs"
          aria-label={t('plugin_website.ali.trade.sku')}
          onChange={(e) => applySku(e.target.value)}
        />
        {skuError ? <p className="text-xs text-destructive">{skuError}</p> : <p className="text-xs text-muted-foreground">{t('plugin_website.ali.trade.skuHint')}</p>}
      </div>
    </div>
  )
}