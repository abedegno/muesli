package org.muesli.notes.session

import kotlinx.coroutines.flow.StateFlow
import org.muesli.notes.api.AuthenticatedSession
import org.muesli.notes.api.SessionId

/**
 * The host application's current authenticated session, if any (issue #768).
 * The notes feature never signs in, stores credentials, or invents a second
 * credential flow: it only observes [session] and, on a 401, asks the host
 * to end that session via [invalidate] so the host's own authentication
 * recovery path takes over.
 */
interface SessionSource {
    /** Null when signed out. Emits a new value on sign-in, sign-out, or account switch. */
    val session: StateFlow<AuthenticatedSession?>

    /**
     * Ends the session identified by [expectedSessionId]. A no-op if the
     * current session no longer matches (it was already replaced or
     * cleared), so a late-arriving 401 from a superseded session can never
     * clobber a session the user has since re-established.
     */
    fun invalidate(expectedSessionId: SessionId)
}
