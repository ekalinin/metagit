import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { LOCALE_KEY } from './locale'

beforeEach(() => {
  // Tests run in a Russian browser; jsdom reports en-US by default.
  vi.spyOn(window.navigator, 'languages', 'get').mockReturnValue(['ru-RU'])
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  window.history.replaceState(null, '', '/')
  window.localStorage.removeItem(LOCALE_KEY)
  document.documentElement.removeAttribute('lang')
})
