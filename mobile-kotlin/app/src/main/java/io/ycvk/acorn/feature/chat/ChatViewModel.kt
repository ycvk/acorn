package io.ycvk.acorn.feature.chat

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.apis.ClientApi
import io.ycvk.acorn.api.infrastructure.ApiClient
import io.ycvk.acorn.api.models.CreateRunRequest
import io.ycvk.acorn.api.models.CreateThreadRequest
import io.ycvk.acorn.api.models.InboxResponse
import io.ycvk.acorn.api.models.PendingActionSummary
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
import io.ycvk.acorn.data.repository.ThreadFileRepository
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import android.net.Uri
import okhttp3.sse.EventSource
import java.io.File
import java.io.IOException
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
    private val files: ThreadFileRepository,
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

    /** Files picked for the next message. */
    private val _attachments = MutableStateFlow<List<PendingAttachment>>(emptyList())
    val attachments: StateFlow<List<PendingAttachment>> = _attachments.asStateFlow()

    /** What the composer is sending while files upload, such as 上传 1/2. */
    private val _uploading = MutableStateFlow<String?>(null)
    val uploading: StateFlow<String?> = _uploading.asStateFlow()

    /** Ids of the files being downloaded to open or save. */
    private val _fileBusy = MutableStateFlow<Set<String>>(emptySet())
    val fileBusy: StateFlow<Set<String>> = _fileBusy.asStateFlow()

    private var eventSource: EventSource? = null
    private var streamingRunId: String? = null
    private val streamGeneration = java.util.concurrent.atomic.AtomicLong(0)

    /** Reconnects since the stream last delivered an event. */
    private var reconnects = 0

    /** Opens a thread, or a new conversation when [threadId] is null. */
    fun loadThread(threadId: String?) {
        streamGeneration.incrementAndGet()  // invalidate any in-flight SSE callbacks
        eventSource?.cancel()
        eventSource = null
        _threadId.value = threadId
        _threadTitle.value = null
        _messages.value = emptyList()  // clear stale messages immediately
        _attachments.value = emptyList()
        _chatState.value = ChatState()
        _error.value = null
        reconnects = 0
        if (threadId == null) return
        loadMessages(threadId)
        loadThreadTitle(threadId)
        reconnectActiveRun(threadId)
    }

    /**
     * Follows the thread's run when the server still has one going or waiting
     * on the owner; the stream replays it from the start. Otherwise the thread
     * is idle and its persisted messages, loaded again here, hold the reply of a
     * run that finished meanwhile. A failed check is retried, since the system
     * may not have given the app its network back yet.
     */
    private fun reconnectActiveRun(threadId: String) {
        val profile = getConnectionProfile() ?: return
        val gen = streamGeneration.get()
        viewModelScope.launch {
            val inbox = fetchInbox(profile, gen) ?: return@launch
            if (_threadId.value != threadId || streamGeneration.get() != gen) return@launch
            val runId = runToFollow(inbox.activeRuns, inbox.pendingActions, threadId)
            if (runId != null) {
                _chatState.value = ChatState(isStreaming = true, runStatus = RunStatus.Running)
                streamEvents(profile, runId, threadId)
            } else {
                _chatState.value = ChatState()
                loadMessages(threadId)
            }
        }
    }

    /** The inbox while [gen] is current; null once superseded or failed, the failure reported. */
    private suspend fun fetchInbox(profile: ConnectionProfile, gen: Long): InboxResponse? = try {
        withRetries({ streamGeneration.get() == gen }) {
            withContext(Dispatchers.IO) {
                ApiClient.accessToken = profile.accessToken
                ClientApi(basePath = profile.serverUrl).clientGetInbox()
            }
        }
    } catch (e: Exception) {
        if (streamGeneration.get() == gen) connectionLost(e)
        null
    }

    /**
     * Runs [call], again after a pause when the network fails, since the
     * system may not have given the app its network back yet. Null once
     * [current] no longer holds; other failures and the last network failure
     * are thrown.
     */
    private suspend fun <T> withRetries(current: () -> Boolean, call: suspend () -> T): T? {
        var attempt = 0
        while (true) {
            try {
                return call()
            } catch (e: IOException) {
                delay(reconnectDelayMs(attempt++) ?: throw e)
                if (!current()) return null
            }
        }
    }

    /** Adds picked files to the next message; a photo is resized for viewing when sent. */
    fun addAttachments(picked: List<PendingAttachment>) {
        val room = MAX_MESSAGE_FILES - _attachments.value.size
        if (picked.size > room) _error.value = "一条消息最多带 $MAX_MESSAGE_FILES 个文件"
        _attachments.value = _attachments.value + picked.take(room.coerceAtLeast(0))
    }

    fun removeAttachment(attachment: PendingAttachment) {
        _attachments.value = _attachments.value.filterNot { it.key == attachment.key }
    }

    /** Downloads a file of the thread and hands the local copy to [onReady]. */
    fun withFile(file: FileView, onReady: (File) -> Unit) {
        val profile = getConnectionProfile() ?: return
        val threadId = _threadId.value ?: return
        val id = file.id ?: return
        if (id in _fileBusy.value) return
        _fileBusy.value = _fileBusy.value + id
        viewModelScope.launch {
            try {
                val local = withContext(Dispatchers.IO) { files.download(profile, threadId, file) }
                onReady(local)
            } catch (e: Exception) {
                _error.value = "没能下载 ${file.name}：${userMessage(e)}"
            } finally {
                _fileBusy.value = _fileBusy.value - id
            }
        }
    }

    /** Saves a file of the thread where the owner chose. */
    fun saveFile(file: FileView, destination: Uri, onSaved: () -> Unit) {
        withFile(file) { local ->
            viewModelScope.launch {
                try {
                    withContext(Dispatchers.IO) { files.saveTo(local, destination) }
                    onSaved()
                } catch (e: Exception) {
                    _error.value = "没能保存 ${file.name}：${userMessage(e)}"
                }
            }
        }
    }

    fun reportError(message: String) {
        _error.value = message
    }

    fun sendMessage(text: String) {
        val profile = getConnectionProfile() ?: run {
            _error.value = "还没有连接服务器"
            return
        }
        val existing = _threadId.value
        val attachments = _attachments.value
        if (text.isBlank() && attachments.isEmpty()) return
        _attachments.value = emptyList()

        // Optimistic user bubble + fresh streaming state.
        val bubble = ChatMessage.User(text, attachments.map { it.view() })
        _messages.value = _messages.value + bubble
        streamGeneration.incrementAndGet()  // cancel any pending reconnectActiveRun
        eventSource?.cancel()
        eventSource = null
        _chatState.value = ChatState(isStreaming = true, runStatus = RunStatus.Running)
        _error.value = null

        viewModelScope.launch {
            try {
                // A new conversation keeps its thread even when the first run fails, so a retry lands in it.
                val title = threadTitleFor(text.ifBlank { attachments.firstOrNull { !it.photo }?.name ?: "图片" })
                val threadId = existing ?: withContext(Dispatchers.IO) {
                    ApiClient.accessToken = profile.accessToken
                    ClientApi(basePath = profile.serverUrl).clientCreateThread(CreateThreadRequest(title = title)).id
                }.also {
                    _threadId.value = it
                    _threadTitle.value = title
                }
                val fileIds = attachments.mapIndexed { index, attachment ->
                    _uploading.value = if (attachments.size == 1) "正在上传 ${attachment.name}" else "正在上传 ${index + 1}/${attachments.size}：${attachment.name}"
                    withContext(Dispatchers.IO) { files.upload(profile, threadId, attachment).id }
                }
                _uploading.value = null
                val runId = withContext(Dispatchers.IO) {
                    ApiClient.accessToken = profile.accessToken
                    ClientApi(basePath = profile.serverUrl).clientCreateRun(
                        threadId = threadId,
                        createRunRequest = CreateRunRequest(input = text.ifBlank { null }, fileIds = fileIds.ifEmpty { null }),
                    ).id
                }
                reconnects = 0
                streamEvents(profile, runId, threadId)
            } catch (e: Exception) {
                _uploading.value = null
                _messages.value = _messages.value - bubble
                _chatState.value = ChatState()
                _restoredInput.value = text
                _attachments.value = attachments + _attachments.value
                _error.value = "没发出去：${userMessage(e)}"
            }
        }
    }

    /**
     * Brings the thread up to date after the app was in the background: the
     * stream may have been cut and the run may have moved on. A run still
     * going is streamed again from its start.
     */
    fun refresh() {
        val threadId = _threadId.value ?: return
        streamGeneration.incrementAndGet()
        eventSource?.cancel()
        eventSource = null
        _chatState.value = ChatState()
        _error.value = null
        reconnects = 0
        loadMessages(threadId)
        reconnectActiveRun(threadId)
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
                val response = withRetries({ _threadId.value == threadId }) {
                    withContext(Dispatchers.IO) {
                        ApiClient.accessToken = profile.accessToken
                        ClientApi(basePath = profile.serverUrl).clientListMessages(threadId, limit = 50)
                    }
                } ?: return@launch
                if (_threadId.value != threadId) return@launch
                _messages.value = response.items.map { msg -> chatMessageFrom(msg.role, msg.content.text, msg.steps.orEmpty(), msg.files.orEmpty(), msg.views.orEmpty()) }
            } catch (e: Exception) {
                if (_threadId.value == threadId) _error.value = userMessage(e)
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

    private fun streamEvents(profile: ConnectionProfile, runId: String, threadId: String) {
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
                reconnects = 0
                _chatState.value = projection.apply(_chatState.value, packet)
                // On a terminal event, fold the streamed assistant text into the
                // persisted message list and clear the streaming bubble so the
                // finalized message is the only copy shown.
                if (packet is RunEventPacket.RunCompleted || packet is RunEventPacket.RunFailed) {
                    val finalText = _chatState.value.assistantText
                    val finalReasoning = _chatState.value.assistantReasoning.ifBlank { null }
                    if (finalText.isNotBlank() && awaitsReply(_messages.value)) {
                        _messages.value = _messages.value +
                            ChatMessage.Assistant(finalText, finalReasoning, _chatState.value.steps, views = _chatState.value.views)
                    }
                    _chatState.value = ChatState(runStatus = _chatState.value.runStatus, failure = _chatState.value.failure)
                    // The stored messages carry what the stream does not: the files sent with them.
                    loadMessages(threadId)
                }
            },
            // Streams we already cancelled (e.g. loadThread) report nothing.
            onError = { t -> if (streamGeneration.get() == gen) streamLost(threadId, t) },
            onClosed = {
                if (streamGeneration.get() == gen && !_chatState.value.runStatus.isFinal()) {
                    streamLost(threadId, IOException("the server closed the stream before the run ended"))
                }
            },
        )
    }

    /**
     * The stream ended before its run did. After a pause the thread is checked
     * again and followed if the run is still going; past the retries the owner
     * is told, and the header no longer claims a run it cannot see.
     */
    private fun streamLost(threadId: String, cause: Throwable) {
        eventSource = null
        val wait = reconnectDelayMs(reconnects++)
        if (wait == null) {
            connectionLost(cause)
            return
        }
        _chatState.value = _chatState.value.copy(isStreaming = false)
        val gen = streamGeneration.incrementAndGet()
        viewModelScope.launch {
            delay(wait)
            if (streamGeneration.get() == gen && _threadId.value == threadId) reconnectActiveRun(threadId)
        }
    }

    private fun connectionLost(cause: Throwable) {
        _chatState.value = _chatState.value.copy(isStreaming = false, runStatus = RunStatus.Idle)
        _error.value = "和服务器的连接断了：${userMessage(cause)}"
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

/** The most files one message sends; the server takes no more. */
const val MAX_MESSAGE_FILES = 10

/** The run of [threadId] worth following: one still going, or one paused on the owner. */
fun runToFollow(activeRuns: List<RunSummary>, pendingActions: List<PendingActionSummary>, threadId: String): String? =
    activeRuns.find { it.threadId == threadId }?.runId
        ?: pendingActions.find { it.threadId == threadId }?.runId

/**
 * Whether a finished run's streamed reply still belongs at the end of
 * [messages]: the last one is the run's input, or the files and views the run
 * handed over before it replied. A listed reply was persisted by loadMessages before the
 * stream's terminal event arrived.
 */
fun awaitsReply(messages: List<ChatMessage>): Boolean = when (val last = messages.lastOrNull()) {
    null -> false
    is ChatMessage.Assistant -> last.text.isBlank()
    else -> true
}

/** How long to wait before reconnect [attempt] (from 0), or null once the retries are spent. */
fun reconnectDelayMs(attempt: Int): Long? = if (attempt < 3) 1_000L shl attempt else null

private fun RunStatus.isFinal() = this == RunStatus.Completed || this == RunStatus.Failed

/** A new thread is titled by the first line of its first message. */
fun threadTitleFor(text: String): String {
    val line = text.lineSequence().map { it.trim() }.firstOrNull { it.isNotEmpty() }.orEmpty()
    return if (line.length > 30) line.take(30) + "…" else line
}
