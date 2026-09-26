package org.muesli.notes.detail

import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.cancel
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.muesli.notes.api.ApiFailure
import org.muesli.notes.api.ApiResult
import org.muesli.notes.model.NoteDetail
import org.muesli.notes.testing.FakeAuthenticatedSession
import org.muesli.notes.testing.FakeNotesRepository
import org.muesli.notes.testing.FakeSessionSource
import java.time.Instant

/**
 * Detail view-model tests (issue #768 Task 4): loading/content/unavailable/
 * failure states, id-only input, complete and optional detail, 404/503
 * mapping, retry, malformed-payload handling, session transition clearing,
 * late-result rejection, and cancellation-on-disposal.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class NoteDetailViewModelTest {
    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() {
        Dispatchers.setMain(dispatcher)
    }

    @After
    fun tearDown() {
        Dispatchers.resetMain()
    }

    private fun completeDetail(id: String = "n1") = NoteDetail(
        id = id,
        title = "Sprint planning",
        status = "ready",
        pinned = true,
        startedAt = Instant.parse("2026-01-10T09:00:00Z"),
        endedAt = Instant.parse("2026-01-10T09:45:00Z"),
        createdAt = Instant.parse("2026-01-10T09:00:05Z"),
        updatedAt = Instant.parse("2026-01-10T09:45:10Z"),
        tags = listOf("planning", "roadmap"),
        bodyMarkdown = "# Sprint planning",
        summaries = emptyList(),
    )

    private fun optionalAbsentDetail(id: String = "n1") = NoteDetail(
        id = id,
        title = "Oldest note",
        status = "processing",
        pinned = false,
        startedAt = null,
        endedAt = null,
        createdAt = Instant.parse("2026-01-01T08:00:00Z"),
        updatedAt = Instant.parse("2026-01-01T08:00:00Z"),
        tags = emptyList(),
        bodyMarkdown = "",
        summaries = emptyList(),
    )

    @Test
    fun `initial state is Loading, keyed only by id, then loads on session available`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val repository = FakeNotesRepository()
        repository.enqueueDetail("n1", ApiResult.Success(completeDetail()))
        val viewModel = NoteDetailViewModel("n1", sessionSource, repository)

        assertEquals(NoteDetailState.Loading, viewModel.state.value)
        dispatcher.scheduler.advanceUntilIdle()

        val content = viewModel.state.value as NoteDetailState.Content
        assertEquals("n1", content.detail.id)
        assertEquals(listOf(session to "n1"), repository.noteDetailCalls)
    }

    @Test
    fun `complete detail is published verbatim with every optional field`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val repository = FakeNotesRepository()
        repository.enqueueDetail("n1", ApiResult.Success(completeDetail()))
        val viewModel = NoteDetailViewModel("n1", sessionSource, repository)
        dispatcher.scheduler.advanceUntilIdle()

        val content = viewModel.state.value as NoteDetailState.Content
        assertEquals(listOf("planning", "roadmap"), content.detail.tags)
        assertEquals(Instant.parse("2026-01-10T09:00:00Z"), content.detail.startedAt)
    }

    @Test
    fun `detail with optional fields absent is published with nulls and empty collections`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val repository = FakeNotesRepository()
        repository.enqueueDetail("n1", ApiResult.Success(optionalAbsentDetail()))
        val viewModel = NoteDetailViewModel("n1", sessionSource, repository)
        dispatcher.scheduler.advanceUntilIdle()

        val content = viewModel.state.value as NoteDetailState.Content
        assertEquals(null, content.detail.startedAt)
        assertTrue(content.detail.tags.isEmpty())
    }

    @Test
    fun `404 (Unavailable) is a distinct terminal state, not a retryable failure`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val repository = FakeNotesRepository()
        repository.enqueueDetail("n1", ApiResult.Failure(ApiFailure.Unavailable))
        val viewModel = NoteDetailViewModel("n1", sessionSource, repository)
        dispatcher.scheduler.advanceUntilIdle()

        assertEquals(NoteDetailState.Unavailable, viewModel.state.value)
    }

    @Test
    fun `503 is a retryable failure and retry re-fetches`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val repository = FakeNotesRepository()
        repository.enqueueDetail("n1", ApiResult.Failure(ApiFailure.Http(503)))
        val viewModel = NoteDetailViewModel("n1", sessionSource, repository)
        dispatcher.scheduler.advanceUntilIdle()
        assertEquals(NoteDetailState.Failure(ApiFailure.Http(503)), viewModel.state.value)

        repository.enqueueDetail("n1", ApiResult.Success(completeDetail()))
        viewModel.retry()
        dispatcher.scheduler.advanceUntilIdle()
        assertTrue(viewModel.state.value is NoteDetailState.Content)
        assertEquals(2, repository.noteDetailCalls.size)
    }

    @Test
    fun `malformed payload is a retryable failure`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val repository = FakeNotesRepository()
        repository.enqueueDetail("n1", ApiResult.Failure(ApiFailure.MalformedPayload))
        val viewModel = NoteDetailViewModel("n1", sessionSource, repository)
        dispatcher.scheduler.advanceUntilIdle()
        assertEquals(NoteDetailState.Failure(ApiFailure.MalformedPayload), viewModel.state.value)
    }

    @Test
    fun `session ending mid-request clears back to Loading and rejects the late result`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val repository = FakeNotesRepository()
        val gate = repository.gateDetail("n1")
        repository.enqueueDetail("n1", ApiResult.Success(completeDetail()))
        val viewModel = NoteDetailViewModel("n1", sessionSource, repository)
        dispatcher.scheduler.runCurrent() // reach the gated await

        sessionSource.set(null)
        dispatcher.scheduler.advanceUntilIdle()
        assertEquals(NoteDetailState.Loading, viewModel.state.value)

        gate.complete(Unit)
        dispatcher.scheduler.advanceUntilIdle()
        assertEquals(
            "a result for an ended session must never resurrect state",
            NoteDetailState.Loading,
            viewModel.state.value,
        )
    }

    @Test
    fun `switching accounts discards a stale in-flight result from the former account`() = runTest {
        val sessionA = FakeAuthenticatedSession("a")
        val sessionB = FakeAuthenticatedSession("b")
        val sessionSource = FakeSessionSource(sessionA)
        val repository = FakeNotesRepository()
        val gate = repository.gateDetail("n1")
        repository.enqueueDetail("n1", ApiResult.Success(completeDetail(id = "n1")))
        val viewModel = NoteDetailViewModel("n1", sessionSource, repository)
        dispatcher.scheduler.runCurrent()

        sessionSource.set(sessionB)
        repository.enqueueDetail("n1", ApiResult.Success(completeDetail(id = "n1")))
        dispatcher.scheduler.advanceUntilIdle()
        val afterSwitch = viewModel.state.value as NoteDetailState.Content

        gate.complete(Unit)
        dispatcher.scheduler.advanceUntilIdle()
        assertEquals("account switch's own fetch result must win, not the stale one", afterSwitch, viewModel.state.value)
        assertEquals(listOf(sessionA to "n1", sessionB to "n1"), repository.noteDetailCalls)
    }

    @Test
    fun `a same-id but newer-generation session (such as a token refresh) discards a stale in-flight result and reloads`() = runTest {
        val sessionGen1 = FakeAuthenticatedSession("s1", generation = 1)
        val sessionGen2 = FakeAuthenticatedSession("s1", generation = 2)
        val sessionSource = FakeSessionSource(sessionGen1)
        val repository = FakeNotesRepository()
        val gate = repository.gateDetail("n1")
        repository.enqueueDetail("n1", ApiResult.Success(completeDetail(id = "n1")))
        val viewModel = NoteDetailViewModel("n1", sessionSource, repository)
        dispatcher.scheduler.runCurrent()

        sessionSource.set(sessionGen2)
        repository.enqueueDetail("n1", ApiResult.Success(optionalAbsentDetail(id = "n1")))
        dispatcher.scheduler.advanceUntilIdle()
        val afterGenerationBump = viewModel.state.value as NoteDetailState.Content
        assertTrue("generation 2's own fetch must publish", afterGenerationBump.detail.tags.isEmpty())

        gate.complete(Unit)
        dispatcher.scheduler.advanceUntilIdle()
        assertEquals(
            "generation 1's stale result must never overwrite generation 2's, even with the same session id",
            afterGenerationBump,
            viewModel.state.value,
        )
        assertEquals(listOf(sessionGen1 to "n1", sessionGen2 to "n1"), repository.noteDetailCalls)
    }

    @Test
    fun `cancelling the view model scope (as onCleared does) discards a late in-flight result`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val repository = FakeNotesRepository()
        val gate = repository.gateDetail("n1")
        repository.enqueueDetail("n1", ApiResult.Success(completeDetail()))
        val viewModel = NoteDetailViewModel("n1", sessionSource, repository)
        dispatcher.scheduler.runCurrent() // in flight, awaiting the gate

        // onCleared() itself is not visible to call directly from a test,
        // but it works by cancelling viewModelScope -- cancelling it here
        // has the identical effect on any in-flight launch this class made.
        viewModel.viewModelScope.cancel()
        gate.complete(Unit)
        dispatcher.scheduler.advanceUntilIdle()

        assertEquals(
            "a result completing after the screen's view model scope is cancelled must never update state",
            NoteDetailState.Loading,
            viewModel.state.value,
        )
    }

    @Test
    fun `a superseded retry's result is discarded, only the latest retry publishes`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val repository = FakeNotesRepository()
        repository.enqueueDetail("n1", ApiResult.Failure(ApiFailure.Http(503)))
        val viewModel = NoteDetailViewModel("n1", sessionSource, repository)
        dispatcher.scheduler.advanceUntilIdle()

        val gate1 = repository.gateDetail("n1")
        repository.enqueueDetail("n1", ApiResult.Failure(ApiFailure.Http(500))) // first retry: will be superseded
        viewModel.retry()
        dispatcher.scheduler.runCurrent()

        // A second retry starts before the first's response arrives.
        repository.enqueueDetail("n1", ApiResult.Success(completeDetail()))
        viewModel.retry()
        dispatcher.scheduler.advanceUntilIdle()

        gate1.complete(Unit)
        dispatcher.scheduler.advanceUntilIdle()
        assertTrue("the superseded first retry's failure must not overwrite the second retry's success", viewModel.state.value is NoteDetailState.Content)
    }

    @Test
    fun `session ending mid-request cancels the in-flight fetch, not just discards its result`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val repository = FakeNotesRepository()
        val gate = repository.gateDetail("n1")
        repository.enqueueDetail("n1", ApiResult.Success(completeDetail()))
        NoteDetailViewModel("n1", sessionSource, repository)
        dispatcher.scheduler.runCurrent() // reach the gated await

        sessionSource.set(null)
        dispatcher.scheduler.advanceUntilIdle()

        assertEquals(
            "sign-out must cancel the coroutine actually running the in-flight detail fetch, " +
                "not merely let it complete and discard the late result",
            1,
            repository.noteDetailCancelledCount,
        )
        gate.complete(Unit)
    }

    @Test
    fun `switching accounts cancels the in-flight fetch for the former account`() = runTest {
        val sessionA = FakeAuthenticatedSession("a")
        val sessionB = FakeAuthenticatedSession("b")
        val sessionSource = FakeSessionSource(sessionA)
        val repository = FakeNotesRepository()
        val gate = repository.gateDetail("n1")
        repository.enqueueDetail("n1", ApiResult.Success(completeDetail(id = "n1")))
        NoteDetailViewModel("n1", sessionSource, repository)
        dispatcher.scheduler.runCurrent() // reach the gated await for account A

        repository.enqueueDetail("n1", ApiResult.Success(completeDetail(id = "n1")))
        sessionSource.set(sessionB)
        dispatcher.scheduler.advanceUntilIdle()

        assertEquals(
            "an account switch must cancel the former account's in-flight detail fetch",
            1,
            repository.noteDetailCancelledCount,
        )
        gate.complete(Unit)
    }

    @Test
    fun `a generation change (retry) cancels the previous in-flight fetch, not just discards its result`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val repository = FakeNotesRepository()
        val gate = repository.gateDetail("n1")
        repository.enqueueDetail("n1", ApiResult.Success(completeDetail()))
        val viewModel = NoteDetailViewModel("n1", sessionSource, repository)
        dispatcher.scheduler.runCurrent() // reach the gated await

        repository.enqueueDetail("n1", ApiResult.Success(completeDetail()))
        viewModel.retry()
        dispatcher.scheduler.advanceUntilIdle()

        assertEquals(
            "retry (a generation change) must cancel the coroutine actually running the superseded fetch",
            1,
            repository.noteDetailCancelledCount,
        )
        gate.complete(Unit)
    }
}
