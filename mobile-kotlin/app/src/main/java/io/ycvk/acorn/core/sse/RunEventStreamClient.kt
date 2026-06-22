package io.ycvk.acorn.core.sse

import io.ycvk.acorn.api.infrastructure.Serializer
import io.ycvk.acorn.api.models.ClientRunStartedEvent
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.sse.EventSource
import okhttp3.sse.EventSourceListener
import okhttp3.sse.EventSources
import java.util.concurrent.TimeUnit

/**
 * SSE client for `GET /v1/runs/{runId}/events?follow=true&after_seq=N`.
 *
 * Consumes the mobile live RunEvent subset. Hand-written because the OpenAPI spec
 * models RunEvent as a oneOf interface; the SSE transport itself is not modelled.
 *
 * NOTE: Full discriminated-union deserialization lands in B2 (RunEventProjection).
 * The generated RunEvent oneOf interface is not implemented by any concrete class, so
 * until B2 introduces a real decoder the skeleton surfaces the concrete envelope type
 * [ClientRunStartedEvent] to callers.
 */
class RunEventStreamClient(
    private val baseUrl: String,
    private val accessToken: String,
) {
    private val client: OkHttpClient = OkHttpClient.Builder()
        .connectTimeout(30, TimeUnit.SECONDS)
        .readTimeout(0, TimeUnit.SECONDS) // no read timeout for SSE
        .build()

    // Reuse the generated client's Moshi (date/UUID adapters + KotlinJsonAdapterFactory)
    // so envelope fields (OffsetDateTime ts, Long seq) decode the same way as REST calls.
    private val moshi = Serializer.moshi

    private val envelopeAdapter by lazy {
        moshi.adapter(ClientRunStartedEvent::class.java)
    }

    private val factory = EventSources.createFactory(client)

    fun streamRunEvents(
        runId: String,
        afterSeq: Int = 0,
        onEvent: (ClientRunStartedEvent) -> Unit,
        onError: (Throwable) -> Unit,
        onClosed: () -> Unit,
    ): EventSource {
        val url = "$baseUrl/v1/runs/$runId/events?follow=true&after_seq=$afterSeq"
        val request = Request.Builder()
            .url(url)
            .header("Authorization", "Bearer $accessToken")
            .header("Accept", "text/event-stream")
            .build()

        return factory.newEventSource(request, object : EventSourceListener() {
            override fun onEvent(eventSource: EventSource, id: String?, type: String?, data: String) {
                try {
                    onEvent(parseRunEvent(data))
                } catch (e: Exception) {
                    onError(e)
                }
            }

            override fun onClosed(eventSource: EventSource) {
                onClosed()
            }

            override fun onFailure(eventSource: EventSource, t: Throwable?, response: Response?) {
                onError(t ?: RuntimeException("SSE failure: ${response?.code}"))
            }
        })
    }

    /**
     * Parses an SSE `data` payload into a concrete envelope event.
     *
     * B2 will dispatch on the `type` discriminator and decode the matching concrete
     * subtype (ClientAssistantDeltaEvent, ClientRunCompletedEvent, ...). Until then we
     * decode into [ClientRunStartedEvent] so the shared envelope (event_id, run_id, seq,
     * ts, type) is available to callers; the `data` block is typed to RunStartedData and
     * will be null for non-started events.
     */
    private fun parseRunEvent(data: String): ClientRunStartedEvent {
        // TODO(b2): peek the `type` discriminator and decode the matching Client*Event subtype.
        return envelopeAdapter.fromJson(data)
            ?: throw IllegalStateException("Failed to decode RunEvent from SSE data")
    }
}
