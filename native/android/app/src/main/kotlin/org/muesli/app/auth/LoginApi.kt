package org.muesli.app.auth

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.serialization.Serializable
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json
import okhttp3.Call
import okhttp3.Callback
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import java.io.IOException
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

/** The outcome of a [LoginApi.login] call. */
sealed interface LoginResult {
    data class Success(val token: String) : LoginResult
    data class Failure(val message: String) : LoginResult
}

@Serializable
private data class LoginRequestDto(val email: String, val password: String)

@Serializable
private data class LoginResponseDto(val token: String)

/**
 * Calls the Go server's `POST /api/login` (internal/api/session.go) to
 * exchange an email + password for a bearer token (issue #768 Task 6, the
 * host app's only credential flow). The password is never persisted here
 * or by any caller; only the returned token is, via [org.muesli.app.session.AppSessionSource].
 */
class LoginApi(
    private val client: OkHttpClient,
    private val json: Json = Json { ignoreUnknownKeys = true },
) {
    suspend fun login(baseUrl: String, email: String, password: String): LoginResult {
        val url = try {
            baseUrl.trim().toHttpUrl().newBuilder().addPathSegments("api/login").build()
        } catch (e: IllegalArgumentException) {
            return LoginResult.Failure("Invalid server URL")
        }
        val requestBody = json.encodeToString(LoginRequestDto.serializer(), LoginRequestDto(email, password))
            .toRequestBody("application/json".toMediaType())
        val request = Request.Builder().url(url).post(requestBody).build()

        val response = try {
            client.newCall(request).await()
        } catch (e: CancellationException) {
            throw e
        } catch (e: IOException) {
            return LoginResult.Failure(e.message ?: "Network error")
        }
        return try {
            response.use { resp ->
                when (resp.code) {
                    200 -> {
                        val body = resp.body?.string()
                        val token = body?.let {
                            try {
                                json.decodeFromString(LoginResponseDto.serializer(), it).token
                            } catch (e: SerializationException) {
                                null
                            }
                        }
                        if (token.isNullOrBlank()) LoginResult.Failure("Malformed response") else LoginResult.Success(token)
                    }
                    401 -> LoginResult.Failure("Invalid email or password")
                    else -> LoginResult.Failure("Server error (${resp.code})")
                }
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: IOException) {
            LoginResult.Failure(e.message ?: "Network error")
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
            cont.resume(response) { _, _, _ -> response.close() }
        }
    })
}
