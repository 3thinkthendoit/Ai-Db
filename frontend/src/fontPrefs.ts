export type FontFamilyId = 'system' | 'yahei' | 'segoe' | 'noto' | 'mono'

export type FontSizeId = 'sm' | 'md' | 'lg' | 'xl'

const FAMILY_KEY = 'aidb.fontFamily'
const SIZE_KEY = 'aidb.fontSize'

const FAMILY_STACKS: Record<FontFamilyId, string> = {
  system:
    '-apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif',
  yahei: '"Microsoft YaHei", "PingFang SC", "Noto Sans SC", sans-serif',
  segoe: '"Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif',
  noto: '"Noto Sans SC", "Source Han Sans SC", "Microsoft YaHei", sans-serif',
  mono: '"Cascadia Code", "JetBrains Mono", Consolas, "Microsoft YaHei", monospace',
}

const SIZE_PX: Record<FontSizeId, string> = {
  sm: '12px',
  md: '13px',
  lg: '14px',
  xl: '16px',
}

export const FONT_FAMILY_IDS: FontFamilyId[] = ['system', 'yahei', 'segoe', 'noto', 'mono']
export const FONT_SIZE_IDS: FontSizeId[] = ['sm', 'md', 'lg', 'xl']

function isFamily(v: string | null): v is FontFamilyId {
  return v === 'system' || v === 'yahei' || v === 'segoe' || v === 'noto' || v === 'mono'
}

function isSize(v: string | null): v is FontSizeId {
  return v === 'sm' || v === 'md' || v === 'lg' || v === 'xl'
}

function apply(family: FontFamilyId, size: FontSizeId) {
  const root = document.documentElement
  root.style.setProperty('--font-sans', FAMILY_STACKS[family])
  root.style.setProperty('--font-size', SIZE_PX[size])
}

export function initFontPrefs(): { family: FontFamilyId; size: FontSizeId } {
  let family: FontFamilyId = 'system'
  let size: FontSizeId = 'md'
  try {
    const f = localStorage.getItem(FAMILY_KEY)
    const s = localStorage.getItem(SIZE_KEY)
    if (isFamily(f)) family = f
    if (isSize(s)) size = s
  } catch { /* ignore */ }
  apply(family, size)
  return { family, size }
}

export function getFontFamily(): FontFamilyId {
  try {
    const f = localStorage.getItem(FAMILY_KEY)
    if (isFamily(f)) return f
  } catch { /* ignore */ }
  return 'system'
}

export function getFontSize(): FontSizeId {
  try {
    const s = localStorage.getItem(SIZE_KEY)
    if (isSize(s)) return s
  } catch { /* ignore */ }
  return 'md'
}

export function setFontFamily(v: FontFamilyId) {
  if (!isFamily(v)) return
  try { localStorage.setItem(FAMILY_KEY, v) } catch { /* ignore */ }
  apply(v, getFontSize())
}

export function setFontSize(v: FontSizeId) {
  if (!isSize(v)) return
  try { localStorage.setItem(SIZE_KEY, v) } catch { /* ignore */ }
  apply(getFontFamily(), v)
}
