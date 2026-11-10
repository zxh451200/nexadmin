import { useEffect, useRef } from 'react'
import Viewer from 'viewerjs'
import 'viewerjs/dist/viewer.css'

export interface InlineViewerProps {
  src: string
  alt?: string
  /** 看图器容器尺寸：viewer 填满容器，图片按 viewMode 1 缩放进容器 */
  className?: string
}

/**
 * 内嵌看图器（viewerjs inline 模式）：缩放、旋转、1:1、重置，不铺满视口。
 * 与全屏灯箱（Lightbox.tsx，yet-another-react-lightbox）分工：
 * 这里给「小图标放大看一眼」，要翻页看大图细节的仍走灯箱。
 */
export function InlineViewer({ src, alt = '', className }: InlineViewerProps) {
  const boxRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const box = boxRef.current
    if (!box) return
    const viewer = new Viewer(box, {
      inline: true,
      // 初始把图缩到容器的 90% 完整显示；不设的话大图会按原始尺寸铺开，小弹窗装不下
      initialCoverage: 0.9,
      navbar: false,
      title: false,
      // 关掉右上角按钮：inline 模式下它是全屏切换，这个字段要的就是「别全屏」
      button: false,
      toolbar: { prev: false, play: false, next: false, flipHorizontal: false, flipVertical: false },
    })
    return () => {
      viewer.destroy()
    }
  }, [src])
  return (
    <div ref={boxRef} className={className}>
      <img src={src} alt={alt} />
    </div>
  )
}
