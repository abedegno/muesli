package org.muesli.app

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.viewModels
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewmodel.CreationExtras
import org.muesli.app.ui.AppRoot
import org.muesli.app.ui.LoginViewModel

/**
 * The app's single, real runnable entry point (issue #768 Task 6). Renders
 * [AppRoot], which shows the login screen while signed out and, once
 * [MuesliApplication.sessionSource] holds an authenticated session,
 * navigates to `NotesFeature` from the `:notes` module.
 */
class MainActivity : ComponentActivity() {
    private val app: MuesliApplication get() = application as MuesliApplication

    private val loginViewModel: LoginViewModel by viewModels {
        object : ViewModelProvider.Factory {
            @Suppress("UNCHECKED_CAST")
            override fun <T : ViewModel> create(modelClass: Class<T>, extras: CreationExtras): T =
                LoginViewModel(app.loginApi, app.sessionSource) as T
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            AppRoot(
                sessionSource = app.sessionSource,
                repository = app.notesRepository,
                loginViewModel = loginViewModel,
            )
        }
    }
}
