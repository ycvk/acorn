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

    private val _error = MutableStateFlow<String?>(null)
    val error: StateFlow<String?> = _error.asStateFlow()

    fun loadThreads() {
        val profile = getConnectionProfile() ?: return
        viewModelScope.launch {
            try {
                val response = withContext(Dispatchers.IO) {
                    ApiClient.accessToken = profile.accessToken
                    val clientApi = ClientApi(basePath = profile.serverUrl)
                    clientApi.clientListThreads(limit = 50)
                }
                _threads.value = response.items
            } catch (e: Exception) {
                _error.value = e.message ?: "Failed to load threads"
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
