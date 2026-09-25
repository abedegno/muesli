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
     * Ends the session identified by the full [expectedSessionId] +
     * [expectedGeneration] identity. A no-op if the current session no
     * longer matches that exact identity -- checked atomically against the
     * live session, not id alone -- so a late-arriving 401 from a
     * superseded session can never clobber a session the user has since
     * re-established, including a same-id replacement (e.g. a token
     * refresh) that bumped only the generation.
     */
    fun invalidate(expectedSessionId: SessionId, expectedGeneration: Long)
}
