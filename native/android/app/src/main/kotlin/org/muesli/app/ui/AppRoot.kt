package org.muesli.app.ui

import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import org.muesli.notes.data.NotesRepository
import org.muesli.notes.session.SessionSource
import org.muesli.notes.ui.NotesFeature

/**
 * The host application's authenticated navigation root (issue #768 Task 6).
 * Signed-out ([sessionSource].session == null) shows [LoginScreen]; only
 * once a session exists does this navigate to
 * [org.muesli.notes.ui.NotesFeature] -- the `:notes` module's own runnable
 * entry point for browsing and reading notes.
 */
@Composable
fun AppRoot(
    sessionSource: SessionSource,
    repository: NotesRepository,
    loginViewModel: LoginViewModel,
) {
    MaterialTheme {
        val session by sessionSource.session.collectAsState()
        if (session == null) {
            LoginScreen(viewModel = loginViewModel)
        } else {
            NotesFeature(sessionSource = sessionSource, repository = repository)
        }
    }
}
