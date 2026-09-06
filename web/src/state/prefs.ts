import type { Lang } from '../api/types'
import { readRaw, writeRaw } from './storage'

const LANG_KEY = 'tutor.lang_pref'

/** Returns the locally mirrored language preference; Hinglish by default. */
export function loadLang(): Lang {
  return readRaw(LANG_KEY) === 'en' ? 'en' : 'hinglish'
}

/** Mirrors the language preference locally so it applies before /v1/me answers. */
export function saveLang(lang: Lang): void {
  writeRaw(LANG_KEY, lang)
}
