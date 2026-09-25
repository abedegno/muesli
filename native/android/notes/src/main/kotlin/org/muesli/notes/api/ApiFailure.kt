package org.muesli.notes.api

/**
 * Every way a #767 mobile contract call can fail, mapped once at the client
 * boundary (per the plan): 401 -> [SessionEnded], detail 404 -> [Unavailable],
 * any other non-2xx -> [Http], an undecodable/invariant-violating 2xx body ->
 * [MalformedPayload], and a transport/I-O failure -> [Network]. Coroutine
 * cancellation is never represented here -- it propagates uncaught.
 */
sealed interface ApiFailure {
    /** The session's credentials were rejected (401): caller must clear session state. */
    data object SessionEnded : ApiFailure

    /** The requested note is absent, deleted, or not owned by this session (404 on detail only). */
    data object Unavailable : ApiFailure

    /** Any other non-2xx HTTP status. Never carries the response body (may contain server internals). */
    data class Http(val status: Int) : ApiFailure

    /** A 2xx body that failed to decode or violated a contract invariant (e.g. a blank id). */
    data object MalformedPayload : ApiFailure

    /** A transport-level failure (DNS, connect, timeout, reset). [message] is diagnostic only. */
    data class Network(val message: String?) : ApiFailure
}

/** The result of one #767 contract call: exactly a value or exactly one [ApiFailure]. */
sealed interface ApiResult<out T> {
    data class Success<out T>(val value: T) : ApiResult<T>
    data class Failure(val reason: ApiFailure) : ApiResult<Nothing>
}
