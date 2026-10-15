// The theme mode chosen by the owner. "system" sets no data-theme attribute:
// color-scheme then follows the OS on its own, see style.css. The inline
// script in index.html applies the saved mode before the first paint.

export type ThemeMode = 'system' | 'light' | 'dark'

export const THEME_KEY = 'metagit.theme'

export function storedThemeMode(): ThemeMode {
  try {
    const value = window.localStorage.getItem(THEME_KEY)
    return value === 'light' || value === 'dark' ? value : 'system'
  } catch {
    return 'system'
  }
}

export function setThemeMode(mode: ThemeMode): void {
  try {
    if (mode === 'system') window.localStorage.removeItem(THEME_KEY)
    else window.localStorage.setItem(THEME_KEY, mode)
  } catch {
    // Storage is not available: the mode lasts until the page is reloaded.
  }
  const root = document.documentElement
  if (mode === 'system') delete root.dataset.theme
  else root.dataset.theme = mode
}
