package io.ycvk.acorn.core.sse

import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import io.ycvk.acorn.api.infrastructure.ClientError
import io.ycvk.acorn.api.infrastructure.ClientException
import io.ycvk.acorn.api.infrastructure.ServerError
import io.ycvk.acorn.api.infrastructure.ServerException
import okhttp3.OkHttpClient
import okhttp3.Response
import java.util.concurrent.TimeUnit
import javax.inject.Qualifier
import javax.inject.Singleton

/** The HTTP client for server-sent event streams: no read timeout. */
@Qualifier
@Retention(AnnotationRetention.BINARY)
annotation class SseHttp

@Module
@InstallIn(SingletonComponent::class)
object SseModule {
    @Provides
    @Singleton
    @SseHttp
    fun provideSseHttpClient(): OkHttpClient = OkHttpClient.Builder()
        .connectTimeout(30, TimeUnit.SECONDS)
        .readTimeout(0, TimeUnit.SECONDS)
        .build()
}

/**
 * What an event stream failed with: OkHttp's error, or for an answer that
 * is not 2xx the exception the REST calls throw for that answer, so the
 * owner reads the same words for it (a revoked device is asked to pair
 * again). Call it from onFailure, while the response body can be read.
 */
fun streamFailure(error: Throwable?, response: Response?): Throwable {
    if (error != null) return error
    if (response == null) return IllegalStateException("the event stream failed without an error or a response")
    val body = runCatching { response.body.string() }
    val headers = response.headers.toMultimap()
    val failure = if (response.code in 400..499) {
        ClientException("Client error : ${response.code} ${response.message}", response.code, ClientError<Unit>(response.message, body.getOrNull(), response.code, headers))
    } else {
        ServerException("Server error : ${response.code} ${response.message}", response.code, ServerError<Unit>(response.message, body.getOrNull(), response.code, headers))
    }
    body.exceptionOrNull()?.let(failure::addSuppressed)
    return failure
}
