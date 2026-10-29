/** 浏览器端把图片转成 WebP（保留原图，仅生成副本）；不支持编码或解码失败时返回 null。 */

const SKIP_TYPES = ['image/gif', 'image/svg+xml', 'image/webp', 'image/avif']

let webpEncode: boolean | null = null

function canEncodeWebp(): boolean {
  if (webpEncode === null) {
    const canvas = document.createElement('canvas')
    canvas.width = 1
    canvas.height = 1
    webpEncode = canvas.toDataURL('image/webp').startsWith('data:image/webp')
  }
  return webpEncode
}

/** 最长边超过 maxEdge 时等比缩小；EXIF 方向由 createImageBitmap 自动摆正，避免转完躺倒。 */
export async function toWebp(file: File, maxEdge = 2560, quality = 0.82): Promise<Blob | null> {
  if (SKIP_TYPES.includes(file.type) || !file.type.startsWith('image/') || !canEncodeWebp()) return null
  const bitmap = await createImageBitmap(file, { imageOrientation: 'from-image' }).catch(() => null)
  if (!bitmap) return null
  try {
    const scale = Math.min(1, maxEdge / Math.max(bitmap.width, bitmap.height))
    const width = Math.max(1, Math.round(bitmap.width * scale))
    const height = Math.max(1, Math.round(bitmap.height * scale))
    const canvas = document.createElement('canvas')
    canvas.width = width
    canvas.height = height
    const ctx = canvas.getContext('2d')
    if (!ctx) return null
    ctx.drawImage(bitmap, 0, 0, width, height)
    return await new Promise<Blob | null>((resolve) => {
      canvas.toBlob((blob) => resolve(blob && blob.type === 'image/webp' ? blob : null), 'image/webp', quality)
    })
  } finally {
    bitmap.close()
  }
}

/** a.jpg → a.webp；没有扩展名时直接补后缀。 */
export function webpName(name: string): string {
  return name.replace(/\.[^./\\]+$/, '') + '.webp'
}
