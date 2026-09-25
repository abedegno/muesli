package org.muesli.notes.api

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json
import okhttp3.Call
import okhttp3.Callback
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import org.muesli.notes.model.NoteDetail
import org.muesli.notes.model.NotesPage
import java.io.IOException
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

/**
 * The only production implementation of [MobileNotesApi]: consumes the #767
 * mobile contract over OkHttp. Persists nothing across calls, never retries
 * internally (the repository owns retry-via-user-action), and lets
 * [CancellationException] propagate uncaught so a cancelled screen's request
 * is actually torn down rather than completing invisibly.
 */
class OkHttpMobileNotesApi(
    private val client: OkHttpClient,
    private val json: Json = Json { ignoreUnknownKeys = true },
) : MobileNotesApi {

    override suspend fun listNotes(session: AuthenticatedSession, limit: Int, cursor: String?): ApiResult<NotesPage> {
        require(limit in 1..100) { "limit must be within 1..100, was $limit" }
        val url = session.baseUrl.toHttpUrl().newBuilder()
            .addPathSegments("api/mobile/v1/notes")
            .addQueryParameter("limit", limit.toString())
            .apply { cursor?.let { addQueryParameter("cursor", it) } }
            .build()
        return runRequest(buildRequest(session, url)) { response ->
            when (response.code) {
                200 -> decodeOrMalformed<MobileNotesListResponseDto, NotesPage>(response) { it.toDomain() }
                401 -> ApiResult.Failure(ApiFailure.SessionEnded)
                else -> ApiResult.Failure(ApiFailure.Http(response.code))
            }
        }
    }

    override suspend fun getNote(session: AuthenticatedSession, noteId: String): ApiResult<NoteDetail> {
        val url = session.baseUrl.toHttpUrl().newBuilder()
            .addPathSegments("api/mobile/v1/notes")
            .addPathSegment(noteId)
            .build()
        return runRequest(buildRequest(session, url)) { response ->
            when (response.code) {
                200 -> decodeOrMalformed<MobileNoteDetailResponseDto, NoteDetail>(response) { it.toDomain() }
                401 -> ApiResult.Failure(ApiFailure.SessionEnded)
                404 -> ApiResult.Failure(ApiFailure.Unavailable)
                else -> ApiResult.Failure(ApiFailure.Http(response.code))
            }
        }
    }

    private fun buildRequest(session: AuthenticatedSession, url: HttpUrl): Request {
        val builder = Request.Builder().url(url).get()
        // Headers are recomputed fresh on every call (never cached here) per
        // AuthenticatedSession's contract, and never logged.
        session.headers().forEach { (name, value) -> builder.addHeader(name, value) }
        return builder.build()
    }

    private suspend fun <T> runRequest(
        request: Request,
        onResponse: (Response) -> ApiResult<T>,
    ): ApiResult<T> {
        val response = try {
            client.newCall(request).await()
        } catch (e: CancellationException) {
            throw e
        } catch (e: IOException) {
            return ApiResult.Failure(ApiFailure.Network(e.message))
        }
        return response.use(onResponse)
    }

    private inline fun <reified D, T> decodeOrMalformed(
        response: Response,
        toDomain: (D) -> T,
    ): ApiResult<T> {
        val body = response.body?.string()
        if (body == null) return ApiResult.Failure(ApiFailure.MalformedPayload)
        return try {
            ApiResult.Success(toDomain(json.decodeFromString<D>(body)))
        } catch (e: SerializationException) {
            ApiResult.Failure(ApiFailure.MalformedPayload)
        } catch (e: IllegalArgumentException) {
            ApiResult.Failure(ApiFailure.MalformedPayload)
        } catch (e: ContractViolationException) {
            ApiResult.Failure(ApiFailure.MalformedPayload)
        }
    }
}

private suspend fun Call.await(): Response = suspendCancellableCoroutine { cont ->
    cont.invokeOnCancellation { runCatching { cancel() } }
    enqueue(object : Callback {
        override fun onFailure(call: Call, e: IOException) {
            if (!cont.isCancelled) cont.resumeWithException(e)
        }

        override fun onResponse(call: Call, response: Response) {
            cont.resume(response)
        }
    })
}
