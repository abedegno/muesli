package org.muesli.app.ui

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import org.muesli.app.auth.LoginApi
import org.muesli.app.auth.LoginResult
import org.muesli.app.session.AppSessionSource

/** UI state for [LoginScreen]. */
data class LoginUiState(
    val serverUrl: String = "",
    val email: String = "",
    val password: String = "",
    val isLoading: Boolean = false,
    val errorMessage: String? = null,
)

/**
 * Drives the signed-out login screen (issue #768 Task 6): collects server
 * URL + email + password, calls [LoginApi.login], and on success installs
 * the resulting bearer token into [AppSessionSource] -- which is what flips
 * [org.muesli.notes.session.SessionSource.session] non-null and causes
 * `AppRoot` to navigate into `NotesFeature`.
 */
class LoginViewModel(
    private val loginApi: LoginApi,
    private val sessionSource: AppSessionSource,
) : ViewModel() {
    private val _uiState = MutableStateFlow(LoginUiState())
    val uiState: StateFlow<LoginUiState> = _uiState.asStateFlow()

    fun onServerUrlChange(value: String) {
        _uiState.value = _uiState.value.copy(serverUrl = value, errorMessage = null)
    }

    fun onEmailChange(value: String) {
        _uiState.value = _uiState.value.copy(email = value, errorMessage = null)
    }

    fun onPasswordChange(value: String) {
        _uiState.value = _uiState.value.copy(password = value, errorMessage = null)
    }

    fun signIn() {
        val state = _uiState.value
        if (state.isLoading) return
        _uiState.value = state.copy(isLoading = true, errorMessage = null)
        viewModelScope.launch {
            when (val result = loginApi.login(state.serverUrl, state.email, state.password)) {
                is LoginResult.Success -> {
                    sessionSource.signIn(result.token, state.serverUrl.trim())
                    _uiState.value = LoginUiState(serverUrl = state.serverUrl)
                }
                is LoginResult.Failure -> {
                    _uiState.value = _uiState.value.copy(isLoading = false, errorMessage = result.message)
                }
            }
        }
    }
}
