import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'

export type ThemeMode = 'light' | 'dark' | 'system'
export type ThemePreset = 'default' | 'tangerine' | 'soft-pop'
type Resolved = 'light' | 'dark'

interface ThemeContextValue {
  mode: ThemeMode
  setMode: (mode: ThemeMode) => void
  resolved: Resolved
  preset: ThemePreset
  setPreset: (preset: ThemePreset) => void
}

const STORAGE_KEY = 'nex-theme'
const PRESET_KEY = 'nex-theme-preset'

/** 主题预设色板：tokens.css 里 [data-preset='…'] 块的取值；default 不写块 = 内置靛蓝/neutral */
const PRESETS: ThemePreset[] = ['default', 'tangerine', 'soft-pop']

const ThemeContext = createContext<ThemeContextValue | null>(null)

function readMode(): ThemeMode {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    if (v === 'light' || v === 'dark' || v === 'system') return v
  } catch {
    /* ignore */
  }
  return 'system'
}

function readPreset(): ThemePreset {
  try {
    const v = localStorage.getItem(PRESET_KEY) as ThemePreset | null
    if (v && PRESETS.includes(v)) return v
  } catch {
    /* ignore */
  }
  return 'default'
}

function systemResolved(): Resolved {
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

function applyTheme(resolved: Resolved) {
  const root = document.documentElement
  root.classList.toggle('dark', resolved === 'dark')
  root.style.colorScheme = resolved
  let meta = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]')
  if (!meta) {
    meta = document.createElement('meta')
    meta.name = 'theme-color'
    document.head.appendChild(meta)
  }
  const bg = getComputedStyle(root).getPropertyValue('--background').trim()
  meta.content = bg ? `hsl(${bg})` : resolved === 'dark' ? 'black' : 'white'
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>(readMode)
  const [preset, setPresetState] = useState<ThemePreset>(readPreset)
  const [system, setSystem] = useState<Resolved>(systemResolved)

  const resolved: Resolved = mode === 'system' ? system : mode

  useEffect(() => {
    const mql = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = () => setSystem(mql.matches ? 'dark' : 'light')
    mql.addEventListener('change', onChange)
    return () => mql.removeEventListener('change', onChange)
  }, [])

  useEffect(() => {
    applyTheme(resolved)
  }, [resolved])

  // 预设写进 <html data-preset>，tokens.css 的属性选择器接管换肤；default 移除属性回到内置色板
  useEffect(() => {
    const root = document.documentElement
    if (preset === 'default') root.removeAttribute('data-preset')
    else root.setAttribute('data-preset', preset)
  }, [preset])

  const setMode = useCallback((next: ThemeMode) => {
    setModeState(next)
    try {
      localStorage.setItem(STORAGE_KEY, next)
    } catch {
      /* ignore */
    }
  }, [])

  const setPreset = useCallback((next: ThemePreset) => {
    setPresetState(next)
    try {
      localStorage.setItem(PRESET_KEY, next)
    } catch {
      /* ignore */
    }
  }, [])

  const value = useMemo(() => ({ mode, setMode, resolved, preset, setPreset }), [mode, setMode, resolved, preset, setPreset])

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext)
  if (!ctx) throw new Error('useTheme must be used within ThemeProvider')
  return ctx
}
