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
import io.ycvk.acorn.api.models.SendViewActionRequest
import io.ycvk.acorn.api.models.View
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.sse.ChatState
import io.ycvk.acorn.core.sse.RunEventPacket
import io.ycvk.acorn.core.sse.RunStatus
import io.ycvk.acorn.core.ui.userMessage
import io.ycvk.acorn.data.repository.ThreadFileRepository
import io.ycvk.acorn.feature.live.FollowedRun
import io.ycvk.acorn.feature.live.RunFollower
import io.ycvk.acorn.feature.live.RunUpdate
import io.ycvk.acorn.feature.views.ViewActionPress
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import android.net.Uri
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
 *   the run reaches a terminal event. The thread's run is read from [RunFollower],
 *   which keeps following runs started here after the screen is gone.
 */
@HiltViewModel
class ChatViewModel @Inject constructor(
    private val follower: RunFollower,
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

    /** The [ViewActionPress.id] of the view action whose request is out. */
    private val _sendingAction = MutableStateFlow<String?>(null)
    val sendingAction: StateFlow<String?> = _sendingAction.asStateFlow()

    /** The id of the view being pinned or unpinned. */
    private val _pinning = MutableStateFlow<String?>(null)
    val pinning: StateFlow<String?> = _pinning.asStateFlow()

    private var streamingRunId: String? = null

    /** Bumped when the thread changes or reloads, so an older check of the inbox is dropped. */
    private val loadGeneration = java.util.concurrent.atomic.AtomicLong(0)

    init {
        viewModelScope.launch { follower.updates.collect(::onRunUpdate) }
    }

    /** Opens a thread, or a new conversation when [threadId] is null. */
    fun loadThread(threadId: String?) {
        loadGeneration.incrementAndGet()
        _threadId.value?.let(follower::unwatch)
        _threadId.value = threadId
        _threadTitle.value = null
        _messages.value = emptyList()  // clear stale messages immediately
        _attachments.value = emptyList()
        _chatState.value = ChatState()
        _error.value = null
        streamingRunId = null
        if (threadId == null) return
        follower.watch(threadId)
        loadMessages(threadId)
        loadThreadTitle(threadId)
        val followed = followedRun(threadId)
        if (followed != null) {
            streamingRunId = followed.runId
            _chatState.value = followed.state
        } else {
            reconnectActiveRun(threadId)
        }
    }

    private fun followedRun(threadId: String): FollowedRun? = follower.runs.value.values.firstOrNull { it.threadId == threadId }

    /**
     * Follows the thread's run when the server still has one going or waiting
     * on the owner; the stream replays it from the start. Otherwise the thread
     * is idle and its persisted messages, loaded again here, hold the reply of a
     * run that finished meanwhile. A failed check is retried, since the system
     * may not have given the app its network back yet.
     */
    private fun reconnectActiveRun(threadId: String) {
        val profile = getConnectionProfile() ?: return
        val gen = loadGeneration.get()
        viewModelScope.launch {
            val inbox = fetchInbox(profile, gen) ?: return@launch
            if (_threadId.value != threadId || loadGeneration.get() != gen) return@launch
            val runId = runToFollow(inbox.activeRuns, inbox.pendingActions, threadId)
            if (runId != null) {
                _chatState.value = ChatState(isStreaming = true, runStatus = RunStatus.Running)
                streamingRunId = runId
                follower.follow(profile, runId, threadId, _threadTitle.value, live = false)
            } else {
                _chatState.value = ChatState()
                loadMessages(threadId)
            }
        }
    }

    /** The inbox while [gen] is current; null once superseded or failed, the failure reported. */
    private suspend fun fetchInbox(profile: ConnectionProfile, gen: Long): InboxResponse? = try {
        withRetries({ loadGeneration.get() == gen }) {
            withContext(Dispatchers.IO) {
                ApiClient.accessToken = profile.accessToken
                ClientApi(basePath = profile.serverUrl).clientGetInbox()
            }
        }
    } catch (e: Exception) {
        if (loadGeneration.get() == gen) connectionLost(e)
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
        loadGeneration.incrementAndGet()  // cancel any pending reconnectActiveRun
        _chatState.value = ChatState(isStreaming = true, runStatus = RunStatus.Running)
        _error.value = null
        val title = threadTitleFor(text.ifBlank { attachments.firstOrNull { !it.photo }?.name ?: "图片" })
        // Shown as sending now, while the app is in front and its Live Update may start.
        val sendKey = follower.beginSend(_threadTitle.value ?: title)

        viewModelScope.launch {
            try {
                // A new conversation keeps its thread even when the first run fails, so a retry lands in it.
                val threadId = existing ?: withContext(Dispatchers.IO) {
                    ApiClient.accessToken = profile.accessToken
                    ClientApi(basePath = profile.serverUrl).clientCreateThread(CreateThreadRequest(title = title)).id
                }.also {
                    _threadId.value = it
                    _threadTitle.value = title
                    follower.watch(it)
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
                streamingRunId = runId
                follower.follow(profile, runId, threadId, _threadTitle.value ?: title, live = true, sendKey = sendKey)
            } catch (e: Exception) {
                follower.endSend(sendKey)
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
     * Sends what the owner pressed or chose in a view. The server records it
     * as their message and starts a run on it; the view comes back with the
     * action marked as sent. A refused action reloads the thread, so a view
     * sent from elsewhere shows it.
     */
    fun sendViewAction(press: ViewActionPress) {
        val profile = getConnectionProfile() ?: run {
            _error.value = "还没有连接服务器"
            return
        }
        val threadId = _threadId.value ?: return
        if (_sendingAction.value != null || _chatState.value.isStreaming) return
        _sendingAction.value = press.id
        _error.value = null
        val sendKey = follower.beginSend(_threadTitle.value ?: "Acorn")
        viewModelScope.launch {
            try {
                val sent = withContext(Dispatchers.IO) {
                    ApiClient.accessToken = profile.accessToken
                    ClientApi(basePath = profile.serverUrl).clientSendViewAction(
                        threadId = threadId,
                        viewId = press.viewId,
                        sendViewActionRequest = SendViewActionRequest(
                            componentId = press.componentId,
                            scope = press.scope.ifEmpty { null },
                            selected = press.selected.ifEmpty { null },
                        ),
                    )
                }
                follower.follow(profile, sent.run.id, threadId, _threadTitle.value, live = true, sendKey = sendKey)
                if (_threadId.value != threadId) return@launch
                _messages.value = withView(_messages.value, sent.view) + ChatMessage.User(sent.text)
                _chatState.value = ChatState(isStreaming = true, runStatus = RunStatus.Running)
                streamingRunId = sent.run.id
            } catch (e: Exception) {
                follower.endSend(sendKey)
                if (_threadId.value != threadId) return@launch
                _error.value = "没发出去：${userMessage(e)}"
                loadMessages(threadId)
            } finally {
                _sendingAction.value = null
            }
        }
    }

    /** Pins [view] to the now page, or takes it off when it is pinned; the server's view replaces it here. */
    fun togglePin(view: View) {
        val profile = getConnectionProfile() ?: run {
            _error.value = "还没有连接服务器"
            return
        }
        val threadId = _threadId.value ?: return
        if (_pinning.value != null) return
        _pinning.value = view.id
        viewModelScope.launch {
            try {
                val changed = withContext(Dispatchers.IO) {
                    ApiClient.accessToken = profile.accessToken
                    val api = ClientApi(basePath = profile.serverUrl)
                    if (view.pinnedAt != null) api.clientUnpinView(threadId, view.id) else api.clientPinView(threadId, view.id)
                }
                if (_threadId.value != threadId) return@launch
                _messages.value = withView(_messages.value, changed)
                _chatState.value = _chatState.value.copy(views = _chatState.value.views.map { if (it.id == changed.id) changed else it })
            } catch (e: Exception) {
                if (_threadId.value == threadId) _error.value = (if (view.pinnedAt != null) "没能取消固定：" else "没能固定：") + userMessage(e)
            } finally {
                _pinning.value = null
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
        loadGeneration.incrementAndGet()
        _error.value = null
        loadMessages(threadId)
        val followed = followedRun(threadId)
        when {
            // Its Live Update service kept it current in the background.
            followed != null && followed.live && followed.running -> Unit
            followed != null -> follower.restart(followed.runId)
            else -> {
                _chatState.value = ChatState()
                reconnectActiveRun(threadId)
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
        follower.stopping(runId, true)
        viewModelScope.launch {
            try {
                withContext(Dispatchers.IO) {
                    ApiClient.accessToken = profile.accessToken
                    ClientApi(basePath = profile.serverUrl).clientInterruptRun(runId)
                }
            } catch (e: Exception) {
                follower.stopping(runId, false)
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

    /**
     * Reflects the thread's run as the follower reads it. On a terminal event
     * the streamed reply folds into the message list and the streaming bubble
     * clears, so the finalized message is the only copy shown; a run whose
     * stream could not be kept leaves the header no longer claiming it.
     */
    private fun onRunUpdate(update: RunUpdate) {
        val threadId = _threadId.value ?: return
        if (update.run.threadId != threadId) return
        update.lost?.let {
            connectionLost(it)
            return
        }
        streamingRunId = update.run.runId
        val state = update.run.state
        _chatState.value = state
        val packet = update.packet
        if (packet is RunEventPacket.ViewUpdated) _messages.value = withView(_messages.value, packet.event.`data`.view)
        if (update.ended) {
            val finalText = state.assistantText
            if (finalText.isNotBlank() && awaitsReply(_messages.value)) {
                _messages.value = _messages.value +
                    ChatMessage.Assistant(finalText, state.assistantReasoning.ifBlank { null }, state.steps, views = state.views)
            }
            _chatState.value = ChatState(runStatus = state.runStatus, failure = state.failure)
            // The stored messages carry what the stream does not: the files sent with them.
            loadMessages(threadId)
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
        _threadId.value?.let(follower::unwatch)
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

/** A new thread is titled by the first line of its first message. */
fun threadTitleFor(text: String): String {
    val line = text.lineSequence().map { it.trim() }.firstOrNull { it.isNotEmpty() }.orEmpty()
    return if (line.length > 30) line.take(30) + "…" else line
}
