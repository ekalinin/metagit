import { useSyncExternalStore } from 'react'
import { en, type Messages } from './locales/en'
import { ru } from './locales/ru'

// The interface language chosen by the owner. "system" stores nothing and
// follows the browser languages. The inline script in index.html sets
// <html lang> by the same rule before the first paint.

export type Locale = 'ru' | 'en'
export type LocaleMode = 'system' | Locale

export const LOCALE_KEY = 'metagit.locale'

const LOCALE_EVENT = 'metagit:locale'

const catalogs: Record<Locale, Messages> = { en, ru }

// The mode chosen while storage is not available: it lasts until the page is
// reloaded.
let unsavedMode: LocaleMode | null = null

export function storedLocaleMode(): LocaleMode {
  try {
    const value = window.localStorage.getItem(LOCALE_KEY)
    return value === 'ru' || value === 'en' ? value : 'system'
  } catch {
    return unsavedMode ?? 'system'
  }
}

export function setLocaleMode(mode: LocaleMode): void {
  try {
    if (mode === 'system') window.localStorage.removeItem(LOCALE_KEY)
    else window.localStorage.setItem(LOCALE_KEY, mode)
  } catch {
    unsavedMode = mode
  }
  window.dispatchEvent(new Event(LOCALE_EVENT))
}

// resolveLocale returns the language for a mode: a chosen language wins,
// otherwise the first supported browser language, otherwise English.
export function resolveLocale(mode: LocaleMode, languages: readonly string[]): Locale {
  if (mode !== 'system') return mode
  for (const tag of languages) {
    const primary = tag.split('-')[0].toLowerCase()
    if (primary === 'ru' || primary === 'en') return primary
  }
  return 'en'
}

function browserLanguages(): readonly string[] {
  const { languages, language } = window.navigator
  if (languages && languages.length > 0) return languages
  return language ? [language] : []
}

export function currentLocale(): Locale {
  return resolveLocale(storedLocaleMode(), browserLanguages())
}

// messages returns the catalog of the current language for code outside
// components. Call it only while rendering inside App: App subscribes with
// useT(), so a language change re-renders the whole tree and the next call
// reads the new catalog.
export function messages(): Messages {
  return catalogs[currentLocale()]
}

function subscribe(onChange: () => void): () => void {
  window.addEventListener(LOCALE_EVENT, onChange)
  window.addEventListener('languagechange', onChange)
  return () => {
    window.removeEventListener(LOCALE_EVENT, onChange)
    window.removeEventListener('languagechange', onChange)
  }
}

export function useLocale(): Locale {
  return useSyncExternalStore(subscribe, currentLocale)
}

export function useT(): Messages {
  return catalogs[useLocale()]
}
