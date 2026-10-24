/**
 * mock 专用的最小 .xlsx 读取（浏览器端）：解 zip（中央目录 + DecompressionStream）、取第一个工作表，
 * 支持 sharedStrings / inlineStr / 数字 / 布尔。只为让 mock 的导入与真实后端行为一致，不用于生产路径。
 * 带上限防炸弹：条目数、解压后体积；DOMParser 不会加载外部实体。
 */

const MAX_ENTRIES = 200
const MAX_INFLATED = 16 * 1024 * 1024

interface ZipEntry {
  method: number
  compSize: number
  offset: number
}

function readEntries(buf: DataView): Map<string, ZipEntry> {
  let eocd = -1
  for (let i = buf.byteLength - 22; i >= Math.max(0, buf.byteLength - 22 - 65535); i--) {
    if (buf.getUint32(i, true) === 0x06054b50) {
      eocd = i
      break
    }
  }
  if (eocd < 0) throw new Error('not a zip')
  const count = buf.getUint16(eocd + 10, true)
  if (count > MAX_ENTRIES) throw new Error('too many files')
  let p = buf.getUint32(eocd + 16, true)
  const dec = new TextDecoder()
  const out = new Map<string, ZipEntry>()
  for (let i = 0; i < count; i++) {
    if (p + 46 > buf.byteLength || buf.getUint32(p, true) !== 0x02014b50) throw new Error('bad central directory')
    const nameLen = buf.getUint16(p + 28, true)
    const extraLen = buf.getUint16(p + 30, true)
    const commentLen = buf.getUint16(p + 32, true)
    const name = dec.decode(new Uint8Array(buf.buffer, buf.byteOffset + p + 46, nameLen))
    out.set(name.toLowerCase(), {
      method: buf.getUint16(p + 10, true),
      compSize: buf.getUint32(p + 20, true),
      offset: buf.getUint32(p + 42, true),
    })
    p += 46 + nameLen + extraLen + commentLen
  }
  return out
}

async function inflate(buf: DataView, e: ZipEntry): Promise<string> {
  const nameLen = buf.getUint16(e.offset + 26, true)
  const extraLen = buf.getUint16(e.offset + 28, true)
  const start = buf.byteOffset + e.offset + 30 + nameLen + extraLen
  const raw = new Uint8Array(buf.buffer, start, e.compSize)
  let bytes: Uint8Array
  if (e.method === 0) {
    bytes = raw
  } else if (e.method === 8) {
    const stream = new Blob([raw.slice()]).stream().pipeThrough(new DecompressionStream('deflate-raw' as CompressionFormat))
    const reader = stream.getReader()
    const chunks: Uint8Array[] = []
    let total = 0
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      total += value.length
      if (total > MAX_INFLATED) {
        await reader.cancel()
        throw new Error('too large')
      }
      chunks.push(value)
    }
    bytes = new Uint8Array(total)
    let o = 0
    for (const c of chunks) {
      bytes.set(c, o)
      o += c.length
    }
  } else {
    throw new Error('unsupported compression')
  }
  return new TextDecoder().decode(bytes)
}

function parseXml(text: string): Document {
  if (/<!DOCTYPE/i.test(text)) throw new Error('DTD not allowed')
  const doc = new DOMParser().parseFromString(text, 'application/xml')
  if (doc.getElementsByTagName('parsererror').length) throw new Error('bad xml')
  return doc
}

function textOf(el: Element): string {
  return Array.from(el.getElementsByTagName('t'))
    .filter((t) => !t.closest('rPh'))
    .map((t) => t.textContent ?? '')
    .join('')
}

function colIndex(ref: string): number {
  let n = 0
  for (const ch of ref.replace(/[0-9]/g, '')) n = n * 26 + ch.charCodeAt(0) - 64
  return n - 1
}

/** 读取第一个工作表为字符串矩阵（下标 i 对应第 i+1 行，空行为 []） */
export async function readXlsxRows(file: File): Promise<string[][]> {
  const buf = new DataView(await file.arrayBuffer())
  const entries = readEntries(buf)
  const read = async (name: string) => {
    const e = entries.get(name)
    return e ? inflate(buf, e) : null
  }
  const sheetXml = await read('xl/worksheets/sheet1.xml')
  if (!sheetXml) throw new Error('no sheet')
  const sstXml = await read('xl/sharedstrings.xml')
  const sst = sstXml ? Array.from(parseXml(sstXml).getElementsByTagName('si')).map(textOf) : []
  const rows: string[][] = []
  for (const row of Array.from(parseXml(sheetXml).getElementsByTagName('row'))) {
    const r = Number(row.getAttribute('r')) || rows.length + 1
    if (r > 20000) throw new Error('too many rows')
    const cells: string[] = []
    for (const c of Array.from(row.getElementsByTagName('c'))) {
      const col = colIndex(c.getAttribute('r') ?? '') >= 0 ? colIndex(c.getAttribute('r') ?? '') : cells.length
      if (col > 255) throw new Error('too many columns')
      const t = c.getAttribute('t')
      const v = c.getElementsByTagName('v')[0]?.textContent ?? ''
      let val = v
      if (t === 's') val = sst[Number(v)] ?? ''
      else if (t === 'inlineStr') val = textOf(c)
      else if (t === 'b') val = v === '1' ? 'TRUE' : 'FALSE'
      while (cells.length <= col) cells.push('')
      cells[col] = val
    }
    while (rows.length < r - 1) rows.push([])
    rows[r - 1] = cells
  }
  return rows
}
