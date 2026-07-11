// @vitest-environment jsdom
import { afterEach, describe, it, expect, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Button } from './Button'

afterEach(cleanup)

describe('Button', () => {
  it('renders children and fires onClick', async () => {
    const onClick = vi.fn()
    render(<Button onClick={onClick}>Record</Button>)
    await userEvent.click(screen.getByRole('button', { name: 'Record' }))
    expect(onClick).toHaveBeenCalledOnce()
  })
  it('does not fire when disabled', async () => {
    const onClick = vi.fn()
    render(
      <Button disabled onClick={onClick}>
        X
      </Button>,
    )
    await userEvent.click(screen.getByRole('button', { name: 'X' }))
    expect(onClick).not.toHaveBeenCalled()
  })
})
