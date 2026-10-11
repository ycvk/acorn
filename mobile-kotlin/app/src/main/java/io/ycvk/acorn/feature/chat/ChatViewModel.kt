package io.ycvk.acorn.feature.chat

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.apis.ClientApi
import io.ycvk.acorn.api.infrastructure.ApiClient
import io.ycvk.acorn.api.models.CreateRunRequest
import io.ycvk.acorn.api.models.CreateThreadRequest
import io.ycvk.acorn.api.models.InboxResponse
import io.ycvk.acorn.api.models.RunSummary
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.sse.ChatState
import io.ycvk.acorn.core.sse.RunEventPacket
import io.ycvk.acorn.core.sse.RunEventProjection
import io.ycvk.acorn.core.sse.RunEventStreamClient
import io.ycvk.acorn.core.sse.RunStatus
import io.ycvk.acorn.core.ui.userMessage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import okhttp3.sse.EventSource
import javax.inject.Inject

/**
 * Owns a single chat thread: its persisted message history plus the live
 * streaming state of an in-flight run.
 *
 * - [threadId] / [messages] are the persisted view: loaded from the server once a
 *   thread is selected, and appended to locally when the user sends a message or
 *   a run completes. A new conversation has no thread until its first message.
 * - [chatState] is the transient streaming view: assistant text/reasoning deltas,
 *   pending activities. Reset on every new run and folded into [messages] once
 *   the run reaches a terminal event.
 */
@HiltViewModel
class ChatViewModel @Inject constructor(
    private val projection: RunEventProjection,
    private val authController: AuthController,
) : ViewModel() {

    private val _threadId = MutableStateFlow<String?>(null)
    val threadId: StateFlow<String?> = _threadId.asStateFlow()

    private val _threadTitle = MutableStateFlow<String?>(null)
    val threadTitle: StateFlow<String?> = _threadTitle.asStateFlow()

    private val _messages = MutableStateFlow<List<ChatMessage>>(emptyList())
    val messages: StateFlow<List<ChatMessage>> = _messages.asStateFlow()

    private val _chatState = MutableStateFlow(ChatState())
    val chatState: StateFlow<ChatState> = _chatState.asStateFlow()

    private val _error = MutableStateFlow<String?>(null)
    val error: StateFlow<String?> = _error.asStateFlow()

    /** Text of a message that failed to send, handed back to the composer. */
    private val _restoredInput = MutableStateFlow<String?>(null)
    val restoredInput: StateFlow<String?> = _restoredInput.asStateFlow()

    private var eventSource: EventSource? = null
    private var streamingRunId: String? = null
    private val streamGeneration = java.util.concurrent.atomic.AtomicLong(0)
    private val lastRunIdByThread = mutableMapOf<String, String>()

    /** Opens a thread, or a new conversation when [threadId] is null. */
    fun loadThread(threadId: String?) {
        streamGeneration.incrementAndGet()  // invalidate any in-flight SSE callbacks
        eventSource?.cancel()
        eventSource = null
        _threadId.value = threadId
        _threadTitle.value = null
        _messages.value = emptyList()  // clear stale messages immediately
        _chatState.value = ChatState()
        _error.value = null
        if (threadId == null) return
        loadMessages(threadId)
        loadThreadTitle(threadId)
        reconnectActiveRun(threadId)
    }
    /**
     * Reconnect to the most recent run for this thread.
     *
     * 1. If inbox shows an active run for this thread, stream its events.
     * 2. Else if we have a lastRunId from a prior sendMessage, stream that run's
     *    events — it may still be running or just finished; follow=true will pick
     *    up terminal events and our onEvent folds the reply into messages.
     * 3. Else reload messages once (run finished before we left).
     *
     * The SSE stream with follow=true handles both "still running" (deltas arrive
     * live) and "just finished" (terminal event arrives, foldReplyIntoMessages
     * persists it). If the run already finished long ago, the terminal event is
     * still in the backlog and will be delivered immediately.
     */
    private fun reconnectActiveRun(threadId: String) {
        val profile = getConnectionProfile() ?: return
        viewModelScope.launch {
            try {
                val inbox = withContext(Dispatchers.IO) {
                    ApiClient.accessToken = profile.accessToken
                    val clientApi = ClientApi(basePath = profile.serverUrl)
                    clientApi.clientGetInbox()
                }
                if (_threadId.value != threadId) return@launch

                val activeRun = inbox.activeRuns.find { it.threadId == threadId }
                val runIdToReconnect = activeRun?.runId ?: lastRunIdByThread[threadId]

                if (runIdToReconnect != null && eventSource == null) {
                    _chatState.value = ChatState(isStreaming = true, runStatus = RunStatus.Running)
                    streamEvents(profile, runIdToReconnect)
                } else if (activeRun == null) {
                    // No active run and no lastRunId — just reload persisted messages.
                    loadMessages(threadId)
                }
            } catch (e: Exception) {
                // Inbox fetch is best-effort; don't surface as error.
            }
        }
    }

    fun sendMessage(text: String) {
        val profile = getConnectionProfile() ?: run {
            _error.value = "还没有连接服务器"
            return
        }
        val existing = _threadId.value

        // Optimistic user bubble + fresh streaming state.
        val bubble = ChatMessage.User(text)
        _messages.value = _messages.value + bubble
        streamGeneration.incrementAndGet()  // cancel any pending reconnectActiveRun
        eventSource?.cancel()
        eventSource = null
        _chatState.value = ChatState(isStreaming = true, runStatus = RunStatus.Running)
        _error.value = null

        viewModelScope.launch {
            try {
                // A new conversation keeps its thread even when the first run fails, so a retry lands in it.
                val threadId = existing ?: withContext(Dispatchers.IO) {
                    ApiClient.accessToken = profile.accessToken
                    ClientApi(basePath = profile.serverUrl).clientCreateThread(CreateThreadRequest(title = threadTitleFor(text))).id
                }.also {
                    _threadId.value = it
                    _threadTitle.value = threadTitleFor(text)
                }
                val runId = withContext(Dispatchers.IO) {
                    ApiClient.accessToken = profile.accessToken
                    ClientApi(basePath = profile.serverUrl).clientCreateRun(
                        threadId = threadId,
                        createRunRequest = CreateRunRequest(input = text),
                    ).id
                }
                lastRunIdByThread[threadId] = runId
                streamEvents(profile, runId)
            } catch (e: Exception) {
                _messages.value = _messages.value - bubble
                _chatState.value = ChatState()
                _restoredInput.value = text
                _error.value = "没发出去：${userMessage(e)}"
            }
        }
    }

    fun consumeRestoredInput() {
        _restoredInput.value = null
    }

    fun dismissError() {
        _error.value = null
        _chatState.value = _chatState.value.copy(failure = null)
    }

    fun deleteThread(onDeleted: () -> Unit) {
        val profile = getConnectionProfile() ?: return
        val threadId = _threadId.value ?: return onDeleted()
        viewModelScope.launch {
            try {
                withContext(Dispatchers.IO) {
                    ApiClient.accessToken = profile.accessToken
                    ClientApi(basePath = profile.serverUrl).clientDeleteThread(threadId)
                }
                onDeleted()
            } catch (e: Exception) {
                _error.value = userMessage(e)
            }
        }
    }

    /** Asks the server to stop the streaming run; the stream reports run.interrupted when it has. */
    fun interruptRun() {
        val profile = getConnectionProfile() ?: return
        val runId = streamingRunId ?: return
        viewModelScope.launch {
            try {
                withContext(Dispatchers.IO) {
                    ApiClient.accessToken = profile.accessToken
                    ClientApi(basePath = profile.serverUrl).clientInterruptRun(runId)
                }
            } catch (e: Exception) {
                _error.value = "没能停下：${userMessage(e)}"
            }
        }
    }

    private fun loadMessages(threadId: String) {
        val profile = getConnectionProfile() ?: return
        viewModelScope.launch {
            try {
                val response = withContext(Dispatchers.IO) {
                    ApiClient.accessToken = profile.accessToken
                    val clientApi = ClientApi(basePath = profile.serverUrl)
                    clientApi.clientListMessages(threadId, limit = 50)
                }
                if (_threadId.value != threadId) return@launch
                _messages.value = response.items.map { msg -> chatMessageFrom(msg.role, msg.content.text, msg.steps.orEmpty()) }
            } catch (e: Exception) {
                _error.value = userMessage(e)
            }
        }
    }

    private fun loadThreadTitle(threadId: String) {
        val profile = getConnectionProfile() ?: return
        viewModelScope.launch {
            try {
                val title = withContext(Dispatchers.IO) {
                    ApiClient.accessToken = profile.accessToken
                    val clientApi = ClientApi(basePath = profile.serverUrl)
                    clientApi.clientGetThread(threadId).title
                }
                if (_threadId.value != threadId) return@launch
                _threadTitle.value = title
            } catch (e: Exception) {
                // Title is cosmetic; leave the placeholder in place silently.
            }
        }
    }

    private fun streamEvents(profile: ConnectionProfile, runId: String) {
        val gen = streamGeneration.incrementAndGet()
        streamingRunId = runId
        val sseClient = RunEventStreamClient(
            baseUrl = profile.serverUrl,
            accessToken = profile.accessToken,
        )
        eventSource = sseClient.streamRunEvents(
            runId = runId,
            afterSeq = 0,
            onEvent = { packet ->
                if (streamGeneration.get() != gen) return@streamRunEvents
                _chatState.value = projection.apply(_chatState.value, packet)
                // On a terminal event, fold the streamed assistant text into the
                // persisted message list and clear the streaming bubble so the
                // finalized message is the only copy shown.
                if (packet is RunEventPacket.RunCompleted || packet is RunEventPacket.RunFailed) {
                    val finalText = _chatState.value.assistantText
                    val finalReasoning = _chatState.value.assistantReasoning.ifBlank { null }
                    // Only append if the last message is a user bubble — avoids
                    // duplicating the assistant reply when loadMessages already
                    // persisted it before the SSE terminal event arrived.
                    val last = _messages.value.lastOrNull()
                    val lastIsUser = last is ChatMessage.User || last is ChatMessage.Wake || last is ChatMessage.Capture
                    if (finalText.isNotBlank() && lastIsUser) {
                        _messages.value = _messages.value +
                            ChatMessage.Assistant(finalText, finalReasoning, _chatState.value.steps)
                    }
                    _chatState.value = ChatState(runStatus = _chatState.value.runStatus, failure = _chatState.value.failure)
                }
            },
            onError = { t ->
                // Suppress errors from streams we already cancelled (e.g. loadThread).
                if (streamGeneration.get() == gen) {
                    _error.value = "和服务器的连接断了：${t?.let(::userMessage) ?: "连接被关闭"}"
                }
            },
            onClosed = {
                if (streamGeneration.get() == gen) {
                    val current = _chatState.value
                    if (current.isStreaming && current.runStatus == RunStatus.Running) {
                        _chatState.value = current.copy(isStreaming = false)
                    }
                }
            },
        )
    }

    /** The paired server, for loading attachments. */
    fun connection(): ConnectionProfile? = getConnectionProfile()

    private fun getConnectionProfile(): ConnectionProfile? =
        (authController.authState.value as? AuthState.Connected)?.profile

    override fun onCleared() {
        streamGeneration.incrementAndGet()
        eventSource?.cancel()
        super.onCleared()
    }
}

/** A new thread is titled by the first line of its first message. */
fun threadTitleFor(text: String): String {
    val line = text.lineSequence().map { it.trim() }.firstOrNull { it.isNotEmpty() }.orEmpty()
    return if (line.length > 30) line.take(30) + "…" else line
}
