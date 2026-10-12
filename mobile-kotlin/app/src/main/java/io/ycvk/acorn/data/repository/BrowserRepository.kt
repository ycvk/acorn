package io.ycvk.acorn.data.repository

import io.ycvk.acorn.api.apis.BrowserApi
import io.ycvk.acorn.api.infrastructure.ApiClient
import io.ycvk.acorn.api.infrastructure.Serializer
import io.ycvk.acorn.api.models.BrowserFrame
import io.ycvk.acorn.api.models.BrowserInputRequest
import io.ycvk.acorn.api.models.BrowserTab
import io.ycvk.acorn.core.auth.ConnectionProfile
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.sse.EventSource
import okhttp3.sse.EventSourceListener
import okhttp3.sse.EventSources
import java.util.concurrent.TimeUnit
import javax.inject.Inject
import javax.inject.Singleton

/**
 * The agent's own browser on its workstation: its pages, a live stream of one
 * page, and input into it. The owner signs in to sites here for the agent.
 * [tabs] and [input] block; call them from an IO dispatcher.
 */
@Singleton
class BrowserRepository @Inject constructor() {
    private val streams = OkHttpClient.Builder()
        .connectTimeout(30, TimeUnit.SECONDS)
        .readTimeout(0, TimeUnit.SECONDS)
        .build()
    private val frameAdapter = Serializer.moshi.adapter(BrowserFrame::class.java)
    private val errorAdapter = Serializer.moshi.adapter(BrowserStreamError::class.java)

    fun tabs(profile: ConnectionProfile): List<BrowserTab> = api(profile).clientListBrowserTabs().tabs

    fun input(profile: ConnectionProfile, request: BrowserInputRequest) {
        api(profile).clientBrowserInput(request)
    }

    /**
     * Streams [tab], laid out for a phone screen of [width] by [height] CSS
     * pixels, until cancelled; a stream ends through [onError] or [onClosed].
     */
    fun frames(
        profile: ConnectionProfile,
        tab: String,
        width: Int,
        height: Int,
        onFrame: (BrowserFrame) -> Unit,
        onError: (Throwable) -> Unit,
        onClosed: () -> Unit,
    ): EventSource {
        val url = "${profile.serverUrl}/v1/browser/frames".toHttpUrl().newBuilder()
            .addQueryParameter("tab", tab)
            .addQueryParameter("width", width.toString())
            .addQueryParameter("height", height.toString())
            .build()
        val request = Request.Builder()
            .url(url)
            .header("Authorization", "Bearer ${profile.accessToken}")
            .header("Accept", "text/event-stream")
            .build()
        return EventSources.createFactory(streams).newEventSource(request, object : EventSourceListener() {
            override fun onEvent(eventSource: EventSource, id: String?, type: String?, data: String) {
                when (type) {
                    "frame" -> frameAdapter.fromJson(data)?.let(onFrame)
                        ?: onError(IllegalStateException("bad browser frame"))
                    "error" -> onError(IllegalStateException(errorAdapter.fromJson(data)?.error ?: data))
                }
            }

            override fun onClosed(eventSource: EventSource) = onClosed()

            override fun onFailure(eventSource: EventSource, t: Throwable?, response: Response?) {
                onError(t ?: IllegalStateException("browser stream failed: HTTP ${response?.code}"))
            }
        })
    }

    private fun api(profile: ConnectionProfile): BrowserApi {
        ApiClient.accessToken = profile.accessToken
        return BrowserApi(basePath = profile.serverUrl)
    }
}

/** The body of an `error` event that ends a frame stream. */
data class BrowserStreamError(val error: String)
