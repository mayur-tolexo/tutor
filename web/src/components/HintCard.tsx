import { useMemo } from 'react'
import type { Hint, Lang } from '../api/types'
import { renderInlineMarkdown } from './markdown'

interface Props {
  hint: Hint
  lang: Lang
  onToggleLang: () => void
}

/** One hint with its level badge and the Hinglish/English toggle. */
export function HintCard({ hint, lang, onToggleLang }: Props) {
  const html = useMemo(() => renderInlineMarkdown(hint.text), [hint.text])
  return (
    <div className="hint">
      <div className="hint-head">
        <span className={`badge badge-l${hint.level}`}>Hint {hint.level}/3</span>
        {hint.line !== undefined && <span className="hint-line">line {hint.line}</span>}
        {hint.source === 'degraded' && <span className="hint-src">basic hint</span>}
        <span className="spacer" />
        <button type="button" className="btn btn-ghost btn-xs" onClick={onToggleLang}>
          Show in {lang === 'hinglish' ? 'English' : 'Hinglish'}
        </button>
      </div>
      <p className="hint-text md" dangerouslySetInnerHTML={{ __html: html }} />
    </div>
  )
}
