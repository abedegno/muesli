// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AnnouncerProvider, AriaAnnouncer } from './AriaAnnouncer'
import { useAnnouncer } from '../hooks/useAnnouncer'

afterEach(cleanup)

function AnnounceControls() {
  const { announce, announceAssertive } = useAnnouncer()

  return (
    <div>
      <button type="button" onClick={() => announce('Saved draft')}>
        Announce polite
      </button>
      <button type="button" onClick={() => announceAssertive('Upload failed')}>
        Announce assertive
      </button>
      <span>Provider child</span>
    </div>
  )
}

describe('AriaAnnouncer', () => {
  it('renders provider children', () => {
    render(
      <AnnouncerProvider>
        <AnnounceControls />
      </AnnouncerProvider>,
    )

    expect(screen.getByText('Provider child')).toBeInTheDocument()
  })

  it('renders hidden polite and assertive live regions', () => {
    const { container } = render(
      <AnnouncerProvider>
        <AriaAnnouncer />
      </AnnouncerProvider>,
    )

    const liveRegions = container.querySelectorAll('[aria-live]')
    expect(liveRegions).toHaveLength(2)
    expect(liveRegions[0]).toHaveAttribute('aria-live', 'polite')
    expect(liveRegions[1]).toHaveAttribute('aria-live', 'assertive')
    expect(liveRegions[0]).toHaveAttribute('aria-atomic', 'true')
    expect(liveRegions[1]).toHaveAttribute('aria-atomic', 'true')
    for (const region of liveRegions) {
      expect(region).toHaveStyle({
        position: 'absolute',
        width: '1px',
        height: '1px',
        overflow: 'hidden',
        clip: 'rect(0,0,0,0)',
      })
    }
  })

  it('announces messages into the matching live region', async () => {
    const user = userEvent.setup()
    const { container } = render(
      <AnnouncerProvider>
        <AnnounceControls />
        <AriaAnnouncer />
      </AnnouncerProvider>,
    )

    await user.click(screen.getByRole('button', { name: 'Announce polite' }))
    await user.click(screen.getByRole('button', { name: 'Announce assertive' }))

    const liveRegions = container.querySelectorAll('[aria-live]')
    expect(liveRegions[0]).toHaveTextContent('Saved draft')
    expect(liveRegions[1]).toHaveTextContent('Upload failed')
  })
})
