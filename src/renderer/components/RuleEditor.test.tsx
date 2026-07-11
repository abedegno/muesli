// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { RuleEditor } from './RuleEditor'

afterEach(cleanup)

describe('RuleEditor', () => {
  it('builds a rule and saves name + rule', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    render(<RuleEditor open title="New list" knownTags={['1on1']} onSave={onSave} onClose={() => {}} />)
    await userEvent.type(screen.getByLabelText('List name'), 'Standups')
    await userEvent.click(screen.getByRole('button', { name: /add condition/i }))
    await userEvent.type(screen.getByLabelText('condition value'), 'standup')
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }))
    expect(onSave).toHaveBeenCalledWith('Standups', {
      op: 'and',
      children: [{ field: 'title', operator: 'contains', value: 'standup' }],
    })
  })
  it('disables save when name is empty', () => {
    render(<RuleEditor open title="New list" knownTags={[]} onSave={vi.fn()} onClose={() => {}} />)
    expect(screen.getByRole('button', { name: /^save$/i })).toBeDisabled()
  })
  it('folder field option exists in the field select', async () => {
    render(<RuleEditor open title="New list" knownTags={[]} onSave={vi.fn()} onClose={() => {}} />)
    await userEvent.click(screen.getByRole('button', { name: /add condition/i }))
    const fieldSelect = screen.getByLabelText('condition field')
    const options = Array.from(fieldSelect.querySelectorAll('option')).map((o) => o.value)
    expect(options).toContain('folder')
  })
})
