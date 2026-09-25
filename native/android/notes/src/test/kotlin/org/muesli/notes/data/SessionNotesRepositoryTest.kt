package org.muesli.notes.data

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.muesli.notes.api.ApiFailure
import org.muesli.notes.api.ApiResult
import org.muesli.notes.list.NotesListState
import org.muesli.notes.list.PaginationStatus
import org.muesli.notes.list.PreviousPageStatus
import org.muesli.notes.list.RefreshStatus
import org.muesli.notes.testing.FakeAuthenticatedSession
import org.muesli.notes.testing.FakeMobileNotesApi
import org.muesli.notes.testing.FakeSessionSource
import org.muesli.notes.testing.testNoteItem
import org.muesli.notes.testing.testPage

/**
 * Repository tests (issue #768 Task 3): first-page outcomes, pagination and
 * refresh gating/ignoring, failure preservation and retry, refresh atomic
 * replace/restore, stale-session/generation rejection, the bounded sliding
 * window with opposite-edge eviction and backward/forward reload, and
 * sign-out clearing with late-result rejection.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class SessionNotesRepositoryTest {

    private fun harness() = FakeMobileNotesApi().let { api ->
        val sessionSource = FakeSessionSource()
        Triple(api, sessionSource, SessionNotesRepository(api, sessionSource))
    }

    /**
     * Builds a 5-page window (pages 2-5 retained, backward key = page 1's
     * null requestCursor) so [SessionNotesRepository.loadPreviousPage] has
     * something to admit, for tests that exercise the backward-direction
     * operation gate.
     */
    private suspend fun buildFivePageWindowWithBackwardKey(
        api: FakeMobileNotesApi,
        repo: SessionNotesRepository,
        session: FakeAuthenticatedSession,
    ) {
        api.enqueueList(null, ApiResult.Success(testPage("pg1", 1, nextCursor = "c1")))
        api.enqueueList("c1", ApiResult.Success(testPage("pg2", 1, nextCursor = "c2")))
        api.enqueueList("c2", ApiResult.Success(testPage("pg3", 1, nextCursor = "c3")))
        api.enqueueList("c3", ApiResult.Success(testPage("pg4", 1, nextCursor = "c4")))
        api.enqueueList("c4", ApiResult.Success(testPage("pg5", 1, nextCursor = "c5")))
        repo.loadFirstPage(session)
        repeat(4) { repo.loadNextPage(session) }
    }

    // --- first page ---

    @Test
    fun `first page success publishes Content`() = runTest {
        val (api, _, repo) = harness()
        val session = FakeAuthenticatedSession("s1")
        api.enqueueList(null, ApiResult.Success(testPage("p1", 2, nextCursor = "c1")))
        repo.loadFirstPage(session)
        val content = repo.state.value as NotesListState.Content
        assertEquals(listOf("p1-0", "p1-1"), content.rows.map { it.id })
        assertTrue(content.canLoadNext)
        assertFalse(content.canLoadPrevious)
    }

    @Test
    fun `first page empty publishes Empty, not a failure`() = runTest {
        val (api, _, repo) = harness()
        val session = FakeAuthenticatedSession("s1")
        api.enqueueList(null, ApiResult.Success(testPage("p", 0, nextCursor = null)))
        repo.loadFirstPage(session)
        assertEquals(NotesListState.Empty, repo.state.value)
    }

    @Test
    fun `first page failure publishes InitialFailure without claiming empty`() = runTest {
        val (api, _, repo) = harness()
        val session = FakeAuthenticatedSession("s1")
        api.enqueueList(null, ApiResult.Failure(ApiFailure.Http(500)))
        repo.loadFirstPage(session)
        assertEquals(NotesListState.InitialFailure(ApiFailure.Http(500)), repo.state.value)
    }

    // --- pagination ---

    @Test
    fun `next page appends and de-duplicates preserving order`() = runTest {
        val (api, _, repo) = harness()
        val session = FakeAuthenticatedSession("s1")
        api.enqueueList(null, ApiResult.Success(testPage("p1", 2, nextCursor = "c1")))
        repo.loadFirstPage(session)
        api.enqueueList("c1", ApiResult.Success(testPage("p2", 2, nextCursor = null)))
        repo.loadNextPage(session)
        val content = repo.state.value as NotesListState.Content
        assertEquals(listOf("p1-0", "p1-1", "p2-0", "p2-1"), content.rows.map { it.id })
        assertFalse(content.canLoadNext)
        assertEquals(PaginationStatus.IDLE, content.paginationStatus)
    }

    @Test
    fun `pagination failure preserves rows and retries the exact same cursor`() = runTest {
        val (api, _, repo) = harness()
        val session = FakeAuthenticatedSession("s1")
        api.enqueueList(null, ApiResult.Success(testPage("p1", 2, nextCursor = "c1")))
        repo.loadFirstPage(session)
        api.enqueueList("c1", ApiResult.Failure(ApiFailure.Http(503)))
        repo.loadNextPage(session)
        var content = repo.state.value as NotesListState.Content
        assertEquals(PaginationStatus.NEXT_PAGE_FAILED, content.paginationStatus)
        assertEquals(listOf("p1-0", "p1-1"), content.rows.map { it.id })

        api.enqueueList("c1", ApiResult.Success(testPage("p2", 1, nextCursor = null)))
        repo.loadNextPage(session)
        content = repo.state.value as NotesListState.Content
        assertEquals(PaginationStatus.IDLE, content.paginationStatus)
        assertEquals(listOf("p1-0", "p1-1", "p2-0"), content.rows.map { it.id })
        assertEquals(2, api.listCalls.count { it.cursor == "c1" })
    }

    @Test
    fun `duplicate next-page trigger while one is in flight issues no second request`() = runTest {
        val (api, _, repo) = harness()
        val session = FakeAuthenticatedSession("s1")
        api.enqueueList(null, ApiResult.Success(testPage("p1", 2, nextCursor = "c1")))
        repo.loadFirstPage(session)

        api.enqueueList("c1", ApiResult.Success(testPage("p2", 1, nextCursor = null)))
        val gate = api.gateList("c1")
        val job1 = launch { repo.loadNextPage(session) }
        runCurrent()
        val job2 = launch { repo.loadNextPage(session) }
        runCurrent()
        assertEquals(1, api.listCalls.count { it.cursor == "c1" })

        gate.complete(Unit)
        job1.join()
        job2.join()
        val content = repo.state.value as NotesListState.Content
        assertEquals(PaginationStatus.IDLE, content.paginationStatus)
        assertEquals(listOf("p1-0", "p1-1", "p2-0"), content.rows.map { it.id })
    }

    // --- refresh vs pagination mutual exclusion ---

    @Test
    fun `refresh trigger during in-flight pagination is ignored, both outcomes`() = runTest {
        for (paginationSucceeds in listOf(true, false)) {
            val (api, _, repo) = harness()
            val session = FakeAuthenticatedSession("s1")
            api.enqueueList(null, ApiResult.Success(testPage("p1", 2, nextCursor = "c1")))
            repo.loadFirstPage(session)

            if (paginationSucceeds) {
                api.enqueueList("c1", ApiResult.Success(testPage("p2", 1, nextCursor = null)))
            } else {
                api.enqueueList("c1", ApiResult.Failure(ApiFailure.Http(500)))
            }
            val gate = api.gateList("c1")
            val paginationJob = launch { repo.loadNextPage(session) }
            runCurrent()

            val refreshJob = launch { repo.refresh(session) }
            runCurrent()
            assertEquals("refresh must not call the API while pagination is in flight", 1, api.listCalls.count { it.cursor == null })

            gate.complete(Unit)
            paginationJob.join()
            refreshJob.join()

            val content = repo.state.value as NotesListState.Content
            assertEquals(RefreshStatus.IDLE, content.refreshStatus)
            assertEquals(
                if (paginationSucceeds) PaginationStatus.IDLE else PaginationStatus.NEXT_PAGE_FAILED,
                content.paginationStatus,
            )
        }
    }

    @Test
    fun `next-page trigger during in-flight refresh is ignored, both outcomes`() = runTest {
        for (refreshSucceeds in listOf(true, false)) {
            val (api, _, repo) = harness()
            val session = FakeAuthenticatedSession("s1")
            api.enqueueList(null, ApiResult.Success(testPage("p1", 2, nextCursor = "c1")))
            repo.loadFirstPage(session)

            if (refreshSucceeds) {
                api.enqueueList(null, ApiResult.Success(testPage("p1b", 1, nextCursor = "c9")))
            } else {
                api.enqueueList(null, ApiResult.Failure(ApiFailure.Http(500)))
            }
            val gate = api.gateList(null)
            val refreshJob = launch { repo.refresh(session) }
            runCurrent()

            val nextPageJob = launch { repo.loadNextPage(session) }
            runCurrent()
            assertEquals("next-page must not call the API while refresh is in flight", 0, api.listCalls.count { it.cursor == "c1" })

            gate.complete(Unit)
            refreshJob.join()
            nextPageJob.join()

            val state = repo.state.value
            if (refreshSucceeds) {
                val content = state as NotesListState.Content
                assertEquals(listOf("p1b-0"), content.rows.map { it.id })
                assertEquals(PaginationStatus.IDLE, content.paginationStatus)
            } else {
                val content = state as NotesListState.Content
                assertEquals(RefreshStatus.REFRESH_FAILED, content.refreshStatus)
                assertEquals(listOf("p1-0", "p1-1"), content.rows.map { it.id })
            }
        }
    }

    @Test
    fun `duplicate previous-page trigger while one is in flight issues no second request`() = runTest {
        val (api, _, repo) = harness()
        val session = FakeAuthenticatedSession("s1")
        buildFivePageWindowWithBackwardKey(api, repo, session)

        api.enqueueList(null, ApiResult.Success(testPage("pg1", 1, nextCursor = "c1")))
        val callsBefore = api.listCalls.size
        val gate = api.gateList(null)
        val job1 = launch { repo.loadPreviousPage(session) }
        runCurrent()
        val job2 = launch { repo.loadPreviousPage(session) }
        runCurrent()
        assertEquals(callsBefore + 1, api.listCalls.size)

        gate.complete(Unit)
        job1.join()
        job2.join()
        val content = repo.state.value as NotesListState.Content
        assertEquals(PreviousPageStatus.IDLE, content.previousPageStatus)
        assertEquals(listOf("pg1-0", "pg2-0", "pg3-0", "pg4-0"), content.rows.map { it.id })
    }

    @Test
    fun `refresh trigger during in-flight previous-page is ignored, both outcomes`() = runTest {
        for (previousPageSucceeds in listOf(true, false)) {
            val (api, _, repo) = harness()
            val session = FakeAuthenticatedSession("s1")
            buildFivePageWindowWithBackwardKey(api, repo, session)

            if (previousPageSucceeds) {
                api.enqueueList(null, ApiResult.Success(testPage("pg1", 1, nextCursor = "c1")))
            } else {
                api.enqueueList(null, ApiResult.Failure(ApiFailure.Http(500)))
            }
            val gate = api.gateList(null)
            val previousJob = launch { repo.loadPreviousPage(session) }
            runCurrent()

            val callsBeforeRefreshAttempt = api.listCalls.size
            val refreshJob = launch { repo.refresh(session) }
            runCurrent()
            assertEquals(
                "refresh must not call the API while previous-page is in flight",
                callsBeforeRefreshAttempt,
                api.listCalls.size,
            )

            gate.complete(Unit)
            previousJob.join()
            refreshJob.join()

            val content = repo.state.value as NotesListState.Content
            assertEquals(RefreshStatus.IDLE, content.refreshStatus)
            assertEquals(
                if (previousPageSucceeds) PreviousPageStatus.IDLE else PreviousPageStatus.PREVIOUS_PAGE_FAILED,
                content.previousPageStatus,
            )
        }
    }

    @Test
    fun `previous-page trigger during in-flight refresh is ignored, both outcomes`() = runTest {
        for (refreshSucceeds in listOf(true, false)) {
            val (api, _, repo) = harness()
            val session = FakeAuthenticatedSession("s1")
            buildFivePageWindowWithBackwardKey(api, repo, session)

            if (refreshSucceeds) {
                api.enqueueList(null, ApiResult.Success(testPage("q1", 1, nextCursor = "d1")))
            } else {
                api.enqueueList(null, ApiResult.Failure(ApiFailure.Http(500)))
            }
            val gate = api.gateList(null)
            val refreshJob = launch { repo.refresh(session) }
            runCurrent()

            val callsBeforePreviousAttempt = api.listCalls.size
            val previousJob = launch { repo.loadPreviousPage(session) }
            runCurrent()
            assertEquals(
                "previous-page must not call the API while refresh is in flight",
                callsBeforePreviousAttempt,
                api.listCalls.size,
            )

            gate.complete(Unit)
            refreshJob.join()
            previousJob.join()

            val state = repo.state.value
            val content = state as NotesListState.Content
            if (refreshSucceeds) {
                assertEquals(listOf("q1-0"), content.rows.map { it.id })
                assertEquals(PreviousPageStatus.IDLE, content.previousPageStatus)
            } else {
                assertEquals(RefreshStatus.REFRESH_FAILED, content.refreshStatus)
                assertEquals(listOf("pg2-0", "pg3-0", "pg4-0", "pg5-0"), content.rows.map { it.id })
            }
        }
    }

    // --- refresh atomic replace / restore ---

    @Test
    fun `refresh success atomically replaces rows and continuation, resets pagination`() = runTest {
        val (api, _, repo) = harness()
        val session = FakeAuthenticatedSession("s1")
        api.enqueueList(null, ApiResult.Success(testPage("p1", 2, nextCursor = "c1")))
        repo.loadFirstPage(session)
        api.enqueueList("c1", ApiResult.Failure(ApiFailure.Http(500)))
        repo.loadNextPage(session) // leaves paginationStatus = NEXT_PAGE_FAILED

        api.enqueueList(null, ApiResult.Success(testPage("q1", 3, nextCursor = "d1")))
        repo.refresh(session)

        val content = repo.state.value as NotesListState.Content
        assertEquals(listOf("q1-0", "q1-1", "q1-2"), content.rows.map { it.id })
        assertEquals(PaginationStatus.IDLE, content.paginationStatus)
        assertEquals(RefreshStatus.IDLE, content.refreshStatus)
        assertFalse("a fresh refresh has nothing before its new first page", content.canLoadPrevious)
        assertTrue(content.canLoadNext)
    }

    @Test
    fun `a pagination result from a superseded generation is discarded`() = runTest {
        val (api, _, repo) = harness()
        val session = FakeAuthenticatedSession("s1")
        api.enqueueList(null, ApiResult.Success(testPage("p1", 2, nextCursor = "c1")))
        repo.loadFirstPage(session)

        api.enqueueList("c1", ApiResult.Success(testPage("p2", 1, nextCursor = null)))
        val gate = api.gateList("c1")
        val paginationJob = launch { repo.loadNextPage(session) }
        runCurrent() // pagination is now in flight, awaiting the gate

        // Meanwhile a fresh collection chain starts (e.g. the screen forced a
        // full reload), bumping the collection generation before pagination's
        // response arrives.
        api.enqueueList(null, ApiResult.Success(testPage("q1", 1, nextCursor = null)))
        repo.loadFirstPage(session)
        val freshContent = repo.state.value as NotesListState.Content
        assertEquals(listOf("q1-0"), freshContent.rows.map { it.id })

        // The stale pagination result must be discarded when it completes.
        gate.complete(Unit)
        paginationJob.join()
        val finalContent = repo.state.value as NotesListState.Content
        assertEquals(
            "a superseded generation's pagination result must never apply",
            listOf("q1-0"),
            finalContent.rows.map { it.id },
        )
    }

    @Test
    fun `a same-id but older-generation session (such as a token refresh) is rejected outright`() = runTest {
        val (api, _, repo) = harness()
        val sessionGen1 = FakeAuthenticatedSession("s1", generation = 1)
        val sessionGen2 = FakeAuthenticatedSession("s1", generation = 2)
        api.enqueueList(null, ApiResult.Success(testPage("p1", 1, nextCursor = "c1")))
        repo.loadFirstPage(sessionGen1)
        api.enqueueList(null, ApiResult.Success(testPage("q1", 1, nextCursor = null)))
        repo.loadFirstPage(sessionGen2) // same id, bumped generation -- e.g. token refresh

        // A call still carrying the stale generation-1 session must be
        // rejected before ever reaching the network, not merely have its
        // eventual result discarded.
        repo.loadNextPage(sessionGen1)
        repo.loadPreviousPage(sessionGen1)
        repo.refresh(sessionGen1)
        assertTrue(
            "no request should have been admitted for the stale-generation session",
            api.listCalls.none { it.cursor == "c1" },
        )
        val content = repo.state.value as NotesListState.Content
        assertEquals(listOf("q1-0"), content.rows.map { it.id })
    }

    @Test
    fun `a pagination result from a same-id but superseded-generation session is discarded`() = runTest {
        val (api, _, repo) = harness()
        val sessionGen1 = FakeAuthenticatedSession("s1", generation = 1)
        val sessionGen2 = FakeAuthenticatedSession("s1", generation = 2)
        api.enqueueList(null, ApiResult.Success(testPage("p1", 2, nextCursor = "c1")))
        repo.loadFirstPage(sessionGen1)

        api.enqueueList("c1", ApiResult.Success(testPage("p2", 1, nextCursor = null)))
        val gate = api.gateList("c1")
        val paginationJob = launch { repo.loadNextPage(sessionGen1) }
        runCurrent() // pagination is in flight, tagged with generation 1

        // The host refreshes the token: a new AuthenticatedSession with the
        // SAME id but a bumped generation replaces the old one, without any
        // change to the session id the earlier gap in this check missed.
        api.enqueueList(null, ApiResult.Success(testPage("q1", 1, nextCursor = null)))
        repo.loadFirstPage(sessionGen2)
        val freshContent = repo.state.value as NotesListState.Content
        assertEquals(listOf("q1-0"), freshContent.rows.map { it.id })

        gate.complete(Unit)
        paginationJob.join()
        val finalContent = repo.state.value as NotesListState.Content
        assertEquals(
            "a same-id but stale-generation pagination result must never apply",
            listOf("q1-0"),
            finalContent.rows.map { it.id },
        )
    }

    @Test
    fun `a late detail 401 for a same-id but superseded-generation session does not invalidate the replacement`() = runTest {
        val (api, sessionSource, repo) = harness()
        val sessionGen1 = FakeAuthenticatedSession("s1", generation = 1)
        val sessionGen2 = FakeAuthenticatedSession("s1", generation = 2)
        sessionSource.set(sessionGen1)

        // sessionSource has already moved on to generation 2 (e.g. a token
        // refresh) by the time a detail request still tagged with the old
        // generation-1 session comes back 401.
        sessionSource.set(sessionGen2)

        api.enqueueDetail("n1", ApiResult.Failure(ApiFailure.SessionEnded))
        val result = repo.noteDetail(sessionGen1, "n1")

        assertEquals(ApiFailure.SessionEnded, (result as ApiResult.Failure).reason)
        assertTrue(
            "a stale-generation 401 must not invalidate the current (generation 2) session",
            sessionSource.invalidated.isEmpty(),
        )
        assertEquals(sessionGen2, sessionSource.session.value)
    }

    @Test
    fun `refresh failure restores the exact pre-refresh snapshot and retry succeeds`() = runTest {
        val (api, _, repo) = harness()
        val session = FakeAuthenticatedSession("s1")
        api.enqueueList(null, ApiResult.Success(testPage("p1", 2, nextCursor = "c1")))
        repo.loadFirstPage(session)
        api.enqueueList("c1", ApiResult.Success(testPage("p2", 1, nextCursor = null)))
        repo.loadNextPage(session)
        val before = repo.state.value as NotesListState.Content

        api.enqueueList(null, ApiResult.Failure(ApiFailure.Http(500)))
        repo.refresh(session)
        val afterFailure = repo.state.value as NotesListState.Content
        assertEquals(before.rows.map { it.id }, afterFailure.rows.map { it.id })
        assertEquals(before.canLoadNext, afterFailure.canLoadNext)
        assertEquals(before.canLoadPrevious, afterFailure.canLoadPrevious)
        assertEquals(RefreshStatus.REFRESH_FAILED, afterFailure.refreshStatus)
        assertEquals(PaginationStatus.IDLE, afterFailure.paginationStatus)

        api.enqueueList(null, ApiResult.Success(testPage("q1", 1, nextCursor = null)))
        repo.refresh(session)
        val afterRetry = repo.state.value as NotesListState.Content
        assertEquals(listOf("q1-0"), afterRetry.rows.map { it.id })
        assertEquals(RefreshStatus.IDLE, afterRetry.refreshStatus)
    }

    // --- 401 / session-ended ---

    @Test
    fun `SessionEnded invalidates the session and clears state`() = runTest {
        val (api, sessionSource, repo) = harness()
        val session = FakeAuthenticatedSession("s1")
        sessionSource.set(session)
        api.enqueueList(null, ApiResult.Failure(ApiFailure.SessionEnded))
        repo.loadFirstPage(session)
        assertEquals(NotesListState.InitialLoading, repo.state.value)
        assertTrue(sessionSource.invalidated.contains(session.id))
    }

    // --- sign-out / stale session ---

    @Test
    fun `clear resets to InitialLoading and a late in-flight result is discarded`() = runTest {
        val (api, _, repo) = harness()
        val session = FakeAuthenticatedSession("s1")
        api.enqueueList(null, ApiResult.Success(testPage("p1", 2, nextCursor = "c1")))
        repo.loadFirstPage(session)

        api.enqueueList("c1", ApiResult.Success(testPage("p2", 1, nextCursor = null)))
        val gate = api.gateList("c1")
        val job = launch { repo.loadNextPage(session) }
        runCurrent()

        repo.clear()
        assertEquals(NotesListState.InitialLoading, repo.state.value)

        gate.complete(Unit)
        job.join()
        assertEquals("a result for a cleared session must never resurrect state", NotesListState.InitialLoading, repo.state.value)
    }

    @Test
    fun `a stale-session next-page call for a no-longer-active session is a no-op`() = runTest {
        val (api, _, repo) = harness()
        val sessionA = FakeAuthenticatedSession("a")
        val sessionB = FakeAuthenticatedSession("b")
        api.enqueueList(null, ApiResult.Success(testPage("a1", 1, nextCursor = "ca")))
        repo.loadFirstPage(sessionA)
        api.enqueueList(null, ApiResult.Success(testPage("b1", 1, nextCursor = "cb")))
        repo.loadFirstPage(sessionB) // account switch

        // A leftover call using the old session must not mutate state.
        repo.loadNextPage(sessionA)
        val content = repo.state.value as NotesListState.Content
        assertEquals(listOf("b1-0"), content.rows.map { it.id })
        assertTrue(api.listCalls.none { it.cursor == "ca" })
    }

    // --- bounded window: eviction + backward/forward reload ---

    @Test
    fun `five-page boundary evicts head, backward-reloads it, then forward-reloads the evicted tail`() = runTest {
        val (api, _, repo) = harness()
        val session = FakeAuthenticatedSession("s1")

        // page1(null)->c1, page2(c1)->c2, page3(c2)->c3, page4(c3)->c4, page5(c4)->c5
        api.enqueueList(null, ApiResult.Success(testPage("pg1", 2, nextCursor = "c1")))
        api.enqueueList("c1", ApiResult.Success(testPage("pg2", 2, nextCursor = "c2")))
        api.enqueueList("c2", ApiResult.Success(testPage("pg3", 2, nextCursor = "c3")))
        api.enqueueList("c3", ApiResult.Success(testPage("pg4", 2, nextCursor = "c4")))
        api.enqueueList("c4", ApiResult.Success(testPage("pg5", 2, nextCursor = "c5")))

        repo.loadFirstPage(session)
        repo.loadNextPage(session) // page2
        repo.loadNextPage(session) // page3
        repo.loadNextPage(session) // page4
        repo.loadNextPage(session) // page5 -> overflow -> evict head(page1)

        var content = repo.state.value as NotesListState.Content
        assertEquals(
            listOf("pg2-0", "pg2-1", "pg3-0", "pg3-1", "pg4-0", "pg4-1", "pg5-0", "pg5-1"),
            content.rows.map { it.id },
        )
        assertTrue("page 1's requestCursor (null) must be the backward key", content.canLoadPrevious)
        assertTrue(content.canLoadNext)
        assertEquals("pg2-0", content.anchorNoteId)

        // Reload page 1 (backward): request cursor must be null (page1's requestCursor).
        api.enqueueList(null, ApiResult.Success(testPage("pg1", 2, nextCursor = "c1")))
        val callsBefore = api.listCalls.size
        repo.loadPreviousPage(session)
        assertEquals(null, api.listCalls[callsBefore].cursor)

        content = repo.state.value as NotesListState.Content
        assertEquals(
            "prepending page1 must overflow and evict the tail (page5), not lose page1's own content",
            listOf("pg1-0", "pg1-1", "pg2-0", "pg2-1", "pg3-0", "pg3-1", "pg4-0", "pg4-1"),
            content.rows.map { it.id },
        )
        assertEquals("pg1-0", content.anchorNoteId)
        assertTrue("evicting the tail (page5) must leave a forward key", content.canLoadNext)

        // Forward-reload the evicted page5: must use page5's own requestCursor
        // (c4), not its nextCursor (c5), and must restore its exact rows.
        api.enqueueList("c4", ApiResult.Success(testPage("pg5", 2, nextCursor = "c5")))
        val callsBeforeForward = api.listCalls.size
        repo.loadNextPage(session)
        assertEquals("c4", api.listCalls[callsBeforeForward].cursor)

        content = repo.state.value as NotesListState.Content
        assertEquals(
            "the window must return to exactly pages 2-5, in original order",
            listOf("pg2-0", "pg2-1", "pg3-0", "pg3-1", "pg4-0", "pg4-1", "pg5-0", "pg5-1"),
            content.rows.map { it.id },
        )
        assertEquals("pg2-0", content.anchorNoteId)
    }

    @Test
    fun `alternating backward and forward reload stays bounded across 20 requests`() = runTest {
        val (api, _, repo) = harness()
        val session = FakeAuthenticatedSession("s1")
        api.enqueueList(null, ApiResult.Success(testPage("pg1", 1, nextCursor = "c1")))
        api.enqueueList("c1", ApiResult.Success(testPage("pg2", 1, nextCursor = "c2")))
        api.enqueueList("c2", ApiResult.Success(testPage("pg3", 1, nextCursor = "c3")))
        api.enqueueList("c3", ApiResult.Success(testPage("pg4", 1, nextCursor = "c4")))
        api.enqueueList("c4", ApiResult.Success(testPage("pg5", 1, nextCursor = "c5")))
        repo.loadFirstPage(session)
        repeat(4) { repo.loadNextPage(session) } // pages 2-5 in window, backward key = null

        repeat(10) {
            api.enqueueList(null, ApiResult.Success(testPage("pg1", 1, nextCursor = "c1")))
            repo.loadPreviousPage(session)
            var content = repo.state.value as NotesListState.Content
            assertEquals(4, content.rows.size)
            assertEquals(listOf("pg1-0", "pg2-0", "pg3-0", "pg4-0"), content.rows.map { it.id })
            assertEquals("pg1-0", content.anchorNoteId)

            api.enqueueList("c4", ApiResult.Success(testPage("pg5", 1, nextCursor = "c5")))
            repo.loadNextPage(session)
            content = repo.state.value as NotesListState.Content
            assertEquals(4, content.rows.size)
            assertEquals(listOf("pg2-0", "pg3-0", "pg4-0", "pg5-0"), content.rows.map { it.id })
            assertEquals("pg2-0", content.anchorNoteId)
        }
    }
}
