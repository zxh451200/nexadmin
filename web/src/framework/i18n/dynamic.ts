import type { i18n, ResourceLanguage } from 'i18next'

const unsafe = new Set(['__proto__', 'prototype', 'constructor'])
type Dict = Record<string, unknown>

/** Replaceable overlays keep static catalogues detached from the mutable store. */
export function createResourceOverlay(instance: i18n) {
  const originals = new Map<string, { lang: string; ns: string; value?: ResourceLanguage }>()
  const copy = <T,>(value: T): T => JSON.parse(JSON.stringify(value)) as T
  function restore(lang: string, ns: string, value?: ResourceLanguage) {
    instance.removeResourceBundle(lang, ns)
    if (value) instance.addResourceBundle(lang, ns, copy(value), true, true)
  }
  return {
    replace(lang: string, ns: string, entries: Record<string, string>) {
      if (unsafe.has(ns)) return
      const key = JSON.stringify([lang, ns])
      if (!originals.has(key)) {
        const value = instance.getResourceBundle(lang, ns) as ResourceLanguage | undefined
        originals.set(key, { lang, ns, value: value ? copy(value) : undefined })
      }
      const tree: Dict = Object.create(null) as Dict
      for (const [name, value] of Object.entries(entries)) {
        const parts = name.split('.')
        if (typeof value !== 'string' || parts.some((p) => !p || unsafe.has(p))) continue
        let node = tree
        for (const part of parts.slice(0, -1)) {
          if (!node[part] || typeof node[part] !== 'object') node[part] = Object.create(null) as Dict
          node = node[part] as Dict
        }
        node[parts[parts.length - 1]] = value
      }
      restore(lang, ns, originals.get(key)!.value)
      instance.addResourceBundle(lang, ns, tree, true, true)
    },
    clear() {
      for (const { lang, ns, value } of originals.values()) restore(lang, ns, value)
      originals.clear()
    },
  }
}

/** Optional providers are discovered without depending on a particular plugin. */
export function startResourceLoaders(instance: i18n) {
  const loaders = import.meta.glob('/src/plugins/**/*.i18n-loader.ts', { eager: true }) as Record<
    string, { start: (instance: i18n) => (() => void) }
  >
  const stops = Object.values(loaders).map((loader) => loader.start(instance))
  if (import.meta.hot) import.meta.hot.dispose(() => stops.forEach((stop) => stop()))
}
