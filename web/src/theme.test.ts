import { setThemeMode, storedThemeMode, THEME_KEY } from './theme'

afterEach(() => {
  window.localStorage.clear()
  delete document.documentElement.dataset.theme
})

test('a saved dark mode is read back, anything else means system', () => {
  expect(storedThemeMode()).toBe('system')
  window.localStorage.setItem(THEME_KEY, 'dark')
  expect(storedThemeMode()).toBe('dark')
  window.localStorage.setItem(THEME_KEY, 'sepia')
  expect(storedThemeMode()).toBe('system')
})

test('a chosen mode is saved and applied, system removes both', () => {
  setThemeMode('dark')
  expect(window.localStorage.getItem(THEME_KEY)).toBe('dark')
  expect(document.documentElement.dataset.theme).toBe('dark')

  setThemeMode('system')
  expect(window.localStorage.getItem(THEME_KEY)).toBeNull()
  expect(document.documentElement).not.toHaveAttribute('data-theme')
})

test('without storage the mode is system and a choice still applies', () => {
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
    throw new Error('denied')
  })
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
    throw new Error('denied')
  })

  expect(storedThemeMode()).toBe('system')
  setThemeMode('light')
  expect(document.documentElement.dataset.theme).toBe('light')
})
