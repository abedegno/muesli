package org.muesli.notes.ui

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import org.muesli.notes.detail.NoteDetailState

/**
 * The read-only note detail screen (issue #768 Task 5). Renders only
 * contract-supported fields (optional fields are simply omitted, never
 * fabricated). [NoteDetailState.Unavailable] offers only "back"; any other
 * failure offers "retry". Body and summary markdown render as plain,
 * selectable, scaling Compose text -- no HTML interpretation, no remote
 * image loading.
 */
@Composable
fun NoteDetailScreen(
    state: NoteDetailState,
    onBack: () -> Unit,
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier.fillMaxSize()) {
        when (state) {
            NoteDetailState.Loading ->
                StatusMessage(message = "Loading note…", modifier = Modifier.fillMaxSize())

            NoteDetailState.Unavailable ->
                StatusMessage(
                    message = "This note is no longer available.",
                    modifier = Modifier.fillMaxSize(),
                    actionLabel = "Back",
                    onAction = onBack,
                )

            is NoteDetailState.Failure ->
                StatusMessage(
                    message = "Couldn't load this note.",
                    modifier = Modifier.fillMaxSize(),
                    actionLabel = "Retry",
                    onAction = onRetry,
                )

            is NoteDetailState.Content -> NoteDetailContent(state)
        }
    }
}

@Composable
private fun NoteDetailContent(state: NoteDetailState.Content) {
    val detail = state.detail
    SelectionContainer {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(16.dp),
        ) {
            Text(text = detail.title.ifBlank { "Untitled note" }, style = MaterialTheme.typography.headlineSmall)
            if (detail.tags.isNotEmpty()) {
                Spacer(Modifier.height(4.dp))
                Text(text = detail.tags.joinToString(), style = MaterialTheme.typography.labelMedium)
            }
            if (detail.bodyMarkdown.isNotBlank()) {
                Spacer(Modifier.height(12.dp))
                Text(text = detail.bodyMarkdown, style = MaterialTheme.typography.bodyLarge)
            }
            detail.summaries.forEach { summary ->
                Spacer(Modifier.height(20.dp))
                Text(text = summary.templateName, style = MaterialTheme.typography.titleMedium)
                summary.sections.forEach { section ->
                    Spacer(Modifier.height(8.dp))
                    Text(text = section.heading, style = MaterialTheme.typography.titleSmall)
                    Text(text = section.contentMarkdown, style = MaterialTheme.typography.bodyMedium)
                }
            }
        }
    }
}
