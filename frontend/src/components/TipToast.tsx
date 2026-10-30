import { useCallback, useEffect, useRef, useState } from 'react'
import { BodyPortal } from '../lib/portal'

type TipKind = 'ok' | 'err'

export function useTipToast(durationMs = 2000) {
  const [tip, setTip] = useState<{ text: string; kind: TipKind } | null>(null)
  const timer = useRef(0)
  const durationRef = useRef(durationMs)
  durationRef.current = durationMs

  useEffect(() => () => window.clearTimeout(timer.current), [])

  const showTip = useCallback((text: string, kind: TipKind = 'ok') => {
    const msg = text.trim()
    if (!msg) return
    setTip({ text: msg, kind })
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => setTip(null), durationRef.current)
  }, [])

  const tipNode = tip ? (
    <BodyPortal>
      <div className={`tip-toast tip-${tip.kind}`} role="status" aria-live="polite">
        {tip.text}
      </div>
    </BodyPortal>
  ) : null

  return { showTip, tipNode }
}
