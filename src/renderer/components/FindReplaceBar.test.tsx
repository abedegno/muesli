// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { FindReplaceBar, type FindReplaceBarProps } from './FindReplaceBar'

afterEach(cleanup)

function renderFindReplaceBar(overrides: Partial<FindReplaceBarProps> = {}) {
  const props: FindReplaceBarProps = {
    mode: 'find',
    query: '',
    replacement: '',
    matchCount: 0,
    currentMatch: 0,
    onQueryChange: vi.fn(),
    onReplacementChange: vi.fn(),
    onNext: vi.fn(),
    onPrev: vi.fn(),
    onReplaceOne: vi.fn(),
    onReplaceAll: vi.fn(),
    onClose: vi.fn(),
    ...overrides,
  }

  return render(<FindReplaceBar {...props} />)
}

describe('FindReplaceBar', () => {
  it('renders find mode without replace controls and closes cleanly', async () => {
    const onClose = vi.fn()

    renderFindReplaceBar({ onClose })

    expect(screen.getByRole('textbox', { name: 'Find' })).toBeInTheDocument()
    expect(screen.queryByRole('textbox', { name: 'Replace' })).not.toBeInTheDocument()
    expect(screen.getByText('0 of 0')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Previous match' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Next match' })).toBeDisabled()
    expect(screen.queryByRole('button', { name: 'Replace' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Replace All' })).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Close find bar' }))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('renders replace mode with the replace row and a 1-indexed match label', () => {
    renderFindReplaceBar({
      mode: 'replace',
      query: 'needle',
      replacement: 'thread',
      matchCount: 3,
      currentMatch: 1,
    })

    expect(screen.getByRole('textbox', { name: 'Find' })).toHaveValue('needle')
    expect(screen.getByRole('textbox', { name: 'Replace' })).toHaveValue('thread')
    expect(screen.getByText('2 of 3')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Previous match' })).toBeEnabled()
    expect(screen.getByRole('button', { name: 'Next match' })).toBeEnabled()
    expect(screen.getByRole('button', { name: 'Replace' })).toBeEnabled()
    expect(screen.getByRole('button', { name: 'Replace All' })).toBeEnabled()
  })

  it('invokes query change and navigation callbacks when matches exist', async () => {
    const onQueryChange = vi.fn()
    const onNext = vi.fn()
    const onPrev = vi.fn()

    renderFindReplaceBar({
      mode: 'find',
      matchCount: 2,
      currentMatch: 0,
      onQueryChange,
      onNext,
      onPrev,
    })

    await userEvent.type(screen.getByRole('textbox', { name: 'Find' }), 'abc')
    expect(onQueryChange.mock.calls.map(([value]) => value)).toEqual(['a', 'b', 'c'])

    await userEvent.click(screen.getByRole('button', { name: 'Previous match' }))
    await userEvent.click(screen.getByRole('button', { name: 'Next match' }))

    expect(onPrev).toHaveBeenCalledTimes(1)
    expect(onNext).toHaveBeenCalledTimes(1)
  })

  it('disables match-dependent replace actions when there are no matches', async () => {
    renderFindReplaceBar({
      mode: 'replace',
      matchCount: 0,
      currentMatch: 0,
    })

    expect(screen.getByText('0 of 0')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Previous match' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Next match' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Replace' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Replace All' })).toBeDisabled()
  })

  it('invokes replacement callbacks when matches exist', async () => {
    const onReplacementChange = vi.fn()
    const onReplaceOne = vi.fn()
    const onReplaceAll = vi.fn()

    renderFindReplaceBar({
      mode: 'replace',
      matchCount: 3,
      currentMatch: 0,
      onReplacementChange,
      onReplaceOne,
      onReplaceAll,
    })

    await userEvent.type(screen.getByRole('textbox', { name: 'Replace' }), 'xyz')
    expect(onReplacementChange.mock.calls.map(([value]) => value)).toEqual(['x', 'y', 'z'])

    await userEvent.click(screen.getByRole('button', { name: 'Replace' }))
    await userEvent.click(screen.getByRole('button', { name: 'Replace All' }))

    expect(onReplaceOne).toHaveBeenCalledTimes(1)
    expect(onReplaceAll).toHaveBeenCalledTimes(1)
  })
})
