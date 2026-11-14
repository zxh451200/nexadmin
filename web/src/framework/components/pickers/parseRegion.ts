import type { RegionNode } from '@/framework/request/types'

export interface ParsedRegionAddress {
  /** 命中的省市区路径（命中几层是几层，直辖市/港澳天然只到两级） */
  path: RegionNode[]
  /** 除掉区划后剩下的详细地址（可能是空串） */
  rest: string
}

/** 「市辖区」「县」这类占位节点：地址文本里一般不写，匹配时直接跳到它们的下级 */
const PLACEHOLDER = new Set(['市辖区', '县', '省直辖县级行政区划', '自治区直辖县级行政区划'])

/** 常用后缀：全名剥掉后缀得到短别名（广西壮族自治区 ↔ 广西、中牟县 ↔ 中牟） */
const SUFFIXES = ['特别行政区', '维吾尔自治区', '壮族自治区', '回族自治区', '自治区', '省', '市', '盟', '地区', '县', '旗']

function aliasesOf(name: string): string[] {
	const out = [name]
	for (const suf of SUFFIXES) {
		if (name.length > suf.length && name.endsWith(suf)) {
			const alias = name.slice(0, -suf.length)
			// 剥完只剩一个字的别名容易误命中同前缀的其他区划（沙县 → “沙”撞沙河市），不收
			if (alias.length >= 2) out.push(alias)
			break
		}
	}
	return out
}

/** 文本是否以该节点开头（含短别名），命中返回消耗掉的长度 */
function matchNode(text: string, node: RegionNode): number {
  const lower = text.toLowerCase()
  for (const alias of aliasesOf(node.name)) {
    if (lower.startsWith(alias.toLowerCase())) return alias.length
  }
  // 拼音：ü 在数据里写作 v（输入法习惯）。文本以完整拼音开头就直接吃掉；
  // 首词是拼音的前缀（“guangdong shenzhen”没拼完）也按词吃掉，剩下的词留给下一层。
  const p = node.pinyin?.toLowerCase()
  if (p) {
    if (lower.startsWith(p)) return p.length
    const word = lower.split(/\s+/)[0]
    if (word && p.startsWith(word)) return word.length
  }
  return 0
}

/**
 * 从一段完整地址里贪心解析省市区：逐层用名称/别名/拼音前缀匹配，
 * 占位节点（市辖区/县）不吃文本直接下钻，命中几层写几层，剩余文本即详细地址。
 * 区划之间的空白不参与匹配（拼音地址 “guangdong shenzhen” 靠它断词），
 * 详细地址保留原文里的空格。解析对着调用方传入的运行时区划树做（区划快照可能被同步替换，不写死名字）。
 */
export function parseRegionAddress(tree: RegionNode[], text: string, level: 2 | 3 = 3): ParsedRegionAddress {
  const src = text.trim()
  const path: RegionNode[] = []
  let nodes: RegionNode[] | undefined = tree
  let pos = 0
  for (let depth = 0; depth < level && nodes?.length; depth++) {
    while (pos < src.length && /\s/.test(src[pos])) pos++
    const at = src.slice(pos)
    const matched = matchAmong(nodes, at)
    // 没有子节点名匹配，但该层只剩占位节点时跳过去试下级（“北海市海城区…”省略市辖区）
    if (!matched) {
      const skipped = skipPlaceholder(nodes, at)
      if (!skipped) {
        // 本层没命中：退而求其次做「全树唯一命中」补全——用户只写“中牟”不带省市也能定位
        //（cpca 的做法）；命中多个同前缀区划（如“朝阳”）时不猜，剩余文本整体留给详细地址
        const uniq = matchUniqueAtDepth(tree, at, level)
        if (uniq) {
          path.push(...uniq.path)
          pos += uniq.len
        }
        break
      }
      path.push(skipped.placeholder, skipped.node)
      pos += skipped.len
      nodes = skipped.node.children
      continue
    }
    path.push(matched.node)
    pos += matched.len
    nodes = matched.node.children
  }
  return { path, rest: src.slice(pos).trimStart() }
}

/** 全树唯一命中兜底：在第 level 层的全部叶子节点里做名称/别名/拼音前缀匹配，恰好唯一命中才采用 */
function matchUniqueAtDepth(
  tree: RegionNode[],
  text: string,
  level: 2 | 3,
): { path: RegionNode[]; len: number } | null {
  const hits: { path: RegionNode[]; len: number }[] = []
  const walk = (nodes: RegionNode[], path: RegionNode[], depth: number) => {
    for (const n of nodes ?? []) {
      const nextPath = [...path, n]
      if (depth + 1 === level) {
        const len = matchNode(text, n)
        if (len > 0) hits.push({ path: nextPath, len })
      } else {
        walk(n.children ?? [], nextPath, depth + 1)
      }
    }
  }
  walk(tree, [], 0)
  return hits.length === 1 ? hits[0] : null
}

/** 在同层节点里找第一个名称/别名/拼音前缀命中的，返回消耗长度 */
function matchAmong(
  nodes: RegionNode[],
  text: string,
): { node: RegionNode; len: number } | null {
  for (const n of nodes) {
    const len = matchNode(text, n)
    if (len > 0) return { node: n, len }
  }
  return null
}

/** 占位节点（市辖区/县）不吃文本，改试其下级；命中则返回占位节点 + 命中的下级 */
function skipPlaceholder(
  nodes: RegionNode[],
  text: string,
): { placeholder: RegionNode; node: RegionNode; len: number } | null {
  for (const ph of nodes) {
    if (!PLACEHOLDER.has(ph.name)) continue
    for (const k of ph.children ?? []) {
      const len = matchNode(text, k)
      if (len > 0) return { placeholder: ph, node: k, len }
    }
  }
  return null
}
