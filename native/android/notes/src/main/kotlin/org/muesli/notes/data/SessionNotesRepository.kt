package org.muesli.notes.data

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import org.muesli.notes.api.ApiFailure
import org.muesli.notes.api.ApiResult
import org.muesli.notes.api.AuthenticatedSession
import org.muesli.notes.api.MobileNotesApi
import org.muesli.notes.api.SessionId
import org.muesli.notes.list.NotesListState
import org.muesli.notes.list.PaginationStatus
import org.muesli.notes.list.PreviousPageStatus
import org.muesli.notes.list.RefreshStatus
import org.muesli.notes.model.NoteDetail
import org.muesli.notes.model.NoteListItem
import org.muesli.notes.session.SessionSource

private const val PAGE_LIMIT = 30
private const val MAX_RETAINED_PAGES = 4

/**
 * The single production [NotesRepository]. Retains at most [MAX_RETAINED_PAGES]
 * pages (<= 120 rows) as an `ArrayDeque`; growing past the cap on one edge
 * evicts the opposite edge and remembers that evicted page's exact
 * [RetainedPage.requestCursor] as an [EdgeKey] so scrolling back to it later
 * reloads identical content -- never the evicted page's own `nextCursor`,
 * which would skip a page. Pagination, previous-page reload, and refresh
 * share one operation gate ([isBusy]); every admitted request is tagged with
 * the active session and a collection generation, and a result is applied
 * only if both still match when it completes (see [isCurrent]).
 */
class SessionNotesRepository(
    private val api: MobileNotesApi,
    private val sessionSource: SessionSource,
) : NotesRepository {

    private val gate = Mutex()
    private val _state = MutableStateFlow<NotesListState>(NotesListState.InitialLoading)
    override val state: StateFlow<NotesListState> = _state.asStateFlow()

    // Session-scoped mutable window. Touched only while holding `gate`
    // (admission) or immediately after an awaited call has been confirmed
    // current via isCurrent (application) -- both are single-writer points.
    private var activeSessionId: SessionId? = null
    private var collectionGeneration: Long = 0
    private val pages = ArrayDeque<RetainedPage>()
    private var cursorBeforeWindow: EdgeKey? = null
    private var cursorAfterWindow: EdgeKey? = null

    override suspend fun loadFirstPage(session: AuthenticatedSession) {
        activeSessionId = session.id
        collectionGeneration += 1
        val requestGeneration = collectionGeneration
        pages.clear()
        cursorBeforeWindow = null
        cursorAfterWindow = null
        _state.value = NotesListState.InitialLoading

        val result = api.listNotes(session, PAGE_LIMIT, cursor = null)
        if (!isCurrent(session, requestGeneration)) return
        when (result) {
            is ApiResult.Success -> installFirstPage(result.value.items, result.value.nextCursor)
            is ApiResult.Failure -> {
                if (result.reason == ApiFailure.SessionEnded) {
                    handleSessionEnded(session)
                } else {
                    _state.value = NotesListState.InitialFailure(result.reason)
                }
            }
        }
    }

    override suspend fun loadNextPage(session: AuthenticatedSession) {
        if (session.id != activeSessionId) return
        val requestGeneration = collectionGeneration
        // `cursor` may legitimately be null (reloading the very first page),
        // so admission is tracked separately rather than via `cursor`'s
        // nullability.
        var admitted = false
        var cursor: String? = null
        gate.withLock {
            val content = _state.value as? NotesListState.Content ?: return@withLock
            if (!content.canLoadNext || isBusy(content)) return@withLock
            val afterWindow = cursorAfterWindow
            cursor = if (afterWindow != null) afterWindow.requestCursor else pages.lastOrNull()?.nextCursor
            admitted = true
            _state.value = content.copy(paginationStatus = PaginationStatus.LOADING_NEXT_PAGE)
        }
        if (!admitted) return

        val consumingAfterWindow = cursorAfterWindow != null
        val result = api.listNotes(session, PAGE_LIMIT, cursor)
        if (!isCurrent(session, requestGeneration)) return
        when (result) {
            is ApiResult.Success -> {
                if (consumingAfterWindow) cursorAfterWindow = null
                appendPage(RetainedPage(cursor, result.value.items, result.value.nextCursor))
                publishContent(paginationStatus = PaginationStatus.IDLE)
            }
            is ApiResult.Failure -> {
                if (result.reason == ApiFailure.SessionEnded) {
                    handleSessionEnded(session)
                } else {
                    publishContent(paginationStatus = PaginationStatus.NEXT_PAGE_FAILED)
                }
            }
        }
    }

    override suspend fun loadPreviousPage(session: AuthenticatedSession) {
        if (session.id != activeSessionId) return
        val requestGeneration = collectionGeneration
        var admitted = false
        var cursor: String? = null
        gate.withLock {
            val content = _state.value as? NotesListState.Content ?: return@withLock
            val beforeWindow = cursorBeforeWindow
            if (beforeWindow == null || isBusy(content)) return@withLock
            cursor = beforeWindow.requestCursor
            admitted = true
            _state.value = content.copy(previousPageStatus = PreviousPageStatus.LOADING_PREVIOUS_PAGE)
        }
        if (!admitted) return

        val result = api.listNotes(session, PAGE_LIMIT, cursor)
        if (!isCurrent(session, requestGeneration)) return
        when (result) {
            is ApiResult.Success -> {
                cursorBeforeWindow = null
                prependPage(RetainedPage(cursor, result.value.items, result.value.nextCursor))
                publishContent(previousPageStatus = PreviousPageStatus.IDLE)
            }
            is ApiResult.Failure -> {
                if (result.reason == ApiFailure.SessionEnded) {
                    handleSessionEnded(session)
                } else {
                    publishContent(previousPageStatus = PreviousPageStatus.PREVIOUS_PAGE_FAILED)
                }
            }
        }
    }

    override suspend fun refresh(session: AuthenticatedSession) {
        if (session.id != activeSessionId) return
        val requestGeneration = collectionGeneration
        val snapshot = gate.withLock {
            val content = _state.value as? NotesListState.Content ?: return@withLock null
            if (isBusy(content)) return@withLock null
            val snap = RefreshSnapshot(
                pages = pages.toList(),
                before = cursorBeforeWindow,
                after = cursorAfterWindow,
                paginationStatus = content.paginationStatus,
                previousPageStatus = content.previousPageStatus,
            )
            _state.value = content.copy(refreshStatus = RefreshStatus.REFRESHING)
            snap
        } ?: return

        val result = api.listNotes(session, PAGE_LIMIT, cursor = null)
        if (!isCurrent(session, requestGeneration)) return
        when (result) {
            is ApiResult.Success -> {
                // A refresh success starts a brand new collection chain: no
                // prior page is "before" a freshly replaced first page.
                collectionGeneration += 1
                installFirstPage(result.value.items, result.value.nextCursor)
            }
            is ApiResult.Failure -> {
                if (result.reason == ApiFailure.SessionEnded) {
                    handleSessionEnded(session)
                    return
                }
                pages.clear()
                pages.addAll(snapshot.pages)
                cursorBeforeWindow = snapshot.before
                cursorAfterWindow = snapshot.after
                publishContent(
                    paginationStatus = snapshot.paginationStatus,
                    previousPageStatus = snapshot.previousPageStatus,
                    refreshStatus = RefreshStatus.REFRESH_FAILED,
                )
            }
        }
    }

    override suspend fun noteDetail(session: AuthenticatedSession, noteId: String): ApiResult<NoteDetail> {
        val result = api.getNote(session, noteId)
        if (result is ApiResult.Failure && result.reason == ApiFailure.SessionEnded) {
            handleSessionEnded(session)
        }
        return result
    }

    override fun clear() {
        activeSessionId = null
        collectionGeneration += 1
        pages.clear()
        cursorBeforeWindow = null
        cursorAfterWindow = null
        _state.value = NotesListState.InitialLoading
    }

    // --- internals ---

    private data class RefreshSnapshot(
        val pages: List<RetainedPage>,
        val before: EdgeKey?,
        val after: EdgeKey?,
        val paginationStatus: PaginationStatus,
        val previousPageStatus: PreviousPageStatus,
    )

    private fun isBusy(content: NotesListState.Content): Boolean =
        content.paginationStatus == PaginationStatus.LOADING_NEXT_PAGE ||
            content.previousPageStatus == PreviousPageStatus.LOADING_PREVIOUS_PAGE ||
            content.refreshStatus == RefreshStatus.REFRESHING

    private fun isCurrent(session: AuthenticatedSession, requestGeneration: Long): Boolean =
        activeSessionId == session.id && collectionGeneration == requestGeneration

    private fun handleSessionEnded(session: AuthenticatedSession) {
        sessionSource.invalidate(session.id)
        clear()
    }

    private fun installFirstPage(items: List<NoteListItem>, nextCursor: String?) {
        pages.clear()
        cursorBeforeWindow = null
        cursorAfterWindow = null
        pages.addLast(RetainedPage(requestCursor = null, rows = items, nextCursor = nextCursor))
        _state.value = if (items.isEmpty() && nextCursor == null) {
            NotesListState.Empty
        } else {
            buildContent(paginationStatus = PaginationStatus.IDLE, previousPageStatus = PreviousPageStatus.IDLE, refreshStatus = RefreshStatus.IDLE)
        }
    }

    /** Appends to the tail (normal continuation or a forward reload of a previously evicted tail page); evicts the head on overflow. */
    private fun appendPage(page: RetainedPage) {
        pages.addLast(page)
        if (pages.size > MAX_RETAINED_PAGES) {
            val evictedHead = pages.removeFirst()
            cursorBeforeWindow = EdgeKey(evictedHead.requestCursor)
        }
    }

    /** Prepends to the head (a backward reload of a previously evicted head page); evicts the tail on overflow. */
    private fun prependPage(page: RetainedPage) {
        pages.addFirst(page)
        if (pages.size > MAX_RETAINED_PAGES) {
            val evictedTail = pages.removeLast()
            cursorAfterWindow = EdgeKey(evictedTail.requestCursor)
        }
    }

    private fun dedupedRows(): List<NoteListItem> {
        val seen = HashSet<String>()
        val out = ArrayList<NoteListItem>()
        for (page in pages) {
            for (row in page.rows) {
                if (seen.add(row.id)) out.add(row)
            }
        }
        return out
    }

    private fun publishContent(
        paginationStatus: PaginationStatus = (_state.value as? NotesListState.Content)?.paginationStatus ?: PaginationStatus.IDLE,
        previousPageStatus: PreviousPageStatus = (_state.value as? NotesListState.Content)?.previousPageStatus ?: PreviousPageStatus.IDLE,
        refreshStatus: RefreshStatus = (_state.value as? NotesListState.Content)?.refreshStatus ?: RefreshStatus.IDLE,
    ) {
        _state.value = buildContent(paginationStatus, previousPageStatus, refreshStatus)
    }

    private fun buildContent(
        paginationStatus: PaginationStatus,
        previousPageStatus: PreviousPageStatus,
        refreshStatus: RefreshStatus,
    ): NotesListState.Content = NotesListState.Content(
        rows = dedupedRows(),
        anchorNoteId = pages.firstOrNull()?.rows?.firstOrNull()?.id,
        canLoadNext = cursorAfterWindow != null || pages.lastOrNull()?.nextCursor != null,
        canLoadPrevious = cursorBeforeWindow != null,
        paginationStatus = paginationStatus,
        previousPageStatus = previousPageStatus,
        refreshStatus = refreshStatus,
    )
}
