package io.ycvk.acorn.feature.threads

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.apis.ClientApi
import io.ycvk.acorn.api.infrastructure.ApiClient
import io.ycvk.acorn.api.models.CreateThreadRequest
import io.ycvk.acorn.api.models.Thread
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.core.auth.ConnectionProfile
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import javax.inject.Inject

/**
 * Backs the Threads tab: lists existing threads and creates new ones.
 *
 * The generated [io.ycvk.acorn.api.apis.ClientApi] is synchronous (OkHttp execute),
 * so calls are offloaded to [Dispatchers.IO]. Auth is via the companion-level
 * [ApiClient.accessToken] set immediately before each call.
 */
@HiltViewModel
class ThreadsViewModel @Inject constructor(
    private val authController: AuthController,
) : ViewModel() {

    private val _threads = MutableStateFlow<List<Thread>>(emptyList())
    val threads: StateFlow<List<Thread>> = _threads.asStateFlow()

    private val _load = MutableStateFlow<ThreadsLoad>(ThreadsLoad.Loading)
    val load: StateFlow<ThreadsLoad> = _load.asStateFlow()

    /** Failures of create and delete; list load failures live in [load]. */
    private val _error = MutableStateFlow<String?>(null)
    val error: StateFlow<String?> = _error.asStateFlow()

    fun loadThreads() {
        val profile = getConnectionProfile()
        if (profile == null) {
            _load.value = ThreadsLoad.Failed("Not connected to a server")
            return
        }
        _load.value = ThreadsLoad.Loading
        viewModelScope.launch {
            try {
                val response = withContext(Dispatchers.IO) {
                    ApiClient.accessToken = profile.accessToken
                    val clientApi = ClientApi(basePath = profile.serverUrl)
                    clientApi.clientListThreads(limit = 50)
                }
                _threads.value = response.items
                _load.value = ThreadsLoad.Loaded
            } catch (e: Exception) {
                _load.value = ThreadsLoad.Failed(e.message ?: "Failed to load threads")
            }
        }
    }

    /**
     * Creates a new thread and invokes [onCreated] with its id so the caller can
     * navigate into the chat. The created thread is prepended to the list so it
     * appears immediately without a refetch.
     */
    fun createNewThread(onCreated: (String) -> Unit) {
        val profile = getConnectionProfile() ?: return
        viewModelScope.launch {
            try {
                val thread = withContext(Dispatchers.IO) {
                    ApiClient.accessToken = profile.accessToken
                    val clientApi = ClientApi(basePath = profile.serverUrl)
                    clientApi.clientCreateThread(CreateThreadRequest(title = "New Thread"))
                }
                _threads.value = listOf(thread) + _threads.value
                onCreated(thread.id)
            } catch (e: Exception) {
                _error.value = e.message ?: "Failed to create thread"
            }
        }
    }
    fun deleteThread(threadId: String) {
        val profile = getConnectionProfile() ?: return
        viewModelScope.launch {
            try {
                withContext(Dispatchers.IO) {
                    ApiClient.accessToken = profile.accessToken
                    ClientApi(basePath = profile.serverUrl).clientDeleteThread(threadId)
                }
                _threads.value = _threads.value.filterNot { it.id == threadId }
            } catch (e: Exception) {
                _error.value = e.message ?: "Failed to delete thread"
            }
        }
    }

    private fun getConnectionProfile(): ConnectionProfile? =
        (authController.authState.value as? AuthState.Connected)?.profile
}

sealed interface ThreadsLoad {
    data object Loading : ThreadsLoad
    data object Loaded : ThreadsLoad
    data class Failed(val message: String) : ThreadsLoad
}

/** What the thread list area shows. */
sealed interface ThreadsContent {
    data object Loading : ThreadsContent
    data class Failed(val message: String) : ThreadsContent
    data object Empty : ThreadsContent
    data object Items : ThreadsContent
}

/**
 * Threads already on screen stay visible while a reload runs or fails; the
 * empty state shows only after a successful load returned nothing.
 */
fun threadsContent(load: ThreadsLoad, threadCount: Int): ThreadsContent = when {
    threadCount > 0 -> ThreadsContent.Items
    load is ThreadsLoad.Loading -> ThreadsContent.Loading
    load is ThreadsLoad.Failed -> ThreadsContent.Failed(load.message)
    else -> ThreadsContent.Empty
}
