import { useState } from 'react'

// Message input with a pending state: disabled + a visible "sending"
// indicator while awaiting the response, re-enabled on success or error. On
// failure the typed text is preserved (not cleared) so the user can retry.
export function ChatComposer({
  sending,
  onSend,
  placeholder = 'Ask a question…',
}: {
  sending: boolean
  onSend: (content: string) => Promise<boolean>
  placeholder?: string
}) {
  const [value, setValue] = useState('')

  const submit = async () => {
    const trimmed = value.trim()
    if (!trimmed || sending) return
    const ok = await onSend(trimmed)
    if (ok) setValue('')
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
      className="mt-2 flex items-end gap-2"
    >
      <textarea
        aria-label="Message"
        value={value}
        disabled={sending}
        aria-busy={sending}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault()
            void submit()
          }
        }}
        placeholder={placeholder}
        rows={2}
        className="h-16 flex-1 resize-none rounded-[var(--radius)] border border-input bg-background px-2 py-1.5 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-60"
      />
      <button
        type="submit"
        disabled={sending || !value.trim()}
        aria-busy={sending}
        className="h-9 shrink-0 rounded-[var(--radius)] bg-primary px-3 text-sm font-medium text-primary-foreground disabled:cursor-not-allowed disabled:opacity-60"
      >
        {sending ? 'Sending…' : 'Send'}
      </button>
    </form>
  )
}
