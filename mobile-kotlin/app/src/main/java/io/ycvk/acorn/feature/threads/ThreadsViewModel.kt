package io.ycvk.acorn.feature.threads

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.apis.ClientApi
import io.ycvk.acorn.api.infrastructure.ApiClient
import io.ycvk.acorn.api.models.Thread
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.ui.userMessage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import javax.inject.Inject

/**
 * Backs the conversations tab: lists threads and deletes them.
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

    private val _refreshing = MutableStateFlow(false)
    val refreshing: StateFlow<Boolean> = _refreshing.asStateFlow()

    /** Delete failures; list load failures live in [load]. */
    private val _error = MutableStateFlow<String?>(null)
    val error: StateFlow<String?> = _error.asStateFlow()

    fun loadThreads() {
        val profile = getConnectionProfile()
        if (profile == null) {
            _load.value = ThreadsLoad.Failed("还没有连接服务器")
            return
        }
        if (_load.value !is ThreadsLoad.Loaded) _load.value = ThreadsLoad.Loading
        _refreshing.value = true
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
                _load.value = ThreadsLoad.Failed(userMessage(e))
            } finally {
                _refreshing.value = false
            }
        }
    }

    fun dismissError() {
        _error.value = null
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
                _error.value = userMessage(e)
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
