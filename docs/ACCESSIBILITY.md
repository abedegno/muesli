# Accessibility Guide

This document describes the accessibility baseline for the Muesli renderer and the conventions for keeping it accessible as the app grows.

---

## Screen-reader announcements — `useAnnouncer`

The `AnnouncerProvider` (in `src/renderer/components/AriaAnnouncer.tsx`) maintains two invisible `aria-live` regions that screen readers watch. Any component inside the provider can get the announce functions via the `useAnnouncer()` hook:

```tsx
import { useAnnouncer } from '@/hooks/useAnnouncer'

function MyComponent() {
  const { announce, announceAssertive } = useAnnouncer()

  const handleSave = async () => {
    await save()
    announce('Saved') // polite — waits for the reader to finish speaking
  }

  const handleError = () => {
    announceAssertive('Upload failed') // assertive — interrupts immediately
  }
}
```

### `announce(msg)` — polite region

Use for **non-urgent status updates** that the user should know about but that do not require immediate attention:

- Save confirmations (`'Title saved'`, `'Saved'`)
- Status transitions that are expected during normal flow (`'Transcribing'`, `'Ready'`)
- Background operation completions

### `announceAssertive(msg)` — assertive region

Use for **urgent state changes** that the user needs to hear right away, even if the reader is currently speaking:

- Recording start / stop (`'Recording started'`, `'Recording stopped'`)
- Errors that block the user (`'Processing failed'`)
- Any action that changes the primary mode of the app

Messages auto-clear after 5 seconds so old text is not re-read on subsequent focus changes.

### Existing wiring (examples)

| Component          | What is announced                                                               | Function used       |
| ------------------ | ------------------------------------------------------------------------------- | ------------------- |
| `RecordControl`    | `'Recording started'` / `'Recording stopped'` on state transitions              | `announceAssertive` |
| `ProcessingBanner` | Status label (`'Transcribing'`, `'Summarizing'`, `'Ready'`, …) on status change | `announce`          |
| `NoteHeader`       | `'Title saved'` after `muesli.updateTitle` resolves                             | `announce`          |

---

## Tab order and label conventions

### Accessible names

Every interactive element must have an accessible name — either visible text, an `aria-label`, or a `<label for>` association:

```tsx
// ✅ Good — visible text
<button>Save</button>

// ✅ Good — aria-label for icon buttons
<button aria-label="Delete note"><Trash size={16} /></button>

// ✅ Good — label association
<label htmlFor="note-title">Title</label>
<input id="note-title" ... />

// ❌ Bad — no accessible name
<button><Trash size={16} /></button>
```

### Semantic HTML

Use native HTML elements wherever possible — they bring keyboard behaviour, role, and focus management for free:

```tsx
// ✅ Use <button> for clickable actions
<button onClick={doThing}>Do thing</button>

// ❌ Never use div/span with onClick as a substitute for a button
<div onClick={doThing}>Do thing</div>
```

### Tab order

Do **not** use positive `tabIndex` values (e.g. `tabIndex={1}`, `tabIndex={2}`). Positive `tabIndex` overrides the natural document order and creates a confusing tab sequence.

- `tabIndex={0}` — makes a normally non-focusable element focusable in document order (use sparingly, prefer native elements).
- `tabIndex={-1}` — removes an element from the tab sequence but still allows programmatic focus (useful for roving-focus widgets such as menus and toolbars).

### Roving-focus widgets

Custom composite widgets (menus, toolbars, tab lists) should use the roving-focus pattern: only the "current" item has `tabIndex={0}`, all others have `tabIndex={-1}`. Arrow keys move focus within the widget. See `NoteHeader` for an example of a keyboard-managed menu using this pattern.

---

## Linting

`eslint-plugin-jsx-a11y` runs on all `src/renderer/**` files as part of `npm run lint` (included in CI). It enforces the rules above automatically.

Rules relaxed for this Electron desktop context:

| Rule                       | Setting | Reason                                                                                                                                  |
| -------------------------- | ------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| `jsx-a11y/anchor-is-valid` | `warn`  | Internal links use React Router — `to` prop not `href`                                                                                  |
| `jsx-a11y/no-autofocus`    | `off`   | `autoFocus` on the first field of a modal dialog is correct ARIA practice (APG §3.1); in a desktop app there are no pop-under surprises |
