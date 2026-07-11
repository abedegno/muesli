import { render, screen, fireEvent } from '@testing-library/react'
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { ConfigView } from './ConfigView'
import type { ConfigEntry } from '../api/types'

const entries: ConfigEntry[] = [
  { name: 'Server address', envVar: 'MUESLI_ADDR', value: ':8080', source: 'default' },
  { name: 'Master key', envVar: 'MUESLI_MASTER_KEY', value: '(set)', source: 'env' },
]

function makeClient(rows: ConfigEntry[] = entries) {
  return { getAdminConfig: vi.fn().mockResolvedValue(rows) }
}

// A realistic-looking raw secret. It must never appear anywhere in the
// rendered DOM — the server contract (ADM06) guarantees secret-shaped
// entries always arrive already collapsed to "(set)"/"(unset)", never the
// raw value, so this string is deliberately absent from every mocked
// response below. Scanning the full rendered tree (not just individual
// screen.getByText queries) guards against a future regression that, say,
// concatenates or dumps entry data somewhere other than the obvious cell.
const rawSecretThatMustNeverAppear = 'fake-super-secret-value-xyz123'

describe('ConfigView', () => {
  it('renders rows with name, value, and source', async () => {
    const client = makeClient()
    render(<ConfigView client={client} />)

    expect(await screen.findByText('Server address')).toBeInTheDocument()
    expect(screen.getByText('MUESLI_ADDR')).toBeInTheDocument()
    expect(screen.getByText(':8080')).toBeInTheDocument()
    expect(screen.getByText('default')).toBeInTheDocument()
    expect(screen.getByText('Master key')).toBeInTheDocument()
    expect(screen.getByText('env')).toBeInTheDocument()
    expect(client.getAdminConfig).toHaveBeenCalled()
  })

  it('shows the redacted placeholder for a secret entry, never the raw value', async () => {
    const client = makeClient()
    const { container } = render(<ConfigView client={client} />)

    await screen.findByText('MUESLI_MASTER_KEY')
    expect(screen.getByText('(set)')).toBeInTheDocument()
    // Scan the entire rendered output (not just a targeted query) so a
    // regression that renders the raw value anywhere in the row/table is
    // still caught, even outside the obvious value cell.
    expect(container.textContent).not.toContain(rawSecretThatMustNeverAppear)
  })

  it('renders a Copy button per row', async () => {
    const client = makeClient()
    render(<ConfigView client={client} />)
    await screen.findByText('MUESLI_ADDR')

    const copyButtons = screen.getAllByRole('button', { name: 'Copy' })
    expect(copyButtons).toHaveLength(entries.length)
  })

  it('shows an error banner when the fetch fails', async () => {
    const client = { getAdminConfig: vi.fn().mockRejectedValue(new Error('network error')) }
    render(<ConfigView client={client} />)
    expect(await screen.findByText('network error')).toBeInTheDocument()
  })

  describe('Copy button', () => {
    // NOTE: this uses fireEvent (not @testing-library/user-event) — user-event's
    // setup() unconditionally installs its own navigator.clipboard stub, which
    // would shadow the mock below and defeat the assertion.
    const writeText = vi.fn().mockResolvedValue(undefined)

    beforeEach(() => {
      writeText.mockClear()
      Object.defineProperty(navigator, 'clipboard', {
        value: { writeText },
        configurable: true,
      })
    })

    afterEach(() => {
      // @ts-expect-error -- test cleanup of a test-only stub
      delete navigator.clipboard
    })

    it("copies the row's value to the clipboard", async () => {
      const client = makeClient()
      render(<ConfigView client={client} />)
      await screen.findByText('MUESLI_ADDR')

      const rows = screen.getAllByRole('row')
      // rows[0] is the header row; rows[1] is MUESLI_ADDR.
      const addrRow = rows[1]
      const copyButton = addrRow.querySelector('button')
      expect(copyButton).not.toBeNull()

      fireEvent.click(copyButton as HTMLButtonElement)

      expect(writeText).toHaveBeenCalledWith(':8080')
      await screen.findByText('Copied')
    })

    it('copies the redacted secret placeholder, never a real secret', async () => {
      const client = makeClient()
      render(<ConfigView client={client} />)
      await screen.findByText('MUESLI_MASTER_KEY')

      const rows = screen.getAllByRole('row')
      const keyRow = rows[2]
      const copyButton = keyRow.querySelector('button')
      expect(copyButton).not.toBeNull()

      fireEvent.click(copyButton as HTMLButtonElement)

      expect(writeText).toHaveBeenCalledWith('(set)')
      await screen.findByText('Copied')
    })
  })

  it('does not throw when navigator.clipboard is undefined (jsdom default)', async () => {
    const client = makeClient()
    render(<ConfigView client={client} />)
    await screen.findByText('MUESLI_ADDR')

    const rows = screen.getAllByRole('row')
    const copyButton = rows[1].querySelector('button')
    expect(() => fireEvent.click(copyButton as HTMLButtonElement)).not.toThrow()
  })
})
