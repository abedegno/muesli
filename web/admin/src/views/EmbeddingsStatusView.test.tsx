import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { EmbeddingsStatusView } from './EmbeddingsStatusView'
import type { EmbeddingsStatus } from '../api/types'

describe('EmbeddingsStatusView', () => {
  it('renders enabled embeddings status', async () => {
    const mockStatus: EmbeddingsStatus = {
      enabled: true,
      model: 'nomic-embed-text',
      dim: 768,
      minScore: 0.6,
      docPrefix: 'search_document: ',
      queryPrefix: 'search_query: ',
    }
    const client = { getEmbeddingsStatus: vi.fn().mockResolvedValue(mockStatus) }

    render(<EmbeddingsStatusView client={client} />)

    expect(await screen.findByText('Enabled')).toBeInTheDocument()
    expect(screen.getByText('nomic-embed-text')).toBeInTheDocument()
    expect(screen.getByText('768')).toBeInTheDocument()
    expect(screen.getByText('0.6')).toBeInTheDocument()
  })

  it('renders disabled embeddings status with configured model', async () => {
    const mockStatus: EmbeddingsStatus = {
      enabled: false,
      model: 'some-model',
      dim: 1536,
      minScore: 0.7,
      docPrefix: '',
      queryPrefix: '',
    }
    const client = { getEmbeddingsStatus: vi.fn().mockResolvedValue(mockStatus) }

    render(<EmbeddingsStatusView client={client} />)

    expect(await screen.findByText('Disabled')).toBeInTheDocument()
    expect(screen.getByText('some-model')).toBeInTheDocument()
    expect(screen.getByText('1536')).toBeInTheDocument()
  })

  it('renders error when fetch fails', async () => {
    const client = {
      getEmbeddingsStatus: vi.fn().mockRejectedValue(new Error('network error')),
    }

    render(<EmbeddingsStatusView client={client} />)

    expect(await screen.findByText('network error')).toBeInTheDocument()
  })
})
