// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, cleanup, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { RuleEditor } from './RuleEditor'
import type { Folder, SmartList } from '../../shared/types'

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

// UUID-shaped ids per the real folder-condition model (RuleCondition.value is a
// folder id string). Named descriptively rather than random for readability.
const TRASHED_ID = 'b1111111-1111-1111-1111-111111111111'
const MISSING_ID = 'c2222222-2222-2222-2222-222222222222'
const STALE_LIVE_ID = 'd3333333-3333-3333-3333-333333333333'
const KNOWN_ID = 'e4444444-4444-4444-4444-444444444444'
const TRASHED_A_ID = 'f5555555-5555-5555-5555-555555555555'
const LIVE_B_STALE_ID = 'a6666666-6666-6666-6666-666666666666'

function smartList(rule: SmartList['rule'], name = 'Existing list'): SmartList {
  return { id: 'sl-1', name, rule, created_at: '2026-01-01T00:00:00Z' }
}

describe('RuleEditor exceptional folder selections', () => {
  it("shows a selected trashed folder as '<name> (trashed)'", () => {
    const initial = smartList({ op: 'and', children: [{ field: 'folder', operator: 'is', value: TRASHED_ID }] })
    const resolvedFolders: Folder[] = [
      { id: TRASHED_ID, name: 'Acme', parent_id: null, created_at: '2026-01-01T00:00:00Z', deleted_at: '2026-02-01T00:00:00Z' },
    ]
    render(
      <RuleEditor open title="Edit list" initial={initial} knownTags={[]} knownFolders={[]}
        resolvedFolders={resolvedFolders} onSave={vi.fn()} onClose={() => {}} />,
    )
    const select = screen.getByLabelText('condition value') as HTMLSelectElement
    expect(select.value).toBe(TRASHED_ID)
    expect(within(select).getByRole('option', { name: 'Acme (trashed)' })).toBeTruthy()
  })

  it('shows an unresolved selected id as Missing folder (<full id>)', () => {
    const initial = smartList({ op: 'and', children: [{ field: 'folder', operator: 'is', value: MISSING_ID }] })
    render(
      <RuleEditor open title="Edit list" initial={initial} knownTags={[]} knownFolders={[]}
        resolvedFolders={[]} onSave={vi.fn()} onClose={() => {}} />,
    )
    const select = screen.getByLabelText('condition value') as HTMLSelectElement
    expect(select.value).toBe(MISSING_ID)
    expect(within(select).getByRole('option', { name: `Missing folder (${MISSING_ID})` })).toBeTruthy()
  })

  it('shows a resolved row with null deleted_at (stale snapshot / concurrent restore) by its plain name', () => {
    const initial = smartList({ op: 'and', children: [{ field: 'folder', operator: 'is', value: STALE_LIVE_ID }] })
    const resolvedFolders: Folder[] = [
      { id: STALE_LIVE_ID, name: 'Fresh', parent_id: null, created_at: '2026-01-01T00:00:00Z', deleted_at: null },
    ]
    render(
      <RuleEditor open title="Edit list" initial={initial} knownTags={[]} knownFolders={[]}
        resolvedFolders={resolvedFolders} onSave={vi.fn()} onClose={() => {}} />,
    )
    const select = screen.getByLabelText('condition value') as HTMLSelectElement
    expect(select.value).toBe(STALE_LIVE_ID)
    expect(within(select).getByRole('option', { name: 'Fresh' })).toBeTruthy()
    expect(within(select).queryByRole('option', { name: 'Fresh (trashed)' })).toBeNull()
  })

  it('does not duplicate an option when resolvedFolders overlaps knownFolders; the known-folder label wins', () => {
    const initial = smartList({ op: 'and', children: [{ field: 'folder', operator: 'is', value: KNOWN_ID }] })
    const knownFolders: Folder[] = [{ id: KNOWN_ID, name: 'CanonicalName', parent_id: null, created_at: '2026-01-01T00:00:00Z', deleted_at: null }]
    const resolvedFolders: Folder[] = [{ id: KNOWN_ID, name: 'StaleName', parent_id: null, created_at: '2026-01-01T00:00:00Z', deleted_at: '2026-02-01T00:00:00Z' }]
    render(
      <RuleEditor open title="Edit list" initial={initial} knownTags={[]} knownFolders={knownFolders}
        resolvedFolders={resolvedFolders} onSave={vi.fn()} onClose={() => {}} />,
    )
    const select = screen.getByLabelText('condition value') as HTMLSelectElement
    const matching = Array.from(select.querySelectorAll(`option[value="${KNOWN_ID}"]`))
    expect(matching).toHaveLength(1)
    expect(matching[0].textContent).toBe('CanonicalName')
  })

  it('saves the exact original folder id unchanged when the rule is untouched', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    const rule = { op: 'and' as const, children: [{ field: 'folder' as const, operator: 'is' as const, value: TRASHED_ID }] }
    const initial = smartList(rule, 'My list')
    const resolvedFolders: Folder[] = [
      { id: TRASHED_ID, name: 'Acme', parent_id: null, created_at: '2026-01-01T00:00:00Z', deleted_at: '2026-02-01T00:00:00Z' },
    ]
    render(
      <RuleEditor open title="Edit list" initial={initial} knownTags={[]} knownFolders={[]}
        resolvedFolders={resolvedFolders} onSave={onSave} onClose={() => {}} />,
    )
    await userEvent.click(screen.getByRole('button', { name: /^save$/i }))
    expect(onSave).toHaveBeenCalledWith('My list', rule)
  })

  it("a folder resolved for one condition (trashed or live) does not appear in the other condition's dropdown", () => {
    const initial = smartList({
      op: 'and',
      children: [
        { field: 'folder', operator: 'is', value: TRASHED_A_ID },
        { field: 'folder', operator: 'is', value: LIVE_B_STALE_ID },
      ],
    })
    const resolvedFolders: Folder[] = [
      { id: TRASHED_A_ID, name: 'Trashed A', parent_id: null, created_at: '2026-01-01T00:00:00Z', deleted_at: '2026-02-01T00:00:00Z' },
      { id: LIVE_B_STALE_ID, name: 'LiveB', parent_id: null, created_at: '2026-01-01T00:00:00Z', deleted_at: null },
    ]
    render(
      <RuleEditor open title="Edit list" initial={initial} knownTags={[]} knownFolders={[]}
        resolvedFolders={resolvedFolders} onSave={vi.fn()} onClose={() => {}} />,
    )
    const [selectA, selectB] = screen.getAllByLabelText('condition value') as HTMLSelectElement[]

    expect(selectA.value).toBe(TRASHED_A_ID)
    expect(within(selectA).getByRole('option', { name: 'Trashed A (trashed)' })).toBeTruthy()
    expect(within(selectA).queryByRole('option', { name: 'LiveB' })).toBeNull()

    expect(selectB.value).toBe(LIVE_B_STALE_ID)
    expect(within(selectB).getByRole('option', { name: 'LiveB' })).toBeTruthy()
    expect(within(selectB).queryByRole('option', { name: 'Trashed A (trashed)' })).toBeNull()
  })

  it('a new or changed folder condition offers only knownFolders, never a resolvedFolders fallback', async () => {
    const initial = smartList({ op: 'and', children: [] })
    const knownFolders: Folder[] = [{ id: KNOWN_ID, name: 'Known One', parent_id: null, created_at: '2026-01-01T00:00:00Z', deleted_at: null }]
    const resolvedFolders: Folder[] = [
      { id: TRASHED_A_ID, name: 'Trashed A', parent_id: null, created_at: '2026-01-01T00:00:00Z', deleted_at: '2026-02-01T00:00:00Z' },
    ]
    render(
      <RuleEditor open title="Edit list" initial={initial} knownTags={[]} knownFolders={knownFolders}
        resolvedFolders={resolvedFolders} onSave={vi.fn()} onClose={() => {}} />,
    )
    await userEvent.click(screen.getByRole('button', { name: /add condition/i }))
    await userEvent.selectOptions(screen.getByLabelText('condition field'), 'folder')
    const select = screen.getByLabelText('condition value') as HTMLSelectElement
    const values = Array.from(select.querySelectorAll('option')).map((o) => o.value)
    expect(values).toEqual(['', KNOWN_ID])
  })
})
