package io.ycvk.acorn.core.sse

import com.squareup.moshi.JsonAdapter
import io.ycvk.acorn.api.infrastructure.Serializer
import io.ycvk.acorn.api.models.ClientAssistantDeltaEvent
import io.ycvk.acorn.api.models.ClientElicitationDecidedEvent
import io.ycvk.acorn.api.models.ClientElicitationPendingEvent
import io.ycvk.acorn.api.models.ClientOperatorQuestionDecidedEvent
import io.ycvk.acorn.api.models.ClientOperatorQuestionPendingEvent
import io.ycvk.acorn.api.models.ClientToolApprovalDecidedEvent
import io.ycvk.acorn.api.models.ClientToolApprovalPendingEvent
import io.ycvk.acorn.api.models.ClientRunCompletedEvent
import io.ycvk.acorn.api.models.ClientRunFailedEvent
import io.ycvk.acorn.api.models.ClientRunInterruptedEvent
import io.ycvk.acorn.api.models.ClientRunResumeRequestedEvent
import io.ycvk.acorn.api.models.ClientRunStartedEvent
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.sse.EventSource
import okhttp3.sse.EventSourceListener
import okhttp3.sse.EventSources
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.TimeUnit

/**
 * SSE client for `GET /v1/runs/{runId}/events?follow=true&after_seq=N`.
 *
 * Consumes the mobile live RunEvent subset. Hand-written because the OpenAPI spec
 * models RunEvent as a oneOf interface; the SSE transport itself is not modelled.
 *
 * Decoding dispatches on the SSE `event:` line (the `type` parameter of
 * [EventSourceListener.onEvent]) to select the matching concrete [Client*Event]
 * Moshi adapter.
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

    // Moshi builds these adapters through kotlin-reflect, which takes seconds on a
    // cold debug build. They are created on first use from the OkHttp callback
    // thread; this client is constructed on the main thread.
    private val eventTypes: Map<String, Class<*>> = mapOf(
        "run.started" to ClientRunStartedEvent::class.java,
        "assistant.delta" to ClientAssistantDeltaEvent::class.java,
        "run.completed" to ClientRunCompletedEvent::class.java,
        "run.failed" to ClientRunFailedEvent::class.java,
        "run.interrupted" to ClientRunInterruptedEvent::class.java,
        "run.resume_requested" to ClientRunResumeRequestedEvent::class.java,
        "elicitation.pending" to ClientElicitationPendingEvent::class.java,
        "elicitation.decided" to ClientElicitationDecidedEvent::class.java,
        "operator_question.pending" to ClientOperatorQuestionPendingEvent::class.java,
        "operator_question.decided" to ClientOperatorQuestionDecidedEvent::class.java,
        "tool_approval.pending" to ClientToolApprovalPendingEvent::class.java,
        "tool_approval.decided" to ClientToolApprovalDecidedEvent::class.java,
    )

    private val eventAdapters = ConcurrentHashMap<String, JsonAdapter<*>>()

    private val factory = EventSources.createFactory(client)

    fun streamRunEvents(
        runId: String,
        afterSeq: Int = 0,
        onEvent: (RunEventPacket) -> Unit,
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
                    val packet = parseRunEvent(type, data)
                    onEvent(packet)
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
     * Parses an SSE `data` payload into a [RunEventPacket] by dispatching on the
     * `type` discriminator (the SSE `event:` line).
     *
     * If the type is unknown the raw data is preserved in [RunEventPacket.Unknown].
     */
    @Suppress("UNCHECKED_CAST")
    private fun parseRunEvent(type: String?, data: String): RunEventPacket {
        if (type == null) {
            return RunEventPacket.Unknown(rawType = "(null)", rawData = data)
        }

        val eventType = eventTypes[type]
            ?: return RunEventPacket.Unknown(rawType = type, rawData = data)
        val adapter = eventAdapters.computeIfAbsent(type) { moshi.adapter(eventType) }

        val parsed = adapter.fromJson(data)
            ?: throw IllegalStateException("Failed to decode RunEvent (type=$type) from SSE data")

        return when (type) {
            "run.started" -> RunEventPacket.Started(parsed as ClientRunStartedEvent)
            "assistant.delta" -> RunEventPacket.AssistantDelta(parsed as ClientAssistantDeltaEvent)
            "run.completed" -> RunEventPacket.RunCompleted(parsed as ClientRunCompletedEvent)
            "run.failed" -> RunEventPacket.RunFailed(parsed as ClientRunFailedEvent)
            "run.interrupted" -> RunEventPacket.RunInterrupted(parsed as ClientRunInterruptedEvent)
            "run.resume_requested" -> RunEventPacket.RunResumeRequested(parsed as ClientRunResumeRequestedEvent)
            "elicitation.pending" -> RunEventPacket.ElicitationPending(parsed as ClientElicitationPendingEvent)
            "elicitation.decided" -> RunEventPacket.ElicitationDecided(parsed as ClientElicitationDecidedEvent)
            "operator_question.pending" -> RunEventPacket.OperatorQuestionPending(parsed as ClientOperatorQuestionPendingEvent)
            "operator_question.decided" -> RunEventPacket.OperatorQuestionDecided(parsed as ClientOperatorQuestionDecidedEvent)
            "tool_approval.pending" -> RunEventPacket.ToolApprovalPending(parsed as ClientToolApprovalPendingEvent)
            "tool_approval.decided" -> RunEventPacket.ToolApprovalDecided(parsed as ClientToolApprovalDecidedEvent)
            else -> RunEventPacket.Unknown(rawType = type, rawData = data)
        }
    }
}

/**
 * Discriminated-union wrapper around the concrete generated `Client*Event` envelope
 * classes. Each subtype delegates [eventId], [runId], [seq] to the wrapped event so
 * callers can access envelope fields without unwrapping.
 *
 * [Unknown] preserves the raw SSE type and data for events the client does not
 * recognise, so they can be logged or ignored without losing information.
 */
sealed class RunEventPacket {
    abstract val eventId: String
    abstract val runId: String
    abstract val seq: Long

    data class Started(val event: ClientRunStartedEvent) : RunEventPacket() {
        override val eventId: String get() = event.eventId
        override val runId: String get() = event.runId
        override val seq: Long get() = event.seq
    }

    data class AssistantDelta(val event: ClientAssistantDeltaEvent) : RunEventPacket() {
        override val eventId: String get() = event.eventId
        override val runId: String get() = event.runId
        override val seq: Long get() = event.seq
    }

    data class RunCompleted(val event: ClientRunCompletedEvent) : RunEventPacket() {
        override val eventId: String get() = event.eventId
        override val runId: String get() = event.runId
        override val seq: Long get() = event.seq
    }

    data class RunFailed(val event: ClientRunFailedEvent) : RunEventPacket() {
        override val eventId: String get() = event.eventId
        override val runId: String get() = event.runId
        override val seq: Long get() = event.seq
    }

    data class RunInterrupted(val event: ClientRunInterruptedEvent) : RunEventPacket() {
        override val eventId: String get() = event.eventId
        override val runId: String get() = event.runId
        override val seq: Long get() = event.seq
    }

    data class RunResumeRequested(val event: ClientRunResumeRequestedEvent) : RunEventPacket() {
        override val eventId: String get() = event.eventId
        override val runId: String get() = event.runId
        override val seq: Long get() = event.seq
    }

    data class ElicitationPending(val event: ClientElicitationPendingEvent) : RunEventPacket() {
        override val eventId: String get() = event.eventId
        override val runId: String get() = event.runId
        override val seq: Long get() = event.seq
    }

    data class ElicitationDecided(val event: ClientElicitationDecidedEvent) : RunEventPacket() {
        override val eventId: String get() = event.eventId
        override val runId: String get() = event.runId
        override val seq: Long get() = event.seq
    }

    data class OperatorQuestionPending(val event: ClientOperatorQuestionPendingEvent) : RunEventPacket() {
        override val eventId: String get() = event.eventId
        override val runId: String get() = event.runId
        override val seq: Long get() = event.seq
    }

    data class OperatorQuestionDecided(val event: ClientOperatorQuestionDecidedEvent) : RunEventPacket() {
        override val eventId: String get() = event.eventId
        override val runId: String get() = event.runId
        override val seq: Long get() = event.seq
    }

    data class ToolApprovalPending(val event: ClientToolApprovalPendingEvent) : RunEventPacket() {
        override val eventId: String get() = event.eventId
        override val runId: String get() = event.runId
        override val seq: Long get() = event.seq
    }

    data class ToolApprovalDecided(val event: ClientToolApprovalDecidedEvent) : RunEventPacket() {
        override val eventId: String get() = event.eventId
        override val runId: String get() = event.runId
        override val seq: Long get() = event.seq
    }

    data class Unknown(val rawType: String, val rawData: String) : RunEventPacket() {
        override val eventId: String = ""
        override val runId: String = ""
        override val seq: Long = 0L
    }
}
