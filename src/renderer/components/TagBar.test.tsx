// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TagBar } from './TagBar'

afterEach(cleanup)

describe('TagBar', () => {
  it('renders existing tags as chips and removes one', async () => {
    const onRemove = vi.fn().mockResolvedValue(undefined)
    render(<TagBar tags={['1on1']} suggestions={['1on1', 'hiring']} onAdd={vi.fn()} onRemove={onRemove} />)
    expect(screen.getByText('#1on1')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /remove 1on1/i }))
    expect(onRemove).toHaveBeenCalledWith('1on1')
  })
  it('adds a tag on Enter', async () => {
    const onAdd = vi.fn().mockResolvedValue(undefined)
    render(<TagBar tags={[]} suggestions={[]} onAdd={onAdd} onRemove={vi.fn()} />)
    await userEvent.type(screen.getByLabelText('Add tag'), 'hiring{Enter}')
    expect(onAdd).toHaveBeenCalledWith('hiring')
  })
  it('ignores blank and duplicate (case-insensitive) adds', async () => {
    const onAdd = vi.fn().mockResolvedValue(undefined)
    render(<TagBar tags={['1on1']} suggestions={[]} onAdd={onAdd} onRemove={vi.fn()} />)
    const input = screen.getByLabelText('Add tag')
    await userEvent.type(input, '   {Enter}')
    await userEvent.type(input, '1ON1{Enter}')
    expect(onAdd).not.toHaveBeenCalled()
  })
})
