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
     * [expectedGeneration] identity, as a true atomic compare-and-set
     * against the live session -- there is no window between checking the
     * identity and clearing it in which a concurrent [session] replacement
     * could sneak in unobserved. Returns `true` only if that exact
     * identity was still live at the moment of the compare-and-set and was
     * therefore actually ended; returns `false` (a genuine no-op, nothing
     * mutated) if the current session no longer matches -- e.g. it was
     * already replaced by a same-id generation bump (a token refresh) or
     * cleared -- so a late-arriving 401 from a superseded session can
     * never clobber a session the user has since re-established. Callers
     * must gate any of their own state clearing on this return value
     * rather than re-deriving currency themselves, to avoid re-opening the
     * same non-atomic check-then-act window this method closes.
     */
    fun invalidate(expectedSessionId: SessionId, expectedGeneration: Long): Boolean
}
