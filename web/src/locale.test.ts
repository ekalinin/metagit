import { currentLocale, LOCALE_KEY, resolveLocale, setLocaleMode, storedLocaleMode } from './locale'

afterEach(() => {
  window.localStorage.clear()
})

test('the first supported browser language wins, otherwise English', () => {
  expect(resolveLocale('system', ['ru-RU'])).toBe('ru')
  expect(resolveLocale('system', ['de'])).toBe('en')
  expect(resolveLocale('system', ['de-DE', 'ru'])).toBe('ru')
  expect(resolveLocale('system', ['en-GB', 'ru'])).toBe('en')
  expect(resolveLocale('system', [])).toBe('en')
})

test('a chosen language wins over the browser', () => {
  expect(resolveLocale('en', ['ru-RU'])).toBe('en')
  expect(resolveLocale('ru', ['en-US'])).toBe('ru')
})

test('an empty language list falls back to navigator.language', () => {
  vi.spyOn(window.navigator, 'languages', 'get').mockReturnValue([])
  vi.spyOn(window.navigator, 'language', 'get').mockReturnValue('ru-RU')

  expect(currentLocale()).toBe('ru')
})

test('a saved language is read back, anything else means system', () => {
  expect(storedLocaleMode()).toBe('system')
  window.localStorage.setItem(LOCALE_KEY, 'en')
  expect(storedLocaleMode()).toBe('en')
  expect(currentLocale()).toBe('en')
  window.localStorage.setItem(LOCALE_KEY, 'de')
  expect(storedLocaleMode()).toBe('system')
})

test('a chosen mode is saved, system removes it', () => {
  setLocaleMode('en')
  expect(window.localStorage.getItem(LOCALE_KEY)).toBe('en')

  setLocaleMode('system')
  expect(window.localStorage.getItem(LOCALE_KEY)).toBeNull()
})

test('without storage the mode is system and a choice still applies', () => {
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
    throw new Error('denied')
  })
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
    throw new Error('denied')
  })
  vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => {
    throw new Error('denied')
  })

  expect(storedLocaleMode()).toBe('system')
  setLocaleMode('en')
  expect(storedLocaleMode()).toBe('en')
  setLocaleMode('system')
  expect(storedLocaleMode()).toBe('system')
})
