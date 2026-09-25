package org.muesli.notes.ui

import androidx.compose.runtime.Composable
import androidx.navigation.compose.rememberNavController
import org.muesli.notes.data.NotesRepository
import org.muesli.notes.session.SessionSource

/**
 * The self-contained entry point for the "View Notes on Android" feature
 * (issue #768). A host places `NotesFeature(sessionSource, repository)`
 * somewhere in its own authenticated navigation graph (Task 6, out of scope
 * for this PR); this composable owns its own internal navigation and
 * view-model wiring and exposes no other integration surface.
 */
@Composable
fun NotesFeature(
    sessionSource: SessionSource,
    repository: NotesRepository,
) {
    val navController = rememberNavController()
    NotesNavGraph(navController = navController, sessionSource = sessionSource, repository = repository)
}
