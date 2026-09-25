package org.muesli.notes.ui

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.lifecycle.viewmodel.initializer
import androidx.lifecycle.viewmodel.viewModelFactory
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import org.muesli.notes.data.NotesRepository
import org.muesli.notes.detail.NoteDetailViewModel
import org.muesli.notes.list.NotesListViewModel
import org.muesli.notes.session.SessionSource

internal const val NOTES_LIST_ROUTE = "notes"
internal const val NOTES_DETAIL_ROUTE = "notes/{noteId}"
internal const val NOTES_DETAIL_ARG_NOTE_ID = "noteId"

/**
 * The notes feature's navigation graph (issue #768 Task 5): a collection
 * destination and a read-only detail destination reached by [stable note id
 * only][NOTES_DETAIL_ARG_NOTE_ID] -- never a possibly truncated list-row
 * payload. Note ids are contract-defined UUIDs with no path-breaking
 * characters, so no additional encoding is applied here.
 */
@Composable
fun NotesNavGraph(
    navController: NavHostController,
    sessionSource: SessionSource,
    repository: NotesRepository,
) {
    NavHost(navController = navController, startDestination = NOTES_LIST_ROUTE) {
        composable(NOTES_LIST_ROUTE) {
            val viewModel: NotesListViewModel = viewModel(
                factory = viewModelFactory { initializer { NotesListViewModel(sessionSource, repository) } },
            )
            val state by viewModel.state.collectAsStateWithLifecycle()
            NotesListScreen(
                state = state,
                onSelectNote = { noteId -> navController.navigate("notes/$noteId") },
                onLoadNext = viewModel::loadNextPage,
                onLoadPrevious = viewModel::loadPreviousPage,
                onRefresh = viewModel::refresh,
                onRetryInitialLoad = viewModel::retryInitialLoad,
            )
        }
        composable(
            route = NOTES_DETAIL_ROUTE,
            arguments = listOf(navArgument(NOTES_DETAIL_ARG_NOTE_ID) { type = NavType.StringType }),
        ) { backStackEntry ->
            val noteId = backStackEntry.arguments?.getString(NOTES_DETAIL_ARG_NOTE_ID).orEmpty()
            val viewModel: NoteDetailViewModel = viewModel(
                key = noteId,
                factory = viewModelFactory { initializer { NoteDetailViewModel(noteId, sessionSource, repository) } },
            )
            val state by viewModel.state.collectAsStateWithLifecycle()
            NoteDetailScreen(
                state = state,
                onBack = { navController.popBackStack() },
                onRetry = viewModel::retry,
            )
        }
    }
}
