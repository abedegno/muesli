package org.muesli.notes.list

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.muesli.notes.api.ApiResult
import org.muesli.notes.data.SessionNotesRepository
import org.muesli.notes.testing.FakeAuthenticatedSession
import org.muesli.notes.testing.FakeMobileNotesApi
import org.muesli.notes.testing.FakeNotesRepository
import org.muesli.notes.testing.FakeSessionSource
import org.muesli.notes.testing.testPage

/**
 * View-model tests (issue #768 Task 3): session-change reactions (load on
 * sign-in/account switch, clear on sign-out) and thin forwarding of user
 * intents to the repository, using a recording [FakeNotesRepository] so
 * these tests are independent of the repository's own windowing/gating
 * logic (covered by SessionNotesRepositoryTest).
 */
@OptIn(ExperimentalCoroutinesApi::class)
class NotesListViewModelTest {
    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() {
        Dispatchers.setMain(dispatcher)
    }

    @After
    fun tearDown() {
        Dispatchers.resetMain()
    }

    @Test
    fun `signing in loads the first page for the new session`() = runTest {
        val sessionSource = FakeSessionSource()
        val repository = FakeNotesRepository()
        val viewModel = NotesListViewModel(sessionSource, repository)
        val session = FakeAuthenticatedSession("s1")

        sessionSource.set(session)
        dispatcher.scheduler.advanceUntilIdle()

        assertEquals(listOf(session), repository.loadFirstPageCalls)
    }

    @Test
    fun `signing out clears repository state`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val repository = FakeNotesRepository()
        NotesListViewModel(sessionSource, repository)
        dispatcher.scheduler.advanceUntilIdle()
        assertEquals(listOf(session), repository.loadFirstPageCalls)

        sessionSource.set(null)
        dispatcher.scheduler.advanceUntilIdle()
        assertEquals(1, repository.clearCallCount)
    }

    @Test
    fun `switching accounts reloads exactly once per account, no duplicate fetch on unrelated recomposition`() = runTest {
        val sessionA = FakeAuthenticatedSession("a")
        val sessionB = FakeAuthenticatedSession("b")
        val sessionSource = FakeSessionSource(sessionA)
        val repository = FakeNotesRepository()
        NotesListViewModel(sessionSource, repository)
        dispatcher.scheduler.advanceUntilIdle()

        // Re-emitting the same session (e.g. a recreation re-collecting the
        // flow) must not trigger a second fetch.
        sessionSource.set(sessionA)
        dispatcher.scheduler.advanceUntilIdle()
        assertEquals(listOf(sessionA), repository.loadFirstPageCalls)

        sessionSource.set(sessionB)
        dispatcher.scheduler.advanceUntilIdle()
        assertEquals(listOf(sessionA, sessionB), repository.loadFirstPageCalls)
    }

    @Test
    fun `a same-id but newer-generation session (such as a token refresh) is treated as a transition and reloads`() = runTest {
        val sessionGen1 = FakeAuthenticatedSession("s1", generation = 1)
        val sessionGen2 = FakeAuthenticatedSession("s1", generation = 2)
        val sessionSource = FakeSessionSource(sessionGen1)
        val repository = FakeNotesRepository()
        NotesListViewModel(sessionSource, repository)
        dispatcher.scheduler.advanceUntilIdle()
        assertEquals(listOf(sessionGen1), repository.loadFirstPageCalls)

        sessionSource.set(sessionGen2)
        dispatcher.scheduler.advanceUntilIdle()
        assertEquals(
            "a bumped generation on the same session id must still trigger a fresh load",
            listOf(sessionGen1, sessionGen2),
            repository.loadFirstPageCalls,
        )
    }

    @Test
    fun `loadNextPage, loadPreviousPage, refresh, and retry forward to the repository with the current session`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val repository = FakeNotesRepository()
        val viewModel = NotesListViewModel(sessionSource, repository)
        dispatcher.scheduler.advanceUntilIdle()

        // Each call is allowed to complete before the next fires: since a
        // view model now cancels its previously tracked job before
        // launching a new one (so an overlapping intent can never orphan a
        // still-running request), firing these back-to-back with no yield
        // between them would cancel each one before it ever started.
        viewModel.loadNextPage()
        dispatcher.scheduler.advanceUntilIdle()
        viewModel.loadPreviousPage()
        dispatcher.scheduler.advanceUntilIdle()
        viewModel.refresh()
        dispatcher.scheduler.advanceUntilIdle()
        viewModel.retryInitialLoad()
        dispatcher.scheduler.advanceUntilIdle()

        assertEquals(listOf(session), repository.loadNextPageCalls)
        assertEquals(listOf(session), repository.loadPreviousPageCalls)
        assertEquals(listOf(session), repository.refreshCalls)
        assertEquals(listOf(session, session), repository.loadFirstPageCalls) // sign-in + retry
    }

    @Test
    fun `user intents are a no-op with no active session`() = runTest {
        val sessionSource = FakeSessionSource(null)
        val repository = FakeNotesRepository()
        val viewModel = NotesListViewModel(sessionSource, repository)
        dispatcher.scheduler.advanceUntilIdle()

        viewModel.loadNextPage()
        viewModel.loadPreviousPage()
        viewModel.refresh()
        dispatcher.scheduler.advanceUntilIdle()

        assertEquals(emptyList<Any>(), repository.loadNextPageCalls)
        assertEquals(emptyList<Any>(), repository.loadPreviousPageCalls)
        assertEquals(emptyList<Any>(), repository.refreshCalls)
    }

    @Test
    fun `exposed state is the repository's published state`() = runTest {
        val sessionSource = FakeSessionSource()
        val repository = FakeNotesRepository()
        val viewModel = NotesListViewModel(sessionSource, repository)
        repository.publish(NotesListState.Empty)
        assertEquals(NotesListState.Empty, viewModel.state.value)
    }

    @Test
    fun `signing out cancels the in-flight first-page load, not just discards its result`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val repository = FakeNotesRepository()
        val gate = repository.gateFirstPage()
        NotesListViewModel(sessionSource, repository)
        dispatcher.scheduler.runCurrent() // reach the gated await inside loadFirstPage

        sessionSource.set(null)
        dispatcher.scheduler.advanceUntilIdle()

        assertEquals(
            "sign-out must cancel the coroutine actually running the in-flight first-page load",
            1,
            repository.loadFirstPageCancelledCount,
        )
        assertEquals(1, repository.clearCallCount)
        gate.complete(Unit) // avoid leaking an uncompleted deferred past the test
    }

    @Test
    fun `switching accounts cancels the in-flight first-page load for the former account`() = runTest {
        val sessionA = FakeAuthenticatedSession("a")
        val sessionB = FakeAuthenticatedSession("b")
        val sessionSource = FakeSessionSource(sessionA)
        val repository = FakeNotesRepository()
        val gateA = repository.gateFirstPage()
        NotesListViewModel(sessionSource, repository)
        dispatcher.scheduler.runCurrent() // reach the gated await for account A

        sessionSource.set(sessionB)
        dispatcher.scheduler.advanceUntilIdle()

        assertEquals(
            "an account switch must cancel the former account's in-flight first-page load",
            1,
            repository.loadFirstPageCancelledCount,
        )
        assertEquals(listOf(sessionA, sessionB), repository.loadFirstPageCalls)
        gateA.complete(Unit)
    }

    @Test
    fun `signing out cancels an in-flight next-page load, not just discards its result`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val repository = FakeNotesRepository()
        val viewModel = NotesListViewModel(sessionSource, repository)
        dispatcher.scheduler.advanceUntilIdle() // let the initial (ungated) first-page load finish

        val gate = repository.gateNextPage()
        viewModel.loadNextPage()
        dispatcher.scheduler.runCurrent() // reach the gated await inside loadNextPage

        sessionSource.set(null)
        dispatcher.scheduler.advanceUntilIdle()

        assertEquals(
            "sign-out must cancel the coroutine actually running the in-flight next-page load",
            1,
            repository.loadNextPageCancelledCount,
        )
        gate.complete(Unit)
    }

    @Test
    fun `signing out cancels an in-flight previous-page load, not just discards its result`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val repository = FakeNotesRepository()
        val viewModel = NotesListViewModel(sessionSource, repository)
        dispatcher.scheduler.advanceUntilIdle() // let the initial (ungated) first-page load finish

        val gate = repository.gatePreviousPage()
        viewModel.loadPreviousPage()
        dispatcher.scheduler.runCurrent() // reach the gated await inside loadPreviousPage

        sessionSource.set(null)
        dispatcher.scheduler.advanceUntilIdle()

        assertEquals(
            "sign-out must cancel the coroutine actually running the in-flight previous-page load",
            1,
            repository.loadPreviousPageCancelledCount,
        )
        gate.complete(Unit)
    }

    @Test
    fun `signing out cancels an in-flight refresh, not just discards its result`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val repository = FakeNotesRepository()
        val viewModel = NotesListViewModel(sessionSource, repository)
        dispatcher.scheduler.advanceUntilIdle() // let the initial (ungated) first-page load finish

        val gate = repository.gateRefresh()
        viewModel.refresh()
        dispatcher.scheduler.runCurrent() // reach the gated await inside refresh

        sessionSource.set(null)
        dispatcher.scheduler.advanceUntilIdle()

        assertEquals(
            "sign-out must cancel the coroutine actually running the in-flight refresh",
            1,
            repository.refreshCancelledCount,
        )
        gate.complete(Unit)
    }

    @Test
    fun `a same-id but newer-generation session transition cancels the in-flight first-page load`() = runTest {
        val sessionGen1 = FakeAuthenticatedSession("s1", generation = 1)
        val sessionGen2 = FakeAuthenticatedSession("s1", generation = 2)
        val sessionSource = FakeSessionSource(sessionGen1)
        val repository = FakeNotesRepository()
        val gateGen1 = repository.gateFirstPage()
        NotesListViewModel(sessionSource, repository)
        dispatcher.scheduler.runCurrent() // reach the gated await for generation 1

        sessionSource.set(sessionGen2)
        dispatcher.scheduler.advanceUntilIdle()

        assertEquals(
            "a bumped generation on the same session id must cancel the prior generation's in-flight first-page load",
            1,
            repository.loadFirstPageCancelledCount,
        )
        assertEquals(listOf(sessionGen1, sessionGen2), repository.loadFirstPageCalls)
        gateGen1.complete(Unit)
    }

    @Test
    fun `launching a second list intent while the first is still in-flight cancels the first, not just orphans it`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val repository = FakeNotesRepository()
        val viewModel = NotesListViewModel(sessionSource, repository)
        dispatcher.scheduler.advanceUntilIdle() // let the initial (ungated) first-page load finish

        val gate = repository.gateNextPage()
        viewModel.loadNextPage()
        dispatcher.scheduler.runCurrent() // reach the gated await inside the first loadNextPage call, still in-flight

        // A second, ungated list intent overlapping the first (e.g. because
        // the repository's own gate makes it return near-immediately) must
        // not silently overwrite the tracked job and orphan the still-running
        // first request -- it must cancel it first.
        viewModel.loadPreviousPage()
        dispatcher.scheduler.advanceUntilIdle()

        assertEquals(
            "an overlapping second list intent must cancel the still-running first request's job, not orphan it",
            1,
            repository.loadNextPageCancelledCount,
        )
        gate.complete(Unit)
    }

    // --- overlap regression: cancellation must not leave the repository stuck busy ---
    //
    // The tests above use FakeNotesRepository, which only records that a
    // cancellation happened. That is not enough to catch the real regression:
    // SessionNotesRepository's loadNextPage/loadPreviousPage/refresh each mark
    // their published status busy *before* awaiting the network call, and
    // originally had no recovery path if that await was cancelled out from
    // under them (as launchWithSession does for every overlapping intent) --
    // the status stayed busy forever and every later intent then no-opped
    // against isBusy(). Catching that requires the real ViewModel wired to the
    // real SessionNotesRepository, asserting both that the overlapping
    // request actually reached the API (proof the repository was no longer
    // stuck rejecting it) and that the final published status recovered.

    /**
     * Builds a 5-page window through the real [NotesListViewModel] /
     * [SessionNotesRepository] combo: page 1 (requestCursor null) is evicted
     * once a 5th page is appended past the repository's retained-page cap,
     * which is what gives [NotesListViewModel.loadPreviousPage] a backward
     * edge key to admit.
     */
    private suspend fun buildFivePageWindow(viewModel: NotesListViewModel, api: FakeMobileNotesApi) {
        api.enqueueList(null, ApiResult.Success(testPage("pg1", 1, nextCursor = "c1")))
        dispatcher.scheduler.advanceUntilIdle()
        api.enqueueList("c1", ApiResult.Success(testPage("pg2", 1, nextCursor = "c2")))
        viewModel.loadNextPage()
        dispatcher.scheduler.advanceUntilIdle()
        api.enqueueList("c2", ApiResult.Success(testPage("pg3", 1, nextCursor = "c3")))
        viewModel.loadNextPage()
        dispatcher.scheduler.advanceUntilIdle()
        api.enqueueList("c3", ApiResult.Success(testPage("pg4", 1, nextCursor = "c4")))
        viewModel.loadNextPage()
        dispatcher.scheduler.advanceUntilIdle()
        api.enqueueList("c4", ApiResult.Success(testPage("pg5", 1, nextCursor = "c5")))
        viewModel.loadNextPage()
        dispatcher.scheduler.advanceUntilIdle()
    }

    @Test
    fun `an overlapping next-page intent cancels the first request and the repository recovers to idle, not stuck busy`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val api = FakeMobileNotesApi()
        val repository = SessionNotesRepository(api, sessionSource)
        val viewModel = NotesListViewModel(sessionSource, repository)

        api.enqueueList(null, ApiResult.Success(testPage("p1", 2, nextCursor = "c1")))
        dispatcher.scheduler.advanceUntilIdle() // initial load completes

        val gate = api.gateList("c1")
        api.enqueueList("c1", ApiResult.Success(testPage("orphaned", 1, nextCursor = "cx"))) // consumed then discarded: this request is cancelled
        val job1 = viewModel.loadNextPage()
        dispatcher.scheduler.runCurrent() // reach the gated await inside loadNextPage, still in-flight

        api.enqueueList("c1", ApiResult.Success(testPage("p2", 1, nextCursor = "c2"))) // the overlapping intent's own request
        val job2 = viewModel.loadNextPage()
        dispatcher.scheduler.advanceUntilIdle() // job1 must be cancelled and restore IDLE before job2's admission check runs

        assertTrue("the first request must actually be cancelled, not orphaned", job1!!.isCancelled)

        gate.complete(Unit)
        dispatcher.scheduler.advanceUntilIdle()

        assertEquals(
            "both the cancelled and the overlapping request must have reached the API -- the second is only admitted if the repository recovered from the first request's cancellation instead of staying stuck busy",
            2,
            api.listCalls.count { it.cursor == "c1" },
        )
        val content = viewModel.state.value as NotesListState.Content
        assertEquals(
            "pagination status must not be left stuck at LOADING_NEXT_PAGE by the cancelled first request",
            PaginationStatus.IDLE,
            content.paginationStatus,
        )
        assertEquals(listOf("p1-0", "p1-1", "p2-0"), content.rows.map { it.id })
        assertFalse(job2!!.isCancelled)
    }

    @Test
    fun `an overlapping previous-page intent cancels the first request and the repository recovers to idle, not stuck busy`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val api = FakeMobileNotesApi()
        val repository = SessionNotesRepository(api, sessionSource)
        val viewModel = NotesListViewModel(sessionSource, repository)
        buildFivePageWindow(viewModel, api)
        val callsBeforeOverlap = api.listCalls.count { it.cursor == null }

        val gate = api.gateList(null)
        api.enqueueList(null, ApiResult.Success(testPage("orphaned-prev", 1, nextCursor = "cx"))) // consumed then discarded: this request is cancelled
        val job1 = viewModel.loadPreviousPage()
        dispatcher.scheduler.runCurrent() // reach the gated await inside loadPreviousPage, still in-flight

        api.enqueueList(null, ApiResult.Success(testPage("pg0", 1, nextCursor = "c0"))) // the overlapping intent's own request
        val job2 = viewModel.loadPreviousPage()
        dispatcher.scheduler.advanceUntilIdle() // job1 must be cancelled and restore IDLE before job2's admission check runs

        assertTrue("the first request must actually be cancelled, not orphaned", job1!!.isCancelled)

        gate.complete(Unit)
        dispatcher.scheduler.advanceUntilIdle()

        assertEquals(
            "both the cancelled and the overlapping request must have reached the API -- the second is only admitted if the repository recovered from the first request's cancellation instead of staying stuck busy",
            2,
            api.listCalls.count { it.cursor == null } - callsBeforeOverlap,
        )
        val content = viewModel.state.value as NotesListState.Content
        assertEquals(
            "previous-page status must not be left stuck at LOADING_PREVIOUS_PAGE by the cancelled first request",
            PreviousPageStatus.IDLE,
            content.previousPageStatus,
        )
        assertEquals(listOf("pg0-0", "pg2-0", "pg3-0", "pg4-0"), content.rows.map { it.id })
        assertFalse(job2!!.isCancelled)
    }

    @Test
    fun `an overlapping refresh intent cancels the first request and the repository recovers to idle, not stuck busy`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val api = FakeMobileNotesApi()
        val repository = SessionNotesRepository(api, sessionSource)
        val viewModel = NotesListViewModel(sessionSource, repository)

        api.enqueueList(null, ApiResult.Success(testPage("p1", 2, nextCursor = "c1")))
        dispatcher.scheduler.advanceUntilIdle() // initial load completes
        val callsBeforeOverlap = api.listCalls.count { it.cursor == null }

        val gate = api.gateList(null)
        api.enqueueList(null, ApiResult.Success(testPage("orphaned-refresh", 1, nextCursor = "cx"))) // consumed then discarded: this request is cancelled
        val job1 = viewModel.refresh()
        dispatcher.scheduler.runCurrent() // reach the gated await inside refresh, still in-flight

        api.enqueueList(null, ApiResult.Success(testPage("refreshed", 2, nextCursor = "c2"))) // the overlapping intent's own request
        val job2 = viewModel.refresh()
        dispatcher.scheduler.advanceUntilIdle() // job1 must be cancelled and restore IDLE before job2's admission check runs

        assertTrue("the first request must actually be cancelled, not orphaned", job1!!.isCancelled)

        gate.complete(Unit)
        dispatcher.scheduler.advanceUntilIdle()

        assertEquals(
            "both the cancelled and the overlapping refresh request must have reached the API -- the second is only admitted if the repository recovered from the first request's cancellation instead of staying stuck busy",
            2,
            api.listCalls.count { it.cursor == null } - callsBeforeOverlap,
        )
        val content = viewModel.state.value as NotesListState.Content
        assertEquals(
            "refresh status must not be left stuck at REFRESHING by the cancelled first request",
            RefreshStatus.IDLE,
            content.refreshStatus,
        )
        assertEquals(listOf("refreshed-0", "refreshed-1"), content.rows.map { it.id })
        assertFalse(job2!!.isCancelled)
    }
}
