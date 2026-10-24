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
export function downloadCsv(filename: string, rows: string[][]) {
  const text = rows.map((r) => r.map(escapeCell).join(',')).join('\r\n')
  saveBlob(filename, new Blob(['\uFEFF', text], { type: 'text/csv;charset=utf-8' }))
}
