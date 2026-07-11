import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { EmbeddingsView } from './EmbeddingsView'
import type { EmbeddingStatus } from '../api/types'

function makeClient(status: EmbeddingStatus) {
  return {
    getEmbeddingStatus: vi.fn().mockResolvedValue(status),
    reembedAll: vi.fn().mockResolvedValue({ status: 'queued', enqueued: 0 }),
  }
}

describe('EmbeddingsView', () => {
  it('renders enabled status with model, dim, and progress', async () => {
    const client = makeClient({ enabled: true, model: 'nomic-embed-text', dim: 768, done: 3, total: 10 })
    render(<EmbeddingsView client={client} />)

    expect(await screen.findByText('Enabled')).toBeInTheDocument()
    expect(screen.getByText('nomic-embed-text')).toBeInTheDocument()
    expect(screen.getByText('768')).toBeInTheDocument()
    expect(screen.getByText('3 / 10 notes embedded')).toBeInTheDocument()
  })

  it('renders disabled status and disables the re-embed button', async () => {
    const client = makeClient({ enabled: false, model: '', dim: 0, done: 0, total: 0 })
    render(<EmbeddingsView client={client} />)

    expect(await screen.findByText('Disabled')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /re-embed all notes/i })).toBeDisabled()
  })

  it('disables the re-embed button while a request is in flight and shows progress text', async () => {
    const client = makeClient({ enabled: true, model: 'nomic-embed-text', dim: 768, done: 0, total: 5 })
    let resolveReembed: (() => void) | undefined
    client.reembedAll.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveReembed = () => resolve({ status: 'queued', enqueued: 5 })
        })
    )
    render(<EmbeddingsView client={client} />)

    const button = await screen.findByRole('button', { name: /re-embed all notes/i })
    expect(button).not.toBeDisabled()

    await userEvent.click(button)

    expect(await screen.findByRole('button', { name: /re-embedding…/i })).toBeDisabled()

    resolveReembed?.()
    await waitFor(() => expect(screen.getByRole('button', { name: /re-embed all notes/i })).not.toBeDisabled())
  })

  it('re-fetches status after a successful re-embed', async () => {
    const client = makeClient({ enabled: true, model: 'nomic-embed-text', dim: 768, done: 0, total: 5 })
    client.getEmbeddingStatus
      .mockResolvedValueOnce({ enabled: true, model: 'nomic-embed-text', dim: 768, done: 0, total: 5 })
      .mockResolvedValueOnce({ enabled: true, model: 'nomic-embed-text', dim: 768, done: 0, total: 5 })

    render(<EmbeddingsView client={client} />)
    await screen.findByText('0 / 5 notes embedded')

    await userEvent.click(screen.getByRole('button', { name: /re-embed all notes/i }))

    await waitFor(() => expect(client.getEmbeddingStatus).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(client.reembedAll).toHaveBeenCalled())
  })

  it('surfaces an error from the status fetch without crashing', async () => {
    const client = {
      getEmbeddingStatus: vi.fn().mockRejectedValue(new Error('network error')),
      reembedAll: vi.fn().mockResolvedValue({ status: 'queued', enqueued: 0 }),
    }
    render(<EmbeddingsView client={client} />)

    expect(await screen.findByText('network error')).toBeInTheDocument()
  })

  it('surfaces an error from the reembed call without crashing', async () => {
    const client = makeClient({ enabled: true, model: 'nomic-embed-text', dim: 768, done: 0, total: 5 })
    client.reembedAll.mockRejectedValue(new Error('reembed failed'))

    render(<EmbeddingsView client={client} />)
    const button = await screen.findByRole('button', { name: /re-embed all notes/i })

    await userEvent.click(button)

    expect(await screen.findByText('reembed failed')).toBeInTheDocument()
    expect(button).not.toBeDisabled()
  })
})
