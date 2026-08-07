// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest'
import { cleanup, render } from '@testing-library/react'
import { monogram, monogramColor } from '@/lib/monogram'
import { MonogramAvatar } from './MonogramAvatar'

afterEach(cleanup)

describe('MonogramAvatar', () => {
  it.each([
    ['single name', 'Alice', 'A'],
    ['full name', 'Ada Lovelace', 'A'],
    ['empty string', '', '•'],
    ['punctuation and emoji', '✨...zebra', 'Z'],
    ['non-Latin script', '東京', '•'],
  ])('renders the derived initial for a %s', (_case, label, initial) => {
    const { container } = render(<MonogramAvatar id="person-1" label={label} />)
    expect(container.firstChild).toHaveTextContent(initial)
  })

  it('applies its deterministic tone, inline colors, and default class', () => {
    const id = 'person-42'
    const label = 'Alex Doe'
    const { container } = render(<MonogramAvatar id={id} label={label} />)
    const avatar = container.firstElementChild as HTMLElement
    const { tone } = monogram({ id, label })
    const colors = monogramColor(label)

    expect(avatar.className).toContain(tone === 'teal' ? 'bg-primary/15' : `bg-${tone}-500/15`)
    expect(avatar).toHaveStyle({ backgroundColor: colors.bg, color: colors.fg })
    expect(avatar.className).toContain('h-12 w-12 text-base')
  })

  it('uses a supplied className instead of the default', () => {
    const { container } = render(<MonogramAvatar id="custom" label="Custom" className="h-6 special-avatar" />)
    const avatar = container.firstElementChild as HTMLElement
    expect(avatar.className).toContain('h-6 special-avatar')
    expect(avatar.className).not.toContain('h-12 w-12 text-base')
  })
})
