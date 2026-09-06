import { BAR_KEYS } from '../editor/keyboardBar'

interface Props {
  onKey: (key: string) => void
}

/** Row of code symbols. Pointer-down is cancelled so the editor keeps focus and the soft keyboard stays up. */
export function KeyboardBar({ onKey }: Props) {
  return (
    <div className="kbar" role="toolbar" aria-label="Code symbols">
      {BAR_KEYS.map((k) => (
        <button
          key={k}
          type="button"
          className={`kkey ${k === 'Tab' ? 'kkey-wide' : ''}`}
          tabIndex={-1}
          onPointerDown={(e) => e.preventDefault()}
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => onKey(k)}
          aria-label={k === 'Tab' ? 'Indent' : k === '←' ? 'Cursor left' : k === '→' ? 'Cursor right' : k}
        >
          {k}
        </button>
      ))}
    </div>
  )
}
