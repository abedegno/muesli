// Minimal, safe-by-construction Markdown: we build React elements (never
// dangerouslySetInnerHTML), supporting #/##/### headings, "- " bullets, and
// paragraphs. Sufficient for v1 summaries + transcript text.
export function Markdown({
  source,
  renderText,
}: {
  source: string
  // Optional per-line text renderer, applied to heading/bullet/paragraph text
  // instead of using the raw string directly. Lets callers (e.g. ChatThread)
  // turn inline `[n]` citation markers into clickable chips while the rest of
  // the line still renders as plain text. Defaults to identity (raw string).
  renderText?: (text: string) => React.ReactNode
}) {
  const render = renderText ?? ((text: string): React.ReactNode => text)
  const lines = source.split('\n')
  const blocks: React.ReactNode[] = []
  let bullets: React.ReactNode[] = []

  const flushBullets = () => {
    if (bullets.length) {
      blocks.push(
        <ul key={`ul-${blocks.length}`}>
          {bullets.map((b, i) => (
            <li key={i}>{b}</li>
          ))}
        </ul>,
      )
      bullets = []
    }
  }

  for (const line of lines) {
    if (line.startsWith('### ')) {
      flushBullets()
      blocks.push(<h3 key={blocks.length}>{render(line.slice(4))}</h3>)
    } else if (line.startsWith('## ')) {
      flushBullets()
      blocks.push(<h2 key={blocks.length}>{render(line.slice(3))}</h2>)
    } else if (line.startsWith('# ')) {
      flushBullets()
      blocks.push(<h1 key={blocks.length}>{render(line.slice(2))}</h1>)
    } else if (line.startsWith('- ')) {
      bullets.push(render(line.slice(2)))
    } else if (line.trim() === '') {
      flushBullets()
    } else {
      flushBullets()
      blocks.push(<p key={blocks.length}>{render(line)}</p>)
    }
  }
  flushBullets()
  return <div className="markdown">{blocks}</div>
}
