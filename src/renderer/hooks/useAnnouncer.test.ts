// @vitest-environment jsdom
import { describe, it, expect, afterEach } from 'vitest'
import { render, cleanup, act } from '@testing-library/react'
import { createElement } from 'react'
import { AnnouncerProvider, AriaAnnouncer } from '../components/AriaAnnouncer'
import { useAnnouncer } from './useAnnouncer'

afterEach(() => {
  cleanup()
})

/**
 * Renders the AnnouncerProvider + AriaAnnouncer so the live regions are in
 * the DOM, and returns the `announce` / `announceAssertive` functions obtained
 * from inside the provider tree.
 */
function renderWithAnnouncer() {
  let announce!: (msg: string) => void
  let announceAssertive!: (msg: string) => void

  function Consumer() {
    const ctx = useAnnouncer()
    announce = ctx.announce
    announceAssertive = ctx.announceAssertive
    return null
  }

  render(
    createElement(AnnouncerProvider, null,
      createElement(AriaAnnouncer, null),
      createElement(Consumer, null),
    ),
  )

  return { announce: (msg: string) => act(() => announce(msg)), announceAssertive: (msg: string) => act(() => announceAssertive(msg)) }
}

describe('useAnnouncer', () => {
  it('announce() puts msg in the polite aria-live region', () => {
    const { announce } = renderWithAnnouncer()

    announce('Note saved')

    const polite = document.querySelector('[aria-live="polite"]')
    expect(polite).not.toBeNull()
    expect(polite?.textContent).toBe('Note saved')
  })

  it('announceAssertive() puts msg in the assertive aria-live region', () => {
    const { announceAssertive } = renderWithAnnouncer()

    announceAssertive('Recording started')

    const assertive = document.querySelector('[aria-live="assertive"]')
    expect(assertive).not.toBeNull()
    expect(assertive?.textContent).toBe('Recording started')
  })

  it('a second announce() replaces the first message', () => {
    const { announce } = renderWithAnnouncer()

    announce('First message')
    announce('Second message')

    const polite = document.querySelector('[aria-live="polite"]')
    expect(polite?.textContent).toBe('Second message')
  })

  it('announce() does not affect the assertive region', () => {
    const { announce } = renderWithAnnouncer()

    announce('Status update')

    const assertive = document.querySelector('[aria-live="assertive"]')
    expect(assertive?.textContent).toBe('')
  })

  it('announceAssertive() does not affect the polite region', () => {
    const { announceAssertive } = renderWithAnnouncer()

    announceAssertive('Error!')

    const polite = document.querySelector('[aria-live="polite"]')
    expect(polite?.textContent).toBe('')
  })
})
