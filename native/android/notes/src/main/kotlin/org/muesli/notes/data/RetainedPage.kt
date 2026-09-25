package org.muesli.notes.data

import org.muesli.notes.model.NoteListItem

/**
 * One fetched page retained in the repository's bounded sliding window. This
 * data structure is exactly what its own eviction requires: enough to
 * reconstruct an identical request if this page is later evicted and needs
 * reloading, and its own [nextCursor] to continue past it.
 */
data class RetainedPage(
    /** The cursor used to fetch this page; null for the very first page. */
    val requestCursor: String?,
    val rows: List<NoteListItem>,
    /** This page's own continuation cursor; null if this was the collection's last page. */
    val nextCursor: String?,
)

/**
 * A one-level memoized edge key: the exact [requestCursor] to re-issue to
 * reload the page just outside the bounded window on that edge. Distinct
 * from "no key" (`null` at the call site) because [requestCursor] itself may
 * legitimately be null (the page just outside the window is the very first
 * page of the collection).
 */
data class EdgeKey(val requestCursor: String?)
