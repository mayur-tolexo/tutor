import { useEffect, useState } from 'react'

/**
 * Pixels between the bottom of the layout viewport and the top of the soft keyboard.
 * Zero when the keyboard is closed or when the browser already resizes the layout viewport.
 */
export function useKeyboardOffset(): number {
  const [offset, setOffset] = useState(0)
  useEffect(() => {
    const vv = window.visualViewport
    if (!vv) return
    const update = () => {
      const o = window.innerHeight - (vv.height + vv.offsetTop)
      setOffset(Math.max(0, Math.round(o)))
    }
    update()
    vv.addEventListener('resize', update)
    vv.addEventListener('scroll', update)
    window.addEventListener('resize', update)
    return () => {
      vv.removeEventListener('resize', update)
      vv.removeEventListener('scroll', update)
      window.removeEventListener('resize', update)
    }
  }, [])
  return offset
}
