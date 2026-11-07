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
import io.ycvk.acorn.api.models.ClientToolCallFailedEvent
import io.ycvk.acorn.api.models.ClientToolCallStartedEvent
import io.ycvk.acorn.api.models.ClientToolCallSucceededEvent
import io.ycvk.acorn.api.models.ClientRunCompletedEvent
import io.ycvk.acorn.api.models.ClientRunFailedEvent
import io.ycvk.acorn.api.models.ClientRunInputEvent
import io.ycvk.acorn.api.models.ClientRunInterruptedEvent
import io.ycvk.acorn.api.models.ClientRunResumeRequestedEvent
import io.ycvk.acorn.api.models.ClientRunStartedEvent
import io.ycvk.acorn.api.models.ClientPlanUpdatedEvent
import io.ycvk.acorn.api.models.ClientSubRunUpdatedEvent
import io.ycvk.acorn.api.models.ClientViewShownEvent
import io.ycvk.acorn.api.models.ClientViewUpdatedEvent
import io.ycvk.acorn.core.auth.ConnectionProfile
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.sse.EventSource
import okhttp3.sse.EventSourceListener
import okhttp3.sse.EventSources
import java.util.concurrent.ConcurrentHashMap
import javax.inject.Inject
import javax.inject.Singleton

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
@Singleton
class RunEventStreamClient @Inject constructor(@SseHttp client: OkHttpClient) {
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
        "run.input" to ClientRunInputEvent::class.java,
        "elicitation.pending" to ClientElicitationPendingEvent::class.java,
        "elicitation.decided" to ClientElicitationDecidedEvent::class.java,
        "operator_question.pending" to ClientOperatorQuestionPendingEvent::class.java,
        "operator_question.decided" to ClientOperatorQuestionDecidedEvent::class.java,
        "tool_approval.pending" to ClientToolApprovalPendingEvent::class.java,
        "tool_approval.decided" to ClientToolApprovalDecidedEvent::class.java,
        "tool.call.started" to ClientToolCallStartedEvent::class.java,
        "tool.call.succeeded" to ClientToolCallSucceededEvent::class.java,
        "tool.call.failed" to ClientToolCallFailedEvent::class.java,
        "view.shown" to ClientViewShownEvent::class.java,
        "view.updated" to ClientViewUpdatedEvent::class.java,
        "sub_run.updated" to ClientSubRunUpdatedEvent::class.java,
        "plan.updated" to ClientPlanUpdatedEvent::class.java,
    )

    private val eventAdapters = ConcurrentHashMap<String, JsonAdapter<*>>()

    private val factory = EventSources.createFactory(client)

    fun streamRunEvents(
        profile: ConnectionProfile,
        runId: String,
        afterSeq: Long,
        onEvent: (RunEventPacket) -> Unit,
        onError: (Throwable) -> Unit,
        onClosed: () -> Unit,
    ): EventSource {
        val url = "${profile.serverUrl}/v1/runs/$runId/events?follow=true&after_seq=$afterSeq"
        val request = Request.Builder()
            .url(url)
            .header("Authorization", "Bearer ${profile.accessToken}")
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
     * An event of an unknown type is [RunEventPacket.Unknown].
     */
    private fun parseRunEvent(type: String?, data: String): RunEventPacket {
        if (type == null) {
            return RunEventPacket.Unknown
        }

        val eventType = eventTypes[type]
            ?: return RunEventPacket.Unknown
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
            "run.input" -> RunEventPacket.Input(parsed as ClientRunInputEvent)
            "elicitation.pending" -> RunEventPacket.ElicitationPending(parsed as ClientElicitationPendingEvent)
            "elicitation.decided" -> RunEventPacket.ElicitationDecided(parsed as ClientElicitationDecidedEvent)
            "operator_question.pending" -> RunEventPacket.OperatorQuestionPending(parsed as ClientOperatorQuestionPendingEvent)
            "operator_question.decided" -> RunEventPacket.OperatorQuestionDecided(parsed as ClientOperatorQuestionDecidedEvent)
            "tool_approval.pending" -> RunEventPacket.ToolApprovalPending(parsed as ClientToolApprovalPendingEvent)
            "tool_approval.decided" -> RunEventPacket.ToolApprovalDecided(parsed as ClientToolApprovalDecidedEvent)
            "tool.call.started" -> RunEventPacket.ToolCallStarted(parsed as ClientToolCallStartedEvent)
            "tool.call.succeeded" -> RunEventPacket.ToolCallSucceeded(parsed as ClientToolCallSucceededEvent)
            "tool.call.failed" -> RunEventPacket.ToolCallFailed(parsed as ClientToolCallFailedEvent)
            "view.shown" -> RunEventPacket.ViewShown(parsed as ClientViewShownEvent)
            "view.updated" -> RunEventPacket.ViewUpdated(parsed as ClientViewUpdatedEvent)
            "sub_run.updated" -> RunEventPacket.SubRunUpdated(parsed as ClientSubRunUpdatedEvent)
            "plan.updated" -> RunEventPacket.PlanUpdated(parsed as ClientPlanUpdatedEvent)
            else -> RunEventPacket.Unknown
        }
    }
}

/**
 * Discriminated-union wrapper around the concrete generated `Client*Event` envelope
 * classes. Each subtype takes [eventId] and [seq] from the wrapped event so callers
 * can read envelope fields without unwrapping.
 *
 * [Unknown] stands for an event the client does not recognise; it is ignored.
 */
sealed class RunEventPacket(val eventId: String, val seq: Long) {
    data class Started(val event: ClientRunStartedEvent) : RunEventPacket(event.eventId, event.seq)
    data class AssistantDelta(val event: ClientAssistantDeltaEvent) : RunEventPacket(event.eventId, event.seq)
    data class RunCompleted(val event: ClientRunCompletedEvent) : RunEventPacket(event.eventId, event.seq)
    data class RunFailed(val event: ClientRunFailedEvent) : RunEventPacket(event.eventId, event.seq)
    data class RunInterrupted(val event: ClientRunInterruptedEvent) : RunEventPacket(event.eventId, event.seq)
    data class RunResumeRequested(val event: ClientRunResumeRequestedEvent) : RunEventPacket(event.eventId, event.seq)

    /** The run took an owner message that waited for it. */
    data class Input(val event: ClientRunInputEvent) : RunEventPacket(event.eventId, event.seq)
    data class ElicitationPending(val event: ClientElicitationPendingEvent) : RunEventPacket(event.eventId, event.seq)
    data class ElicitationDecided(val event: ClientElicitationDecidedEvent) : RunEventPacket(event.eventId, event.seq)
    data class OperatorQuestionPending(val event: ClientOperatorQuestionPendingEvent) : RunEventPacket(event.eventId, event.seq)
    data class OperatorQuestionDecided(val event: ClientOperatorQuestionDecidedEvent) : RunEventPacket(event.eventId, event.seq)
    data class ToolApprovalPending(val event: ClientToolApprovalPendingEvent) : RunEventPacket(event.eventId, event.seq)
    data class ToolApprovalDecided(val event: ClientToolApprovalDecidedEvent) : RunEventPacket(event.eventId, event.seq)
    data class ToolCallStarted(val event: ClientToolCallStartedEvent) : RunEventPacket(event.eventId, event.seq)
    data class ToolCallSucceeded(val event: ClientToolCallSucceededEvent) : RunEventPacket(event.eventId, event.seq)
    data class ToolCallFailed(val event: ClientToolCallFailedEvent) : RunEventPacket(event.eventId, event.seq)
    data class ViewShown(val event: ClientViewShownEvent) : RunEventPacket(event.eventId, event.seq)
    data class ViewUpdated(val event: ClientViewUpdatedEvent) : RunEventPacket(event.eventId, event.seq)

    /** A task the run handed to a sub-agent started an attempt, reported or was accepted. */
    data class SubRunUpdated(val event: ClientSubRunUpdatedEvent) : RunEventPacket(event.eventId, event.seq)

    /** The run wrote its whole plan again. */
    data class PlanUpdated(val event: ClientPlanUpdatedEvent) : RunEventPacket(event.eventId, event.seq)

    data object Unknown : RunEventPacket("", 0L)
}
