// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest'
import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HelpTip } from './HelpTip'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})
it('opens help on keyboard focus and dismisses it with Escape', async () => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    }
  )
  render(<HelpTip label="Live transcript">Shows words while recording.</HelpTip>)
  await userEvent.tab()
  expect(screen.getByRole('button', { name: 'Help: Live transcript' })).toHaveFocus()
  expect(await screen.findByRole('tooltip')).toHaveTextContent('Shows words while recording.')
  await userEvent.keyboard('{Escape}')
  await waitFor(() => expect(screen.queryByRole('tooltip')).not.toBeInTheDocument())
})

it('keeps focused help open when keyboard focus scrolls its container, and supports clicking', async () => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    }
  )
  const view = render(
    <div data-testid="scroller">
      <HelpTip label="Final transcript">Processes the full recording.</HelpTip>
    </div>
  )
  await userEvent.tab()
  expect(await screen.findByRole('tooltip')).toBeInTheDocument()
  await act(async () => {
    screen.getByTestId('scroller').dispatchEvent(new Event('scroll'))
  })
  expect(screen.getByRole('tooltip')).toBeInTheDocument()
  await userEvent.keyboard('{Escape}')
  await waitFor(() => expect(screen.queryByRole('tooltip')).not.toBeInTheDocument())
  await userEvent.click(screen.getByRole('button', { name: 'Help: Final transcript' }))
  expect(await screen.findByRole('tooltip')).toHaveTextContent('Processes the full recording.')
  view.unmount()
})
