import { describe, it, expect } from 'vitest'
import { matchesRule, evaluateList, countList, describeRule } from './smartList'
import type { Note, RuleGroup, SmartList } from '../../shared/types'

const note = (over: Partial<Note>): Note => ({
  id: '1', title: '', status: 'ready', created_at: new Date().toISOString(), updated_at: new Date().toISOString(), tags: [], partial_transcript: false, ...over,
})

describe('matchesRule', () => {
  const tagIs: RuleGroup = { op: 'and', children: [{ field: 'tag', operator: 'is', value: '1on1' }] }
  it('tag is / isNot (case-insensitive)', () => {
    expect(matchesRule(note({ tags: ['1ON1'] }), tagIs)).toBe(true)
    expect(matchesRule(note({ tags: ['x'] }), tagIs)).toBe(false)
    expect(matchesRule(note({ tags: ['x'] }), { op: 'and', children: [{ field: 'tag', operator: 'isNot', value: '1on1' }] })).toBe(true)
  })
  it('title contains / equals', () => {
    expect(matchesRule(note({ title: 'Weekly Standup' }), { op: 'and', children: [{ field: 'title', operator: 'contains', value: 'standup' }] })).toBe(true)
    expect(matchesRule(note({ title: 'Weekly Standup' }), { op: 'and', children: [{ field: 'title', operator: 'equals', value: 'weekly standup' }] })).toBe(true)
  })
  it('status is / isNot', () => {
    expect(matchesRule(note({ status: 'failed' }), { op: 'and', children: [{ field: 'status', operator: 'is', value: 'failed' }] })).toBe(true)
    expect(matchesRule(note({ status: 'ready' }), { op: 'and', children: [{ field: 'status', operator: 'isNot', value: 'failed' }] })).toBe(true)
    expect(matchesRule(note({ status: 'failed' }), { op: 'and', children: [{ field: 'status', operator: 'isNot', value: 'failed' }] })).toBe(false)
  })
  it('created withinLastDays', () => {
    const old = new Date(Date.now() - 40 * 86400000).toISOString()
    const r: RuleGroup = { op: 'and', children: [{ field: 'created', operator: 'withinLastDays', value: 30 }] }
    expect(matchesRule(note({ created_at: new Date().toISOString() }), r)).toBe(true)
    expect(matchesRule(note({ created_at: old }), r)).toBe(false)
  })
  it('empty and → true, empty or → false; nested AND/OR', () => {
    expect(matchesRule(note({}), { op: 'and', children: [] })).toBe(true)
    expect(matchesRule(note({}), { op: 'or', children: [] })).toBe(false)
    const nested: RuleGroup = { op: 'or', children: [
      { op: 'and', children: [{ field: 'title', operator: 'contains', value: 'x' }] },
      { field: 'status', operator: 'is', value: 'ready' },
    ] }
    expect(matchesRule(note({ status: 'ready', title: 'no' }), nested)).toBe(true)
  })
  it('folder is: matches note with that folder_id, does not match without it', () => {
    const r: RuleGroup = { op: 'and', children: [{ field: 'folder', operator: 'is', value: 'folder-abc' }] }
    expect(matchesRule(note({ folder_ids: ['folder-abc'] }), r)).toBe(true)
    expect(matchesRule(note({ folder_ids: ['other-folder'] }), r)).toBe(false)
    expect(matchesRule(note({ folder_ids: [] }), r)).toBe(false)
  })
  it('folder isNot: excludes note with that folder, includes note without it', () => {
    const r: RuleGroup = { op: 'and', children: [{ field: 'folder', operator: 'isNot', value: 'folder-abc' }] }
    expect(matchesRule(note({ folder_ids: ['folder-abc'] }), r)).toBe(false)
    expect(matchesRule(note({ folder_ids: ['other-folder'] }), r)).toBe(true)
    expect(matchesRule(note({ folder_ids: [] }), r)).toBe(true)
  })
  it('folder is: note with folder_ids undefined is treated as not-in-folder', () => {
    const r: RuleGroup = { op: 'and', children: [{ field: 'folder', operator: 'is', value: 'folder-abc' }] }
    expect(matchesRule(note({ folder_ids: undefined }), r)).toBe(false)
  })
  it('folder isNot: note with folder_ids undefined is treated as not-in-folder', () => {
    const r: RuleGroup = { op: 'and', children: [{ field: 'folder', operator: 'isNot', value: 'folder-abc' }] }
    expect(matchesRule(note({ folder_ids: undefined }), r)).toBe(true)
  })
})

describe('describeRule', () => {
  it('formats a single tag/title/status/created condition', () => {
    expect(describeRule({ op: 'and', children: [{ field: 'tag', operator: 'is', value: '1on1' }] })).toBe('tag is 1on1')
    expect(describeRule({ op: 'and', children: [{ field: 'title', operator: 'contains', value: 'standup' }] })).toBe('title contains standup')
    expect(describeRule({ op: 'and', children: [{ field: 'status', operator: 'is', value: 'ready' }] })).toBe('status is ready')
    expect(describeRule({ op: 'and', children: [{ field: 'created', operator: 'withinLastDays', value: 7 }] })).toBe('created within last 7 days')
  })
  it('renders operator wording for isNot / equals', () => {
    expect(describeRule({ op: 'and', children: [{ field: 'tag', operator: 'isNot', value: 'x' }] })).toBe('tag is not x')
    expect(describeRule({ op: 'and', children: [{ field: 'title', operator: 'equals', value: 'Daily' }] })).toBe('title = Daily')
  })
  it('appends +N when the rule has more than one condition (AND)', () => {
    expect(describeRule({ op: 'and', children: [
      { field: 'tag', operator: 'is', value: '1on1' },
      { field: 'status', operator: 'is', value: 'ready' },
    ] })).toBe('tag is 1on1 and +1')
  })
  it('appends +N with OR connector when op is or', () => {
    expect(describeRule({ op: 'or', children: [
      { field: 'tag', operator: 'is', value: '1on1' },
      { field: 'status', operator: 'is', value: 'ready' },
    ] })).toBe('tag is 1on1 or +1')
  })
  it('appends +2 with and connector for three conditions', () => {
    expect(describeRule({ op: 'and', children: [
      { field: 'tag', operator: 'is', value: '1on1' },
      { field: 'status', operator: 'is', value: 'ready' },
      { field: 'title', operator: 'contains', value: 'sync' },
    ] })).toBe('tag is 1on1 and +2')
  })
  it('finds the first leaf condition inside a nested group', () => {
    expect(describeRule({ op: 'or', children: [
      { op: 'and', children: [{ field: 'title', operator: 'contains', value: 'sync' }] },
      { field: 'status', operator: 'is', value: 'ready' },
    ] })).toBe('title contains sync or +1')
  })
  it('returns "" for an empty rule', () => {
    expect(describeRule({ op: 'and', children: [] })).toBe('')
  })
})

describe('evaluateList / countList', () => {
  const list: SmartList = { id: 'l', name: 'Ready', created_at: '', rule: { op: 'and', children: [{ field: 'status', operator: 'is', value: 'ready' }] } }
  const notes = [note({ id: 'a', status: 'ready' }), note({ id: 'b', status: 'failed' })]
  it('filters and counts', () => {
    expect(evaluateList(notes, list).map((n) => n.id)).toEqual(['a'])
    expect(countList(notes, list)).toBe(1)
  })
})
