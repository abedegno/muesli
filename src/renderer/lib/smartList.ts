import type { Note, RuleGroup, RuleCondition, RuleNode, SmartList } from '../../shared/types'
import { isRuleGroup } from '../../shared/types'

function matchesCondition(note: Note, c: RuleCondition): boolean {
  switch (c.field) {
    case 'tag': {
      const has = (note.tags ?? []).some((t) => t.toLowerCase() === String(c.value).toLowerCase())
      return c.operator === 'isNot' ? !has : has
    }
    case 'title': {
      const title = note.title.toLowerCase()
      const v = String(c.value).toLowerCase()
      if (c.operator === 'equals') return title === v
      if (c.operator === 'contains') return title.includes(v)
      return false
    }
    case 'status':
      if (c.operator === 'is') return note.status === c.value
      if (c.operator === 'isNot') return note.status !== c.value
      return false
    case 'created': {
      const days = Number(c.value)
      return Date.now() - new Date(note.created_at).getTime() <= days * 86400000
    }
    case 'folder': {
      const has = (note.folder_ids ?? []).includes(String(c.value))
      return c.operator === 'isNot' ? !has : has
    }
    default:
      return false
  }
}

// matchesRule evaluates a rule group against a note. Empty `and` → true; empty `or` → false.
export function matchesRule(note: Note, group: RuleGroup): boolean {
  const evalNode = (n: RuleNode): boolean => (isRuleGroup(n) ? matchesRule(note, n) : matchesCondition(note, n))
  return group.op === 'and' ? group.children.every(evalNode) : group.children.some(evalNode)
}

export function evaluateList(notes: Note[], list: SmartList): Note[] {
  return notes.filter((n) => matchesRule(n, list.rule))
}

export function countList(notes: Note[], list: SmartList): number {
  return notes.reduce((acc, n) => acc + (matchesRule(n, list.rule) ? 1 : 0), 0)
}

// firstLeaf returns the first leaf RuleCondition in document order, recursing into
// nested groups; null if the rule contains no conditions.
function firstLeaf(group: RuleGroup): RuleCondition | null {
  for (const child of group.children) {
    if (isRuleGroup(child)) {
      const leaf = firstLeaf(child)
      if (leaf) return leaf
    } else {
      return child
    }
  }
  return null
}

// countConditions totals the leaf conditions across nested groups.
function countConditions(group: RuleGroup): number {
  return group.children.reduce((acc, child) => acc + (isRuleGroup(child) ? countConditions(child) : 1), 0)
}

function describeCondition(c: RuleCondition): string {
  switch (c.operator) {
    case 'is': return `${c.field} is ${c.value}`
    case 'isNot': return `${c.field} is not ${c.value}`
    case 'contains': return `${c.field} contains ${c.value}`
    case 'equals': return `${c.field} = ${c.value}`
    case 'withinLastDays': return `${c.field} within last ${c.value} days`
    default: return `${c.field} ${c.value}`
  }
}

// describeRule renders a one-line human summary of a rule's first leaf condition,
// e.g. "tag is 1on1". A trailing " +N" notes how many additional conditions exist.
// An empty rule returns "" (callers should render nothing).
export function describeRule(rule: RuleGroup): string {
  const leaf = firstLeaf(rule)
  if (!leaf) return ''
  const total = countConditions(rule)
  const base = describeCondition(leaf)
  return total > 1 ? `${base} ${rule.op} +${total - 1}` : base
}
