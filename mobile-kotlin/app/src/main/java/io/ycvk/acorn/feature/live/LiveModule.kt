package io.ycvk.acorn.feature.live

import dagger.Module
import io.ycvk.acorn.core.auth.authorized
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import io.ycvk.acorn.api.apis.ClientApi
import io.ycvk.acorn.core.sse.RunEventProjection
import io.ycvk.acorn.core.sse.RunEventStreamClient
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import javax.inject.Singleton

/** Reads runs over the server's event stream. */
fun sseRunStreams(client: RunEventStreamClient) = RunStreams { profile, runId, afterSeq, onEvent, onError, onClosed ->
    val source = client.streamRunEvents(profile = profile, runId = runId, afterSeq = afterSeq, onEvent = onEvent, onError = onError, onClosed = onClosed)
    AutoCloseable { source.cancel() }
}

@Module
@InstallIn(SingletonComponent::class)
object LiveModule {
    @Provides
    @Singleton
    fun provideRunFollower(
        projection: RunEventProjection,
        liveUpdates: ServiceLiveUpdates,
        notifications: LiveNotifications,
        streams: RunEventStreamClient,
    ): RunFollower = RunFollower(
        streams = sseRunStreams(streams),
        projection = projection,
        liveUpdates = liveUpdates,
        endings = notifications,
        titles = { profile, threadId ->
            ClientApi(basePath = profile.serverUrl).authorized(profile).clientGetThread(threadId).title
        },
        scope = CoroutineScope(SupervisorJob() + Dispatchers.Default.limitedParallelism(1)),
        io = Dispatchers.IO,
    )
}
