export const AI_UNAVAILABLE_MESSAGE =
  'AI features need an agent plugin configured. Install Ollama, or ask your administrator to configure one.'
export const OLLAMA_DOWNLOAD_URL = 'https://ollama.com/download'

export function AgentUnavailableNotice({ compact = false }: { compact?: boolean }) {
  return (
    <div
      role="status"
      className={compact
        ? 'rounded-[var(--radius)] border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm'
        : 'rounded-[var(--radius)] border border-amber-500/30 bg-amber-500/10 p-4 text-sm'}
    >
      <span>{AI_UNAVAILABLE_MESSAGE}</span>{' '}
      <a className="underline underline-offset-2" href={OLLAMA_DOWNLOAD_URL} target="_blank" rel="noreferrer">
        Download Ollama
      </a>
    </div>
  )
}
