import { useEffect, useState } from 'react'

export function useMediaQuery(query: string): boolean {
  const get = () => (typeof window === 'undefined' ? false : window.matchMedia(query).matches)
  const [matches, setMatches] = useState<boolean>(get)

  useEffect(() => {
    const mql = window.matchMedia(query)
    const onChange = () => setMatches(mql.matches)
    onChange()
    mql.addEventListener('change', onChange)
    return () => mql.removeEventListener('change', onChange)
  }, [query])

  return matches
}

/** 小于 md(768px) 即视为手机 */
export function useIsMobile(): boolean {
  return useMediaQuery('(max-width: 767px)')
}
