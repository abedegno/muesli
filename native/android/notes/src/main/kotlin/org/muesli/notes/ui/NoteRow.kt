package org.muesli.notes.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import org.muesli.notes.model.NoteListItem

/**
 * One read-only note row (issue #768 Task 5). No create/edit/delete/audio/
 * share affordance is ever rendered here or reachable from it. The row's
 * merged semantics carry pinned/tag state so a screen reader announces it
 * without relying on any visual-only cue, and the touch target is at least
 * 48dp tall.
 */
@Composable
fun NoteRow(item: NoteListItem, onClick: (String) -> Unit, modifier: Modifier = Modifier) {
    val title = item.title.ifBlank { "Untitled note" }
    val label = buildString {
        append(title)
        if (item.pinned) append(", pinned")
        if (item.tags.isNotEmpty()) append(", tags: ").append(item.tags.joinToString())
    }
    Column(
        modifier = modifier
            .fillMaxWidth()
            .defaultMinSize(minHeight = 48.dp)
            .clickable { onClick(item.id) }
            .padding(horizontal = 16.dp, vertical = 12.dp)
            .semantics(mergeDescendants = true) {
                contentDescription = label
                role = Role.Button
            },
    ) {
        Text(text = title, style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
        if (item.snippet.isNotBlank()) {
            Text(
                text = item.snippet,
                style = MaterialTheme.typography.bodyMedium,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}
