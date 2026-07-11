// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { EmptyState } from './EmptyState'

afterEach(cleanup)

describe('EmptyState', () => {
  it('always renders the title', () => {
    render(<EmptyState title="Nothing here yet" />)

    expect(screen.getByText('Nothing here yet')).toBeInTheDocument()
  })

  it('renders the hint when provided and omits it when not provided', () => {
    const { rerender } = render(<EmptyState title="Nothing here yet" hint="Try adding a note." />)

    expect(screen.getByText('Try adding a note.')).toBeInTheDocument()

    rerender(<EmptyState title="Nothing here yet" />)

    expect(screen.queryByText('Try adding a note.')).not.toBeInTheDocument()
  })

  it('renders the action when provided and omits it when not provided', () => {
    const { rerender } = render(
      <EmptyState title="Nothing here yet" action={<button type="button">Create note</button>} />,
    )

    expect(screen.getByRole('button', { name: 'Create note' })).toBeInTheDocument()

    rerender(<EmptyState title="Nothing here yet" />)

    expect(screen.queryByRole('button', { name: 'Create note' })).not.toBeInTheDocument()
  })
})
