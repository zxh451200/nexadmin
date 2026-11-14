import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Input } from '@/framework/ui/input'
import { cn } from '@/lib/utils'
import { RegionSelect } from './RegionSelect'
import { parseRegionAddress } from './pickers/parseRegion'
import { findRegionPath, useRegionTree, type RegionValue } from './pickers/useRegionTree'
import type { RegionNode } from '@/framework/request/types'

/** 地址复合值：region 是省市区编码，address 是详细地址（持久化的自由文本）。
    完整地址不落库，由区划树 + address 推导。 */
export interface AddressValue {
  region?: RegionValue
  address?: string
}

export interface AddressFieldProps {
  value?: AddressValue | null
  onChange?: (value: AddressValue) => void
  /** 区划层级，默认 3（省/市/区县） */
  level?: 2 | 3
  disabled?: boolean
  className?: string
}

function toRegionValue(path: RegionNode[]): RegionValue {
  return { province: path[0]?.code, city: path[1]?.code, district: path[2]?.code }
}

function sameRegion(a?: RegionValue, b?: RegionValue): boolean {
  return a?.province === b?.province && a?.city === b?.city && a?.district === b?.district
}

/**
 * 顺丰/电商式地址字段：省市区选择 + 详细地址 + 完整地址三段联动。
 * 往完整地址里输入（如「中牟散大声地111」）会**边打边解析**：区划选择器实时跟进
 *（区县名认短别名“中牟”，不带省市时按全树唯一命中补全），详细地址等失焦时再回填，
 * 避免打字过程中输入框内容被推导值顶掉；粘贴一段完整地址（如「广西北海市海城区XX路」）
 * 立即解析回填，剩余文本落到详细地址；解析不命中时整串进详细地址，不丢内容。
 */
export function AddressField({ value, onChange, level = 3, disabled, className }: AddressFieldProps) {
  const { t } = useTranslation()
  const { data: tree = [] } = useRegionTree(level)
  // 完整地址输入框的本地草稿：null = 跟随推导值；用户手动编辑后暂存，失焦/粘贴时解析回填
  const [fullDraft, setFullDraft] = useState<string | null>(null)
  // 自己 emit 出去的值（打字联动时 input 受控内容不能被推导值顶掉）；
  // 「外部改动」（区划选择、详细地址输入）与「完整地址打字引发的联动」用它区分。
  const emitted = useRef(value ?? null)
  const fromFull = useRef(false)

  const region = value?.region
  const address = value?.address ?? ''
  const fullComputed = findRegionPath(tree, region).map((n) => n.name).join('') + address

  useEffect(() => {
    const prev = emitted.current
    const changed =
      !sameRegion(value?.region, prev?.region) || (value?.address ?? '') !== (prev?.address ?? '')
    if (changed) {
      if (!fromFull.current) setFullDraft(null) // 外部改动：完整地址回到推导值
      fromFull.current = false
      emitted.current = value ?? null
    }
  // 只盯这几个字段：完整地址打字联动引发的 region/address 变化不把草稿顶掉（emitted 已记账），
  // eslint 覆盖依赖数这里是有意为之
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value?.region?.province, value?.region?.city, value?.region?.district, value?.address])

  const emit = (v: AddressValue, fromFullInput = false) => {
    fromFull.current = fromFullInput
    emitted.current = v
    onChange?.({ region: v.region, address: v.address })
  }

  const applyParsed = (text: string) => {
    const clean = text.trim()
    if (!clean) return
    const { path, rest } = parseRegionAddress(tree, clean, level)
    emit({ region: path.length ? toRegionValue(path) : region, address: rest })
    setFullDraft(null)
  }

  // 打字过程的实时反馈：只把区划选择器跟上去，详细地址留到失焦再回填
  const liveParseRegion = (text: string) => {
    const { path } = parseRegionAddress(tree, text.trim(), level)
    if (path.length && !sameRegion(toRegionValue(path), region)) {
      emit({ region: toRegionValue(path), address }, true)
    }
  }

  return (
    <div className={cn('flex flex-col gap-3', className)}>
      <RegionSelect
        level={level}
        value={region ?? null}
        disabled={disabled}
        onChange={(r) => emit({ region: r, address })}
      />
      <label className="flex flex-col gap-1 text-xs text-muted-foreground">
        {t('picker.addressDetail')}
        <Input
          value={address}
          disabled={disabled}
          maxLength={255}
          onChange={(e) => emit({ region, address: e.target.value })}
        />
      </label>
      <label className="flex flex-col gap-1 text-xs text-muted-foreground">
        {t('picker.addressFull')}
        <Input
          value={fullDraft ?? fullComputed}
          disabled={disabled}
          maxLength={512}
          placeholder={t('picker.addressPasteHint')}
          onChange={(e) => {
            const text = e.target.value
            setFullDraft(text)
            liveParseRegion(text)
          }}
          onPaste={(e) => {
            const text = e.clipboardData.getData('text')
            if (!text.trim()) return
            e.preventDefault()
            applyParsed(text)
          }}
          onBlur={() => {
            if (fullDraft != null) applyParsed(fullDraft)
          }}
        />
      </label>
    </div>
  )
}
