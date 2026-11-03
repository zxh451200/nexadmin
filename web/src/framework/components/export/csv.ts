// 导出下载的唯一实现：公式注入转义 + BOM（Excel 靠 BOM 认 UTF-8，否则中文成乱码）。
// 此前 user / website / pay 各有一份同样的代码，新增导出请统一走这里。

function escapeCell(v: string): string {
  if (/^[=+\-@]/.test(v)) v = `'${v}`
  return /[",\r\n]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v
}

/** 触发浏览器下载一个 Blob */
export function saveBlob(filename: string, blob: Blob) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 0)
}

/** 生成带 BOM 的 CSV 并触发浏览器下载（BOM 让 Excel 正确识别 UTF-8） */
export function downloadCsv(filename: string, rows: (string | number | null | undefined)[][]) {
  const text = rows.map((r) => r.map((v) => escapeCell(v === null || v === undefined ? '' : String(v))).join(',')).join('\r\n')
  saveBlob(filename, new Blob(['\uFEFF', text], { type: 'text/csv;charset=utf-8' }))
}

/** 后端 blob 的真实格式：按 MIME 认扩展名，服务端切 xlsx 时前端不用改 */
export function blobExt(blob: Blob): 'csv' | 'xlsx' {
  return blob.type.includes('sheet') ? 'xlsx' : 'csv'
}

/** 本地时间的 yyyyMMdd-HHmmss，用作导出文件名后缀，避免同一秒重复导出互相覆盖 */
export function stamp(d = new Date()): string {
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}-${p(d.getHours())}${p(d.getMinutes())}${p(d.getSeconds())}`
}
