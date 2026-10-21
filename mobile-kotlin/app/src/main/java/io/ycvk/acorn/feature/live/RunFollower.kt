package io.ycvk.acorn.feature.live

import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.sse.ChatState
import io.ycvk.acorn.core.sse.RunEventPacket
import io.ycvk.acorn.core.sse.RunEventProjection
import io.ycvk.acorn.core.sse.RunStatus
import io.ycvk.acorn.feature.chat.reconnectDelayMs
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.IOException
import java.util.UUID

/** A run the app follows: its thread, the state its events project to, and whether a Live Update shows it. */
data class FollowedRun(
    val runId: String,
    val threadId: String,
    val title: String,
    /** Started or resumed from this phone, so a Live Update shows it while it runs. */
    val live: Boolean,
    val state: ChatState,
    /** The owner asked to stop it; ending as a failure then leaves no notification. */
    val stopping: Boolean = false,
) {
    val running: Boolean get() = state.runStatus == RunStatus.Running
}

/** A message on its way from this phone, shown as sending until its run has an id. */
data class PendingSend(val key: String, val title: String)

/** One step of a followed run: the event it took and the run after it, or [lost] once its stream could not be kept. */
data class RunUpdate(val run: FollowedRun, val packet: RunEventPacket?, val lost: Throwable? = null) {
    val ended: Boolean get() = packet is RunEventPacket.RunCompleted || packet is RunEventPacket.RunFailed
}

/** Opens a run's event stream after event [afterSeq], from its first event at 0; closing the handle ends it. */
fun interface RunStreams {
    fun open(
        profile: ConnectionProfile,
        runId: String,
        afterSeq: Long,
        onEvent: (RunEventPacket) -> Unit,
        onError: (Throwable) -> Unit,
        onClosed: () -> Unit,
    ): AutoCloseable
}

/** Keeps the Live Update service running while it has something to show. */
fun interface LiveUpdates {
    fun ensureShown()
}

/** Hears when a run shown as a Live Update ends. */
fun interface RunEndings {
    fun ended(run: FollowedRun)
}

/** Reads a thread's title, for a run followed without one. Blocking. */
fun interface ThreadTitles {
    fun title(profile: ConnectionProfile, threadId: String): String
}

/**
 * Holds the event stream of every run the app follows and projects it. The
 * chat screen reads a run from here instead of opening its own stream, and
 * the Live Update service shows the runs started from this phone while they
 * run. A run is kept while it runs as a Live Update or while a screen watches
 * its thread; it is dropped when it ends, when its stream cannot be kept, or
 * when it waits on the owner with nobody watching. A broken stream is opened
 * again after the last event read.
 *
 * All state changes run on [scope], which must run one coroutine at a time.
 */
class RunFollower(
    private val streams: RunStreams,
    private val projection: RunEventProjection,
    private val liveUpdates: LiveUpdates,
    private val endings: RunEndings,
    private val titles: ThreadTitles,
    private val scope: CoroutineScope,
    private val io: CoroutineDispatcher,
) {
    private class Tracked(
        val profile: ConnectionProfile,
        /** What the events read so far project to; shown once [awaiting] is null. */
        var state: ChatState,
        /** The decision whose event ends the replay of what came before it. */
        var awaiting: String?,
    ) {
        var handle: AutoCloseable? = null
        var generation = 0
        var failures = 0
        var lastSeq = 0L
    }

    private val tracked = mutableMapOf<String, Tracked>()
    private val watching = mutableMapOf<String, Int>()

    private val _runs = MutableStateFlow<Map<String, FollowedRun>>(emptyMap())
    val runs: StateFlow<Map<String, FollowedRun>> = _runs.asStateFlow()

    private val _sending = MutableStateFlow<List<PendingSend>>(emptyList())
    val sending: StateFlow<List<PendingSend>> = _sending.asStateFlow()

    private val _updates = MutableSharedFlow<RunUpdate>(extraBufferCapacity = 64)
    val updates: SharedFlow<RunUpdate> = _updates.asSharedFlow()

    /**
     * Shows a message as sending before its run exists. Call it while the app
     * is in front, so the Live Update service can start; the key goes to
     * [follow] once the run has an id, or to [endSend] if sending fails.
     */
    fun beginSend(title: String): String {
        val key = UUID.randomUUID().toString()
        _sending.update { it + PendingSend(key, title) }
        liveUpdates.ensureShown()
        return key
    }

    fun endSend(key: String) {
        _sending.update { sends -> sends.filterNot { it.key == key } }
    }

    /**
     * Follows a run from its first event. A [live] run shows as a Live Update
     * while it runs; following a run already followed only makes it live.
     * Without a [title] the thread's title is read from the server. A run
     * followed for the owner's [decided] action shows as starting until that
     * decision's event, so the wait before it does not end the run here.
     */
    fun follow(
        profile: ConnectionProfile,
        runId: String,
        threadId: String,
        title: String?,
        live: Boolean,
        sendKey: String? = null,
        decided: String? = null,
    ) {
        scope.launch {
            val existing = _runs.value[runId]
            if (existing == null) {
                put(FollowedRun(runId, threadId, title ?: DEFAULT_TITLE, live, starting))
                tracked[runId] = Tracked(profile, starting, decided)
                open(runId)
                if (title == null) readTitle(profile, runId, threadId)
            } else if (live && !existing.live) {
                put(existing.copy(live = true))
            }
            sendKey?.let(::endSend)
            if (live) liveUpdates.ensureShown()
        }
    }

    /** Marks a run the owner asked to stop, or unmarks it when the request failed. */
    fun stopping(runId: String, stopping: Boolean) {
        scope.launch { _runs.value[runId]?.let { put(it.copy(stopping = stopping)) } }
    }

    /** Marks a thread as shown on screen, so its runs are kept while they wait on the owner. */
    fun watch(threadId: String) {
        scope.launch { watching[threadId] = (watching[threadId] ?: 0) + 1 }
    }

    /** Ends a screen's watch; runs of the thread that are not running as Live Updates are dropped. */
    fun unwatch(threadId: String) {
        scope.launch {
            val left = (watching[threadId] ?: 0) - 1
            if (left > 0) watching[threadId] = left else watching.remove(threadId)
            if (left <= 0) {
                _runs.value.values.filter { it.threadId == threadId && !(it.live && it.running) }.forEach { drop(it.runId) }
            }
        }
    }

    /** Opens a run's stream again after the last event read, as the system may have cut it. */
    fun restart(runId: String) {
        scope.launch {
            val t = tracked[runId] ?: return@launch
            t.failures = 0
            reopen(runId, t)
        }
    }

    private fun open(runId: String) {
        val t = tracked[runId] ?: return
        val generation = ++t.generation
        fun current() = tracked[runId] === t && t.generation == generation
        t.handle = streams.open(
            t.profile,
            runId,
            t.lastSeq,
            onEvent = { packet -> scope.launch { if (current()) onEvent(runId, packet) } },
            onError = { error -> scope.launch { if (current()) onBroken(runId, error) } },
            onClosed = { scope.launch { if (current()) onBroken(runId, IOException("the server closed the stream before the run ended")) } },
        )
    }

    private fun reopen(runId: String, t: Tracked) {
        t.handle?.close()
        t.handle = null
        open(runId)
    }

    private suspend fun onEvent(runId: String, packet: RunEventPacket) {
        val t = tracked[runId] ?: return
        val run = _runs.value[runId] ?: return
        t.failures = 0
        t.lastSeq = maxOf(t.lastSeq, packet.seq)
        t.state = projection.apply(t.state, packet)
        val update = RunUpdate(run.copy(state = t.state), packet)
        if (t.awaiting != null && !update.ended) {
            if (decidedAction(packet) == t.awaiting) t.awaiting = null
            return
        }
        val next = update.run
        when {
            update.ended -> {
                drop(runId)
                if (next.live) endings.ended(next)
            }
            // A run waiting on the owner is read again when someone acts on it.
            !next.running && (watching[next.threadId] ?: 0) == 0 -> drop(runId)
            else -> {
                put(next)
                if (next.live && next.running && !run.running) liveUpdates.ensureShown()
            }
        }
        _updates.emit(update)
    }

    private suspend fun onBroken(runId: String, error: Throwable) {
        val t = tracked[runId] ?: return
        t.handle?.close()
        t.handle = null
        val wait = reconnectDelayMs(t.failures++)
        if (wait == null) {
            val run = _runs.value[runId]
            drop(runId)
            run?.let { _updates.emit(RunUpdate(it, null, error)) }
            return
        }
        delay(wait)
        if (tracked[runId] === t) reopen(runId, t)
    }

    /** Names the run after its thread; when the title cannot be read the Live Update keeps saying Acorn. */
    private fun readTitle(profile: ConnectionProfile, runId: String, threadId: String) {
        scope.launch {
            val title = runCatching { withContext(io) { titles.title(profile, threadId) } }.getOrNull() ?: return@launch
            _runs.value[runId]?.let { put(it.copy(title = title)) }
        }
    }

    private fun put(run: FollowedRun) {
        _runs.update { it + (run.runId to run) }
    }

    private fun drop(runId: String) {
        tracked.remove(runId)?.handle?.close()
        _runs.update { it - runId }
    }

    private companion object {
        const val DEFAULT_TITLE = "Acorn"
        val starting = ChatState(isStreaming = true, runStatus = RunStatus.Running)
    }
}

/** The pending action an event records the owner's decision on. */
private fun decidedAction(packet: RunEventPacket): String? = when (packet) {
    is RunEventPacket.ToolApprovalDecided -> packet.event.`data`.actionId
    is RunEventPacket.OperatorQuestionDecided -> packet.event.`data`.actionId
    is RunEventPacket.ElicitationDecided -> packet.event.`data`.actionId
    else -> null
}
