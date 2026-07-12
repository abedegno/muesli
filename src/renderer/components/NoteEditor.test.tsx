// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

vi.mock('@tiptap/react', async () => {
  const { useEffect, useState } = await import('react')

  type MockEditor = {
    _markdown: string
    _options: { onUpdate?: ({ editor }: { editor: MockEditor }) => void } | null
    storage: { markdown: { getMarkdown: () => string } }
    view: { dispatch: ReturnType<typeof vi.fn> }
    state: { doc: {}; tr: { setMeta: ReturnType<typeof vi.fn> } }
    commands: { setTextSelection: ReturnType<typeof vi.fn> }
    chain: () => {
      focus: () => unknown
      setTextSelection: () => unknown
      deleteSelection: () => unknown
      insertContent: () => unknown
      run: () => unknown
    }
    getText: () => string
  }

  const createEditor = (content: string): MockEditor => {
    const editor: MockEditor = {
      _markdown: content,
      _options: null,
      storage: {
        markdown: {
          getMarkdown: () => editor._markdown,
        },
      },
      view: {
        dispatch: vi.fn(),
      },
      state: {
        doc: {},
        tr: {
          setMeta: vi.fn(() => ({})),
        },
      },
      commands: {
        setTextSelection: vi.fn(),
      },
      chain: () => {
        const chain = {
          focus: () => chain,
          setTextSelection: () => chain,
          deleteSelection: () => chain,
          insertContent: () => chain,
          run: vi.fn(),
        }
        return chain
      },
      getText: () => editor._markdown,
    }

    return editor
  }

  function useEditor(options: { content: string; onUpdate?: ({ editor }: { editor: MockEditor }) => void }) {
    const [editor] = useState(() => createEditor(options.content))
    editor._options = options
    return editor
  }

  function useEditorState<T>({
    editor,
    selector,
  }: {
    editor: MockEditor | null
    selector: (args: { editor: MockEditor | null }) => T
  }) {
    return selector({ editor })
  }

  function EditorContent({ editor }: { editor: MockEditor | null }) {
    const [text, setText] = useState(editor?._markdown ?? '')

    useEffect(() => {
      setText(editor?._markdown ?? '')
    }, [editor])

    return (
      <div
        aria-label="Note editor"
        contentEditable
        suppressContentEditableWarning
        onInput={(event) => {
          if (!editor) return
          const next = event.currentTarget.textContent ?? ''
          editor._markdown = next
          setText(next)
          editor._options?.onUpdate?.({ editor })
        }}
      >
        {text}
      </div>
    )
  }

  return { EditorContent, useEditor, useEditorState }
})

import { NoteEditor } from './NoteEditor'

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

describe('NoteEditor', () => {
  it('renders the editor surface with the initial markdown', () => {
    render(<NoteEditor initialMarkdown="Hello world" onSave={vi.fn().mockResolvedValue(undefined)} />)

    expect(screen.getByLabelText('Note editor')).toBeInTheDocument()
    expect(screen.getByText('Hello world')).toBeInTheDocument()
    expect(screen.queryByText('Saving…')).not.toBeInTheDocument()
    expect(screen.queryByText('Saved')).not.toBeInTheDocument()
    expect(screen.queryByText(/Save failed/i)).not.toBeInTheDocument()
  })

  it('opens and closes the find bar from keyboard shortcuts', async () => {
    const user = userEvent.setup()

    render(<NoteEditor initialMarkdown="Hello world" onSave={vi.fn().mockResolvedValue(undefined)} />)

    await user.click(screen.getByLabelText('Note editor'))
    await user.keyboard('{Control>}f{/Control}')

    expect(screen.getByRole('textbox', { name: 'Find' })).toBeInTheDocument()

    await user.keyboard('{Escape}')

    expect(screen.queryByRole('textbox', { name: 'Find' })).not.toBeInTheDocument()
  })

  it('debounces edits before calling onSave and updates the save status', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)

    render(<NoteEditor initialMarkdown="Hello" onSave={onSave} />)

    const editor = screen.getByLabelText('Note editor')
    await act(async () => {
      editor.textContent = 'Hello!'
      fireEvent.input(editor)
      await new Promise((resolve) => setTimeout(resolve, 900))
    })
    expect(onSave).toHaveBeenCalledTimes(1)

    expect(onSave.mock.calls[0][0]).toContain('!')
    expect(await screen.findByText('Saved')).toBeInTheDocument()
  })
})
