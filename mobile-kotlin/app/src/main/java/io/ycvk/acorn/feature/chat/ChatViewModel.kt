package io.ycvk.acorn.feature.chat

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.apis.ClientApi
import io.ycvk.acorn.api.infrastructure.ApiClient
import io.ycvk.acorn.api.models.CreateRunRequest
import io.ycvk.acorn.api.models.Message
import io.ycvk.acorn.api.models.ReasoningMessagePart
import io.ycvk.acorn.api.models.TextMessagePart
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.sse.ChatState
import io.ycvk.acorn.core.sse.RunEventPacket
import io.ycvk.acorn.core.sse.RunEventProjection
import io.ycvk.acorn.core.sse.RunEventStreamClient
import io.ycvk.acorn.core.sse.RunStatus
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
 *   a run completes.
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

    private val _messages = MutableStateFlow<List<ChatMessage>>(emptyList())
    val messages: StateFlow<List<ChatMessage>> = _messages.asStateFlow()

    private val _chatState = MutableStateFlow(ChatState())
    val chatState: StateFlow<ChatState> = _chatState.asStateFlow()

    private val _error = MutableStateFlow<String?>(null)
    val error: StateFlow<String?> = _error.asStateFlow()

    private var eventSource: EventSource? = null

    fun loadThread(threadId: String) {
        // Switching thread: drop any in-flight stream and reset state.
        eventSource?.cancel()
        eventSource = null
        _threadId.value = threadId
        _chatState.value = ChatState()
        _error.value = null
        loadMessages(threadId)
    }

    fun sendMessage(text: String) {
        val profile = getConnectionProfile() ?: run {
            _error.value = "Not paired"
            return
        }
        val threadId = _threadId.value ?: run {
            _error.value = "No thread loaded"
            return
        }

        // Optimistic user bubble + fresh streaming state.
        _messages.value = _messages.value + ChatMessage.User(text)
        _chatState.value = ChatState()
        _error.value = null

        viewModelScope.launch {
            try {
                val runId = withContext(Dispatchers.IO) {
                    ApiClient.accessToken = profile.accessToken
                    val clientApi = ClientApi(basePath = profile.serverUrl)
                    clientApi.clientCreateRun(
                        threadId = threadId,
                        createRunRequest = CreateRunRequest(input = text),
                    ).id
                }
                streamEvents(profile, runId)
            } catch (e: Exception) {
                _error.value = e.message ?: "Failed to send message"
            }
        }
    }

    fun interruptRun() {
        eventSource?.cancel()
        eventSource = null
        _chatState.value = _chatState.value.copy(
            isStreaming = false,
            runStatus = RunStatus.Interrupted,
        )
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
                _messages.value = response.items.map { msg ->
                    val text = extractText(msg)
                    val reasoning = extractReasoning(msg)
                    when (msg.role) {
                        Message.Role.user -> ChatMessage.User(text)
                        Message.Role.assistant -> ChatMessage.Assistant(text, reasoning)
                        // System / tool messages render as assistant bubbles so the
                        // history reads top-to-bottom without gaps.
                        else -> ChatMessage.Assistant(text, reasoning)
                    }
                }
            } catch (e: Exception) {
                _error.value = e.message ?: "Failed to load messages"
            }
        }
    }

    private fun streamEvents(profile: ConnectionProfile, runId: String) {
        val sseClient = RunEventStreamClient(
            baseUrl = profile.serverUrl,
            accessToken = profile.accessToken,
        )
        eventSource = sseClient.streamRunEvents(
            runId = runId,
            afterSeq = 0,
            onEvent = { packet ->
                _chatState.value = projection.apply(_chatState.value, packet)
                // On a terminal event, fold the streamed assistant text into the
                // persisted message list and clear the streaming bubble.
                if (packet is RunEventPacket.RunCompleted || packet is RunEventPacket.RunFailed) {
                    val finalText = _chatState.value.assistantText
                    val finalReasoning = _chatState.value.assistantReasoning.ifBlank { null }
                    if (finalText.isNotBlank()) {
                        _messages.value = _messages.value +
                            ChatMessage.Assistant(finalText, finalReasoning)
                    }
                }
            },
            onError = { t -> _error.value = t.message ?: "SSE error" },
            onClosed = {
                val current = _chatState.value
                if (current.isStreaming && current.runStatus == RunStatus.Running) {
                    _chatState.value = current.copy(isStreaming = false)
                }
            },
        )
    }

    private fun getConnectionProfile(): ConnectionProfile? =
        (authController.authState.value as? AuthState.Connected)?.profile

    override fun onCleared() {
        eventSource?.cancel()
        super.onCleared()
    }

    private fun extractText(message: Message): String {
        // content.text is always present (non-nullable in the generated model).
        val body = message.content.text
        // Prefer structured parts when present; they carry the authoritative text.
        val parts = message.content.parts
        if (parts.isNullOrEmpty()) return body
        return parts.filterIsInstance<TextMessagePart>().joinToString("") { it.text }
            .ifBlank { body }
    }

    private fun extractReasoning(message: Message): String? {
        val parts = message.content.parts ?: return null
        val reasoning = parts.filterIsInstance<ReasoningMessagePart>()
            .joinToString("\n") { it.reasoning }
        return reasoning.ifBlank { null }
    }
}
