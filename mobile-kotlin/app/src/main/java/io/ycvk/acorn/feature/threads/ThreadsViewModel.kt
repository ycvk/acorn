package io.ycvk.acorn.feature.threads

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.apis.ClientApi
import io.ycvk.acorn.api.infrastructure.ApiClient
import io.ycvk.acorn.api.models.CreateRunRequest
import io.ycvk.acorn.api.models.CreateThreadRequest
import io.ycvk.acorn.api.models.MessageContent
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.sse.ChatState
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
 * Minimal vertical-slice ViewModel for the Threads tab: starts a test run and streams
 * its SSE events through [RunEventProjection] into [ChatState].
 *
 * This proves the pairing → device-auth → SSE streaming path works end to end. Full
 * chat UX (composer, message history, markdown) lands in B3.
 */
@HiltViewModel
class ThreadsViewModel @Inject constructor(
    private val projection: RunEventProjection,
    private val authController: AuthController,
) : ViewModel() {

    private val _chatState = MutableStateFlow(ChatState())
    val chatState: StateFlow<ChatState> = _chatState.asStateFlow()

    private val _error = MutableStateFlow<String?>(null)
    val error: StateFlow<String?> = _error.asStateFlow()

    private var eventSource: EventSource? = null

    /**
     * Creates a thread (if needed) + run with the given prompt, then streams events.
     */
    fun startTestRun(prompt: String) {
        val profile = (authController.authState.value as? AuthState.Connected)?.profile
        if (profile == null) {
            _error.value = "Not paired"
            return
        }

        // Cancel any in-flight stream before starting a new one.
        eventSource?.cancel()

        _error.value = null
        _chatState.value = ChatState()

        viewModelScope.launch {
            try {
                val runId = withContext(Dispatchers.IO) {
                    createRun(profile, prompt)
                }
                streamEvents(profile, runId)
            } catch (e: Exception) {
                _error.value = e.message ?: "Run failed to start"
            }
        }
    }

    fun stop() {
        eventSource?.cancel()
        eventSource = null
    }

    override fun onCleared() {
        eventSource?.cancel()
        super.onCleared()
    }

    /**
     * Creates a thread and a run carrying [prompt] as inline input, returning the run ID.
     */
    private fun createRun(profile: ConnectionProfile, prompt: String): String {
        // The generated ApiClient reads the companion-level accessToken for Bearer auth.
        ApiClient.accessToken = profile.accessToken

        val clientApi = ClientApi(basePath = profile.serverUrl)
        val thread = clientApi.clientCreateThread(CreateThreadRequest(title = "Mobile test"))
        val run = clientApi.clientCreateRun(
            threadId = thread.id,
            createRunRequest = CreateRunRequest(
                input = prompt,
            ),
        )
        return run.id
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
                // onEvent fires on OkHttp's event-source thread; StateFlow is thread-safe.
                _chatState.value = projection.apply(_chatState.value, packet)
            },
            onError = { t ->
                _error.value = t.message ?: "SSE error"
            },
            onClosed = {
                // If the server closed without a terminal event, mark streaming stopped.
                val current = _chatState.value
                if (current.isStreaming && current.runStatus == RunStatus.Running) {
                    _chatState.value = current.copy(isStreaming = false)
                }
            },
        )
    }

    /**
     * Helper retained for future message-based flows (B3). Builds the CreateMessage
     * payload; not used by the test run which uses inline `input`.
     */
    @Suppress("unused")
    private fun messageContent(text: String): MessageContent =
        MessageContent(type = MessageContent.Type.text, text = text)
}
