import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

export interface TileAction {
  key: string
  /** 读屏与悬停提示都用它（同一个字符串） */
  label: string
  icon: ReactNode
  onSelect: () => void
  destructive?: boolean
  disabled?: boolean
}

/**
 * 图块（单图字段 / 图集 / 视频块）上的操作按钮：全站只有这一套。
 *
 * - 位置固定右上角：右下角在视频块上会压住原生播放控件，左下角在宫格里又和文件名打架；
 *   右上角与左上角的拖拽手柄对称，两者都不挡画面主体。
 * - 悬停或键盘聚焦时浮出，隐藏时不接事件（否则透明按钮会偷走「点图放大」的那一下）。
 * - 触屏没有 hover（hover:none）时始终显示，否则手机上这些按钮永远摸不到。
 *
 * 之前单图字段是「整块黑色蒙版居中两枚图标」、图集是「底部常驻白条 + 替换/删除」，
 * 同一种操作两副样子，还都盖住画面——统一到这里。
 */
export function TileActions({ actions, className }: { actions: TileAction[]; className?: string }) {
  if (actions.length === 0) return null
  return (
    <div
      className={cn(
        'pointer-events-none absolute inset-e-1 top-1 z-10 flex gap-1 opacity-0 transition-opacity duration-150',
        'group-focus-within:pointer-events-auto group-focus-within:opacity-100',
        'group-hover:pointer-events-auto group-hover:opacity-100',
        '[@media(hover:none)]:pointer-events-auto [@media(hover:none)]:opacity-100',
        className,
      )}
    >
      {actions.map((a) => (
        <button
          key={a.key}
          type="button"
          aria-label={a.label}
          title={a.label}
          disabled={a.disabled}
          onClick={a.onSelect}
          className={cn(
            'inline-flex size-8 items-center justify-center rounded-md bg-black/55 text-white shadow-xs backdrop-blur-sm transition-colors hover:bg-black/75 focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50 md:size-7',
            a.destructive && 'hover:bg-destructive',
          )}
        >
          {a.icon}
        </button>
      ))}
    </div>
  )
}
