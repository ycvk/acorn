package io.ycvk.acorn.core.sse

import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import okhttp3.OkHttpClient
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

    @Provides
    @Singleton
    fun provideRunEventStreamClient(@SseHttp client: OkHttpClient): RunEventStreamClient = RunEventStreamClient(client)
}
