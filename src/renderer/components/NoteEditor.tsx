import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useEditor, EditorContent, useEditorState } from '@tiptap/react'
import StarterKit from '@tiptap/starter-kit'
import { Extension } from '@tiptap/core'
import { Markdown, type MarkdownStorage } from 'tiptap-markdown'
import { Plugin, PluginKey } from '@tiptap/pm/state'
import { Decoration, DecorationSet } from '@tiptap/pm/view'
import type { Node as PmNode } from '@tiptap/pm/model'
import { debounce } from '@/lib/debounce'
import { useFindReplace, type MatchRange } from '@/lib/useFindReplace'
import { FindReplaceBar } from './FindReplaceBar'

type SaveState = 'idle' | 'saving' | 'saved' | 'error'

// ---------------------------------------------------------------------------
// Plain-text ↔ ProseMirror position mapping
// ---------------------------------------------------------------------------

/**
 * Walks the ProseMirror document and builds an array where
 * `posMap[i]` is the PM doc position corresponding to the i-th character
 * of the plain text returned by `editor.getText()` (blockSeparator="\n").
 *
 * The logic mirrors ProseMirror's own `textBetween`: a single separator is
 * inserted whenever we transition from one block (or out of a block) back into
 * another block, matching exactly what `doc.textBetween(0, size, '\n')` does.
 */
function buildPlainTextPosMap(doc: PmNode): number[] {
  const posMap: number[] = []
  let separated = true  // start true so the first block never gets a separator

  doc.nodesBetween(0, doc.content.size, (node, pos) => {
    if (node.isText && node.text) {
      for (let i = 0; i < node.text.length; i++) {
        posMap.push(pos + i)
      }
      separated = false
      return false  // text nodes have no children
    }
    if (node.isBlock) {
      if (!separated) {
        // Insert the "\n" separator — associate it with the current block pos
        posMap.push(pos)
        separated = true
      }
      return true  // continue into block children
    }
    // hardBreak is an inline leaf that editor.getText() serializes as '\n',
    // so it contributes one character to the plain-text string. Push its PM
    // position so subsequent offsets stay aligned. Do NOT set separated
    // (it is inline content, not a block boundary).
    if (node.type.name === 'hardBreak') {
      posMap.push(pos)
      return false
    }
    // All other inline leaf/atomic nodes (images, etc.) produce nothing in
    // getText() — skip without advancing posMap or touching separated.
    return false
  })

  return posMap
}

/**
 * Map a plain-text match range to a ProseMirror selection range.
 * Returns null if the mapping can't be done (e.g. out-of-bounds).
 */
function matchToPmRange(
  match: MatchRange,
  posMap: number[],
): { from: number; to: number } | null {
  const from = posMap[match.from]
  const lastCharPos = posMap[match.to - 1]
  if (from === undefined || lastCharPos === undefined) return null
  return { from, to: lastCharPos + 1 }
}

// ---------------------------------------------------------------------------
// TipTap highlight extension
// ---------------------------------------------------------------------------

interface FindReplacePluginState {
  matches: MatchRange[]
  currentIndex: number
}

const findReplacePluginKey = new PluginKey<FindReplacePluginState>('findReplace')

const FindHighlightExtension = Extension.create({
  name: 'findHighlight',

  addProseMirrorPlugins() {
    return [
      new Plugin<FindReplacePluginState>({
        key: findReplacePluginKey,

        state: {
          init(): FindReplacePluginState {
            return { matches: [], currentIndex: -1 }
          },
          apply(tr, value): FindReplacePluginState {
            const meta = tr.getMeta(findReplacePluginKey) as FindReplacePluginState | undefined
            return meta ?? value
          },
        },

        props: {
          decorations(state) {
            const pluginState = findReplacePluginKey.getState(state)
            if (!pluginState || pluginState.matches.length === 0) {
              return DecorationSet.empty
            }

            const { matches, currentIndex } = pluginState
            const posMap = buildPlainTextPosMap(state.doc)
            const decorations: Decoration[] = []

            for (let i = 0; i < matches.length; i++) {
              const pmRange = matchToPmRange(matches[i], posMap)
              if (!pmRange) continue
              const cls =
                i === currentIndex
                  ? 'find-highlight find-highlight-current'
                  : 'find-highlight'
              decorations.push(
                Decoration.inline(pmRange.from, pmRange.to, { class: cls }),
              )
            }

            return DecorationSet.create(state.doc, decorations)
          },
        },
      }),
    ]
  },
})

// ---------------------------------------------------------------------------
// NoteEditor component
// ---------------------------------------------------------------------------

// NoteEditor is a focused rich-text editor that serializes to Markdown and
// autosaves (debounced) via onSave. It surfaces an explicit save indicator.
export function NoteEditor({
  initialMarkdown,
  onSave,
  editable = true,
}: {
  initialMarkdown: string
  onSave: (markdown: string) => Promise<void>
  editable?: boolean
}) {
  const [saveState, setSaveState] = useState<SaveState>('idle')

  // ---------- find/replace bar state ----------
  const [findOpen, setFindOpen] = useState(false)
  const [findMode, setFindMode] = useState<'find' | 'replace'>('find')
  const [findQuery, setFindQuery] = useState('')
  const [findReplacement, setFindReplacement] = useState('')

  const save = useMemo(
    () =>
      debounce((md: string) => {
        setSaveState('saving')
        onSave(md)
          .then(() => setSaveState('saved'))
          .catch(() => setSaveState('error'))
      }, 800),
    [onSave],
  )

  const editor = useEditor({
    editable,
    extensions: [StarterKit, Markdown, FindHighlightExtension],
    content: initialMarkdown,
    onUpdate: ({ editor: ed }) => {
      // tiptap-markdown registers `editor.storage.markdown`, but TipTap 3's
      // `Storage` type has no index signature, so bridge through `unknown`.
      const storage = ed.storage as unknown as { markdown: MarkdownStorage }
      save(storage.markdown.getMarkdown())
    },
    editorProps: {
      attributes: {
        class: 'prose-sm max-w-[70ch] focus:outline-none min-h-[12rem]',
        'aria-label': 'Note editor',
      },
    },
  })

  // ---------- current plain text for the hook ----------
  // blockSeparator:'\n' must match the single-slot posMap inserts per block boundary
  // (e.g. "para1\npara2" → match at offset 6 correctly maps to para2's first PM pos).
  // Derive plain text via useEditorState so it recomputes on every editor
  // transaction. TipTap React 3 does NOT re-render on transactions by default,
  // so reading editor.getText() during plain render leaves match offsets stale
  // after an edit/replace, causing wrong-range or missed replacements.
  const plainText =
    useEditorState({
      editor,
      selector: ({ editor }) => (editor ? editor.getText({ blockSeparator: '\n' }) : ''),
    }) ?? ''
  const { matches, currentIndex, next, prev } =
    useFindReplace(plainText, findOpen ? findQuery : '')

  // ---------- push match decorations into the editor ----------
  useEffect(() => {
    if (!editor) return
    editor.view.dispatch(
      editor.state.tr.setMeta(findReplacePluginKey, { matches, currentIndex }),
    )
  }, [editor, matches, currentIndex])

  // ---------- scroll/select the current match ----------
  useEffect(() => {
    if (!editor || !findOpen || currentIndex < 0 || currentIndex >= matches.length)
      return
    const posMap = buildPlainTextPosMap(editor.state.doc)
    const pmRange = matchToPmRange(matches[currentIndex], posMap)
    if (pmRange) {
      editor.commands.setTextSelection({ from: pmRange.from, to: pmRange.to })
    }
  }, [editor, findOpen, currentIndex, matches])

  // ---------- keyboard shortcuts ----------
  const containerRef = useRef<HTMLDivElement>(null)

  const openFind = useCallback((mode: 'find' | 'replace') => {
    setFindMode(mode)
    setFindOpen(true)
  }, [])

  const closeFind = useCallback(() => {
    setFindOpen(false)
    setFindQuery('')
    setFindReplacement('')
  }, [])

  useEffect(() => {
    const el = containerRef.current
    if (!el) return

    const handler = (e: KeyboardEvent) => {
      const isMod = e.metaKey || e.ctrlKey

      if (isMod && e.key === 'f') {
        e.preventDefault()
        openFind('find')
        return
      }
      if (isMod && e.key === 'h') {
        e.preventDefault()
        openFind('replace')
        return
      }
      if (e.key === 'Escape' && findOpen) {
        e.preventDefault()
        closeFind()
        return
      }
      if (e.key === 'Enter' && findOpen && !e.isComposing) {
        e.preventDefault()
        if (e.shiftKey) {
          prev()
        } else {
          next()
        }
      }
    }

    el.addEventListener('keydown', handler, true)
    return () => el.removeEventListener('keydown', handler, true)
  }, [findOpen, openFind, closeFind, next, prev])

  // ---------- replace handlers ----------
  const handleReplaceOne = useCallback(() => {
    if (!editor || currentIndex < 0 || currentIndex >= matches.length) return
    const posMap = buildPlainTextPosMap(editor.state.doc)
    const pmRange = matchToPmRange(matches[currentIndex], posMap)
    if (!pmRange) return
    const chain = editor.chain().focus().setTextSelection({ from: pmRange.from, to: pmRange.to })
    if (findReplacement === '') {
      chain.deleteSelection().run()
    } else {
      chain.insertContent({ type: 'text', text: findReplacement }).run()
    }
  }, [editor, currentIndex, matches, findReplacement])

  const handleReplaceAll = useCallback(() => {
    if (!editor || matches.length === 0) return
    const posMap = buildPlainTextPosMap(editor.state.doc)
    // Apply replacements from last match to first so earlier PM offsets stay valid
    let chain = editor.chain().focus()
    for (let i = matches.length - 1; i >= 0; i--) {
      const pmRange = matchToPmRange(matches[i], posMap)
      if (!pmRange) continue
      chain = chain.setTextSelection({ from: pmRange.from, to: pmRange.to })
      if (findReplacement === '') {
        chain = chain.deleteSelection()
      } else {
        chain = chain.insertContent({ type: 'text', text: findReplacement })
      }
    }
    chain.run()
  }, [editor, matches, findReplacement])

  // Flush any pending save on unmount so navigating away never loses notes.
  const saveRef = useRef(save)
  saveRef.current = save
  useEffect(() => () => saveRef.current.flush(), [])

  return (
    <>
      <style>{`
        .find-highlight { background: rgba(250,200,0,0.35); }
        .find-highlight-current { background: rgba(250,150,0,0.6); outline: 1px solid orange; }
      `}</style>

      <div ref={containerRef}>
        <div className="mb-2 h-4 text-xs text-muted-foreground" aria-live="polite">
          {saveState === 'saving' && 'Saving…'}
          {saveState === 'saved' && 'Saved'}
          {saveState === 'error' && (
            <span className="text-destructive">Save failed — retrying on next edit</span>
          )}
        </div>

        {findOpen && (
          <div className="mb-2">
            <FindReplaceBar
              mode={findMode}
              query={findQuery}
              replacement={findReplacement}
              matchCount={matches.length}
              currentMatch={currentIndex}
              onQueryChange={setFindQuery}
              onReplacementChange={setFindReplacement}
              onNext={next}
              onPrev={prev}
              onReplaceOne={handleReplaceOne}
              onReplaceAll={handleReplaceAll}
              onClose={closeFind}
            />
          </div>
        )}

        <EditorContent editor={editor} />
      </div>
    </>
  )
}
