import type { Lang } from '../api/types'

/** True when the browser exposes speech synthesis. */
export function isSpeechAvailable(): boolean {
  return typeof window !== 'undefined' && 'speechSynthesis' in window && typeof SpeechSynthesisUtterance !== 'undefined'
}

/** Removes light markdown so it is not read aloud: code ticks, emphasis, headings, links, list bullets. */
export function stripMarkdown(md: string): string {
  return md
    .replace(/```[\s\S]*?```/g, (m) => m.replace(/```\w*\n?/g, ''))
    .replace(/`([^`]*)`/g, '$1')
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/(\*\*|__)(.*?)\1/g, '$2')
    .replace(/(^|[^*\w])[*_]([^*_\n]+)[*_](?=[^*\w]|$)/g, '$1$2')
    .replace(/^\s{0,3}#{1,6}\s+/gm, '')
    .replace(/^\s*[-*+]\s+/gm, '')
    .replace(/^\s*\d+\.\s+/gm, '')
    .replace(/~~(.*?)~~/g, '$1')
    .replace(/[ \t]+/g, ' ')
    .replace(/\n{2,}/g, '\n')
    .trim()
}

/** Prefers a Hindi (India) voice for Hinglish, otherwise an Indian-English or default voice. */
function pickVoice(lang: Lang): SpeechSynthesisVoice | null {
  const voices = window.speechSynthesis.getVoices()
  if (!voices.length) return null
  const want = lang === 'hinglish' ? ['hi-IN', 'hi_IN', 'hi'] : ['en-IN', 'en_IN', 'hi-IN']
  for (const tag of want) {
    const v = voices.find((x) => x.lang.toLowerCase().startsWith(tag.toLowerCase()))
    if (v) return v
  }
  return voices.find((v) => v.default) ?? null
}

/** Speaks the text, cancelling anything already playing. Markdown is stripped first. */
export function speak(text: string, lang: Lang): void {
  if (!isSpeechAvailable()) return
  const synth = window.speechSynthesis
  synth.cancel()
  const u = new SpeechSynthesisUtterance(stripMarkdown(text))
  const voice = pickVoice(lang)
  if (voice) {
    u.voice = voice
    u.lang = voice.lang
  } else {
    u.lang = lang === 'hinglish' ? 'hi-IN' : 'en-IN'
  }
  u.rate = 0.95
  synth.speak(u)
}

/** Stops any current speech; safe to call when nothing is playing. */
export function stopSpeaking(): void {
  if (isSpeechAvailable()) window.speechSynthesis.cancel()
}
