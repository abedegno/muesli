// @vitest-environment jsdom
import { afterEach, describe, it, expect, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { SegmentedControl } from './SegmentedControl'

afterEach(cleanup)

describe('SegmentedControl', () => {
  it('reports the selected value and marks active state', async () => {
    const onChange = vi.fn()
    render(
      <SegmentedControl
        ariaLabel="View"
        value="enhanced"
        onValueChange={onChange}
        options={[
          { value: 'enhanced', label: 'Enhanced' },
          { value: 'notes', label: 'My notes' },
        ]}
      />,
    )
    expect(screen.getByRole('radio', { name: 'Enhanced' })).toHaveAttribute('data-state', 'on')
    await userEvent.click(screen.getByRole('radio', { name: 'My notes' }))
    expect(onChange).toHaveBeenCalledWith('notes')
  })
})
