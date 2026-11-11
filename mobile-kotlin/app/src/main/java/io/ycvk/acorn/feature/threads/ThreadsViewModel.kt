package io.ycvk.acorn.feature.threads

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.apis.ClientApi
import io.ycvk.acorn.api.models.Thread
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.authorized
import io.ycvk.acorn.core.ui.Load
import io.ycvk.acorn.core.ui.userMessage
import io.ycvk.acorn.data.repository.ThreadSnapshots
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
 * so calls are offloaded to [Dispatchers.IO].
 */
@HiltViewModel
class ThreadsViewModel @Inject constructor(
    private val authController: AuthController,
    private val snapshots: ThreadSnapshots,
) : ViewModel() {

    private val _threads = MutableStateFlow<List<Thread>>(emptyList())
    val threads: StateFlow<List<Thread>> = _threads.asStateFlow()

    private val _load = MutableStateFlow<Load<Unit>>(Load.Loading)
    val load: StateFlow<Load<Unit>> = _load.asStateFlow()

    private val _refreshing = MutableStateFlow(false)
    val refreshing: StateFlow<Boolean> = _refreshing.asStateFlow()

    /** Delete failures; list load failures live in [load]. */
    private val _error = MutableStateFlow<String?>(null)
    val error: StateFlow<String?> = _error.asStateFlow()

    fun loadThreads() {
        val profile = profile()
        if (profile == null) {
            _load.value = Load.Failed("还没有连接服务器")
            return
        }
        if (_load.value !is Load.Loaded) _load.value = Load.Loading
        _refreshing.value = true
        viewModelScope.launch {
            try {
                val response = withContext(Dispatchers.IO) {
                    val clientApi = ClientApi(basePath = profile.serverUrl).authorized(profile)
                    val listed = clientApi.clientListThreads(limit = LIST_LIMIT)
                    // A full page may leave conversations out; only a whole list says which copies are of ones that are gone.
                    if (listed.items.size < LIST_LIMIT) snapshots.keepOnly(listed.items.map { it.id }.toSet())
                    listed
                }
                _threads.value = response.items
                _load.value = Load.Loaded(Unit)
            } catch (e: Exception) {
                _load.value = Load.Failed(userMessage(e))
            } finally {
                _refreshing.value = false
            }
        }
    }

    fun dismissError() {
        _error.value = null
    }

    fun deleteThread(threadId: String) {
        val profile = profile() ?: return
        viewModelScope.launch {
            try {
                withContext(Dispatchers.IO) {
                    ClientApi(basePath = profile.serverUrl).authorized(profile).clientDeleteThread(threadId)
                    snapshots.forget(threadId)
                }
                _threads.value = _threads.value.filterNot { it.id == threadId }
            } catch (e: Exception) {
                _error.value = userMessage(e)
            }
        }
    }

    private fun profile() = authController.profile

    private companion object {
        const val LIST_LIMIT = 50
    }
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
fun threadsContent(load: Load<Unit>, threadCount: Int): ThreadsContent = when {
    threadCount > 0 -> ThreadsContent.Items
    load is Load.Loading -> ThreadsContent.Loading
    load is Load.Failed -> ThreadsContent.Failed(load.message)
    else -> ThreadsContent.Empty
}
