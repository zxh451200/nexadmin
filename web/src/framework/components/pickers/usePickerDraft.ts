import { useState } from 'react'

export interface PickerDraft<T> {
  /** 弹层开关；打开时自动从当前值拷贝草稿（传给 PickerShell 的 onOpenChange 与手动关闭都用它） */
  open: boolean
  setOpen: (open: boolean) => void
  /** 未打开过时为 undefined，调用方用可选链读取 */
  draft: T | undefined
  setDraft: (draft: T | undefined) => void
}

/** 弹层选择器的「草稿」模式：选中只改草稿，点确认才落值；每次打开时用 make 从当前值现拷一份
    （值可能在关闭期间被外部改掉，不能只在首次渲染算），取消/点击遮罩关闭即弃。 */
export function usePickerDraft<T>(make: () => T): PickerDraft<T> {
  const [open, setOpenRaw] = useState(false)
  const [draft, setDraft] = useState<T>()
  const setOpen = (o: boolean) => {
    setOpenRaw(o)
    if (o) setDraft(make())
  }
  return { open, setOpen, draft, setDraft }
}
