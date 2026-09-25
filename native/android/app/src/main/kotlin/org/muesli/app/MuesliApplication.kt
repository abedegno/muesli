package org.muesli.app

import android.app.Application
import okhttp3.OkHttpClient
import org.muesli.app.auth.LoginApi
import org.muesli.app.session.AppSessionSource
import org.muesli.notes.api.OkHttpMobileNotesApi
import org.muesli.notes.data.NotesRepository
import org.muesli.notes.data.SessionNotesRepository

/**
 * The host application's composition root (issue #768 Task 6): owns the one
 * [OkHttpClient], this app's [org.muesli.notes.session.SessionSource]
 * implementation ([AppSessionSource]), the login call ([LoginApi]), and the
 * single production [NotesRepository] ([SessionNotesRepository], backed by
 * the `:notes` module's [OkHttpMobileNotesApi]) shared by every screen --
 * so the login flow and the notes feature observe and mutate the same
 * session identity.
 */
class MuesliApplication : Application() {
    val okHttpClient: OkHttpClient by lazy { OkHttpClient() }
    val sessionSource: AppSessionSource by lazy { AppSessionSource() }
    val loginApi: LoginApi by lazy { LoginApi(okHttpClient) }
    val notesRepository: NotesRepository by lazy {
        SessionNotesRepository(api = OkHttpMobileNotesApi(okHttpClient), sessionSource = sessionSource)
    }
}
