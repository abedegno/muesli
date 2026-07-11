// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { Markdown } from './Markdown'

afterEach(cleanup)

describe('Markdown', () => {
  it('renders headings, paragraphs, and the wrapper element', () => {
    const { container } = render(<Markdown source={'# Title\n## Subtitle\n### Small\nPlain paragraph'} />)

    const wrapper = container.querySelector('div.markdown')
    expect(wrapper).not.toBeNull()
    expect(wrapper?.children).toHaveLength(4)
    expect(screen.getByRole('heading', { level: 1, name: 'Title' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 2, name: 'Subtitle' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 3, name: 'Small' })).toBeInTheDocument()
    expect(screen.getByText('Plain paragraph')).toBeInTheDocument()
  })

  it('groups consecutive bullet lines into one list and flushes on blank lines', () => {
    const { container } = render(<Markdown source={'- One\n- Two\n\n- Three\n- Four'} />)

    const wrapper = container.querySelector('div.markdown')
    expect(wrapper).not.toBeNull()
    expect(wrapper?.querySelectorAll('ul')).toHaveLength(2)
    expect(wrapper?.querySelectorAll('li')).toHaveLength(4)
    expect(screen.getByText('One')).toBeInTheDocument()
    expect(screen.getByText('Four')).toBeInTheDocument()
  })

  it('flushes bullet groups when interrupted by a heading or paragraph', () => {
    const { container } = render(
      <Markdown source={'- One\n- Two\n# Title\n- Three\nParagraph\n- Four'} />,
    )

    const wrapper = container.querySelector('div.markdown')
    expect(wrapper).not.toBeNull()
    expect(wrapper?.querySelectorAll('ul')).toHaveLength(3)
    expect(wrapper?.children[0].tagName).toBe('UL')
    expect(wrapper?.children[1].tagName).toBe('H1')
    expect(wrapper?.children[2].tagName).toBe('UL')
    expect(wrapper?.children[3].tagName).toBe('P')
    expect(wrapper?.children[4].tagName).toBe('UL')
  })

  it('passes unprefixed text to renderText and renders its output', () => {
    const renderText = vi.fn((text: string) => <span>WRAPPED:{text}</span>)

    render(<Markdown source={'# Title\n- item\nParagraph'} renderText={renderText} />)

    expect(renderText).toHaveBeenCalledWith('Title')
    expect(renderText).toHaveBeenCalledWith('item')
    expect(renderText).toHaveBeenCalledWith('Paragraph')
    expect(screen.getByText('WRAPPED:Title')).toBeInTheDocument()
    expect(screen.getByText('WRAPPED:item')).toBeInTheDocument()
    expect(screen.getByText('WRAPPED:Paragraph')).toBeInTheDocument()
  })

  it('renders an empty markdown wrapper for empty source', () => {
    const { container } = render(<Markdown source="" />)

    const wrapper = container.querySelector('div.markdown')
    expect(wrapper).not.toBeNull()
    expect(wrapper?.children).toHaveLength(0)
  })

  it('flushes bullet groups at the end of the source', () => {
    const { container } = render(<Markdown source={'- One\n- Two'} />)

    const wrapper = container.querySelector('div.markdown')
    expect(wrapper).not.toBeNull()
    expect(wrapper?.querySelectorAll('ul')).toHaveLength(1)
    expect(wrapper?.querySelectorAll('li')).toHaveLength(2)
  })

  it('renders a mixed source in the expected block order', () => {
    const { container } = render(<Markdown source={'# Title\n- One\n- Two\nParagraph'} />)

    const wrapper = container.querySelector('div.markdown')
    expect(wrapper).not.toBeNull()
    expect(wrapper?.children).toHaveLength(3)
    expect(wrapper?.children[0].tagName).toBe('H1')
    expect(wrapper?.children[1].tagName).toBe('UL')
    expect(wrapper?.children[2].tagName).toBe('P')
    expect(screen.getByRole('heading', { level: 1, name: 'Title' })).toBeInTheDocument()
    expect(screen.getByText('One')).toBeInTheDocument()
    expect(screen.getByText('Paragraph')).toBeInTheDocument()
  })
})
