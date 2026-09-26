package org.muesli.notes.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import org.muesli.notes.list.NotesListState
import org.muesli.notes.list.PaginationStatus
import org.muesli.notes.list.PreviousPageStatus
import org.muesli.notes.list.RefreshStatus

/**
 * The read-only notes collection screen (issue #768 Task 5). Renders every
 * [NotesListState]: initial loading, initial failure (retryable, never
 * claims "empty"), empty, and content with independent pagination/previous-
 * page/refresh status. Refresh and both edge controls are disabled whenever
 * any other collection operation is in flight, mirroring the repository's
 * shared operation gate -- never duplicating or restarting a request. No
 * create/edit/delete/record/upload/search control is rendered anywhere on
 * this screen.
 */
@Composable
fun NotesListScreen(
    state: NotesListState,
    onSelectNote: (String) -> Unit,
    onLoadNext: () -> Unit,
    onLoadPrevious: () -> Unit,
    onRefresh: () -> Unit,
    onRetryInitialLoad: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier.fillMaxSize()) {
        when (state) {
            NotesListState.InitialLoading ->
                StatusMessage(message = "Loading notes…", modifier = Modifier.fillMaxSize())

            is NotesListState.InitialFailure ->
                StatusMessage(
                    message = "Couldn't load your notes.",
                    modifier = Modifier.fillMaxSize(),
                    actionLabel = "Retry",
                    onAction = onRetryInitialLoad,
                )

            NotesListState.Empty ->
                StatusMessage(message = "No notes yet.", modifier = Modifier.fillMaxSize())

            is NotesListState.Content -> NotesListContent(
                state = state,
                onSelectNote = onSelectNote,
                onLoadNext = onLoadNext,
                onLoadPrevious = onLoadPrevious,
                onRefresh = onRefresh,
            )
        }
    }
}

@Composable
private fun NotesListContent(
    state: NotesListState.Content,
    onSelectNote: (String) -> Unit,
    onLoadNext: () -> Unit,
    onLoadPrevious: () -> Unit,
    onRefresh: () -> Unit,
) {
    val busy = state.refreshStatus == RefreshStatus.REFRESHING ||
        state.paginationStatus == PaginationStatus.LOADING_NEXT_PAGE ||
        state.previousPageStatus == PreviousPageStatus.LOADING_PREVIOUS_PAGE

    Row(modifier = Modifier.fillMaxWidth().padding(8.dp), horizontalArrangement = Arrangement.End) {
        TextButton(
            onClick = onRefresh,
            enabled = !busy,
            modifier = Modifier.defaultMinSize(minHeight = 48.dp),
        ) {
            Text(if (state.refreshStatus == RefreshStatus.REFRESHING) "Refreshing…" else "Refresh")
        }
    }
    if (state.refreshStatus == RefreshStatus.REFRESH_FAILED) {
        StatusMessage(message = "Refresh failed.", actionLabel = "Retry", onAction = onRefresh)
    }

    LazyColumn(modifier = Modifier.fillMaxSize()) {
        if (state.canLoadPrevious) {
            item(key = "load-previous") {
                LoadEdgeControl(
                    label = "Load earlier notes",
                    loading = state.previousPageStatus == PreviousPageStatus.LOADING_PREVIOUS_PAGE,
                    failed = state.previousPageStatus == PreviousPageStatus.PREVIOUS_PAGE_FAILED,
                    failureMessage = "Couldn't load earlier notes.",
                    enabled = !busy,
                    onClick = onLoadPrevious,
                )
            }
        }
        items(items = state.rows, key = { it.id }) { row ->
            NoteRow(item = row, onClick = onSelectNote)
        }
        if (state.canLoadNext) {
            item(key = "load-next") {
                LoadEdgeControl(
                    label = "Load more notes",
                    loading = state.paginationStatus == PaginationStatus.LOADING_NEXT_PAGE,
                    failed = state.paginationStatus == PaginationStatus.NEXT_PAGE_FAILED,
                    failureMessage = "Couldn't load more notes.",
                    enabled = !busy,
                    onClick = onLoadNext,
                )
            }
        }
    }
}

@Composable
private fun LoadEdgeControl(
    label: String,
    loading: Boolean,
    failed: Boolean,
    failureMessage: String,
    enabled: Boolean,
    onClick: () -> Unit,
) {
    Column(modifier = Modifier.fillMaxWidth().padding(16.dp)) {
        if (failed) {
            Text(text = failureMessage, style = MaterialTheme.typography.bodyMedium)
        }
        TextButton(onClick = onClick, enabled = enabled, modifier = Modifier.defaultMinSize(minHeight = 48.dp)) {
            Text(
                when {
                    loading -> "Loading…"
                    failed -> "Retry"
                    else -> label
                },
            )
        }
    }
}
