import { type ReactNode } from 'react'
import { createPortal } from 'react-dom'

/** Render overlays on document.body so panel max-width / overflow cannot clip them. */
export function BodyPortal({ children }: { children: ReactNode }) {
  if (typeof document === 'undefined') return null
  return createPortal(children, document.body)
}
