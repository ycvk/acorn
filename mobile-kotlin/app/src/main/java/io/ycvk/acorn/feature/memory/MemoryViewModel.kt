package io.ycvk.acorn.feature.memory

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.infrastructure.ClientException
import io.ycvk.acorn.api.models.MemoryDetail
import io.ycvk.acorn.api.models.MemorySummary
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.core.ui.userMessage
import io.ycvk.acorn.data.repository.MemoryRepository
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.FlowPreview
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.debounce
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.drop
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import javax.inject.Inject

private const val SEARCH_DEBOUNCE_MS = 300L
private const val PAGE = 50
private const val MAX_LIMIT = 200

sealed interface MemoryListLoad {
    data object Loading : MemoryListLoad
    data class Loaded(val memories: List<MemorySummary>, val hasMore: Boolean) : MemoryListLoad
    data class Failed(val message: String) : MemoryListLoad
}

/** What the memory list area shows. */
sealed interface MemoryListContent {
    data object Loading : MemoryListContent
    data class Failed(val message: String) : MemoryListContent
    data object Empty : MemoryListContent
    data object NoMatches : MemoryListContent
    data class Items(val memories: List<MemorySummary>, val canLoadMore: Boolean) : MemoryListContent
}

fun memoryListContent(load: MemoryListLoad, query: String, kind: MemorySummary.Kind?, limit: Int): MemoryListContent = when (load) {
    MemoryListLoad.Loading -> MemoryListContent.Loading
    is MemoryListLoad.Failed -> MemoryListContent.Failed(load.message)
    is MemoryListLoad.Loaded -> when {
        load.memories.isNotEmpty() -> MemoryListContent.Items(load.memories, load.hasMore && limit < MAX_LIMIT)
        query.isBlank() && kind == null -> MemoryListContent.Empty
        else -> MemoryListContent.NoMatches
    }
}

sealed interface MemoryLoad {
    data object Loading : MemoryLoad
    data class Loaded(val detail: MemoryDetail) : MemoryLoad
    data class Failed(val message: String) : MemoryLoad
}

/** A change the owner is making to the open memory. */
sealed interface MemoryChange {
    data object Idle : MemoryChange
    data object Saving : MemoryChange
    data class Failed(val message: String) : MemoryChange
}

/** Backs the memory list in the knowledge tab and the memory page. */
@OptIn(FlowPreview::class)
@HiltViewModel
class MemoryViewModel @Inject constructor(
    private val authController: AuthController,
    private val repository: MemoryRepository,
) : ViewModel() {

    private val _query = MutableStateFlow("")
    val query: StateFlow<String> = _query.asStateFlow()

    private val _kind = MutableStateFlow<MemorySummary.Kind?>(null)
    val kind: StateFlow<MemorySummary.Kind?> = _kind.asStateFlow()

    private val _limit = MutableStateFlow(PAGE)
    val limit: StateFlow<Int> = _limit.asStateFlow()

    private val _list = MutableStateFlow<MemoryListLoad>(MemoryListLoad.Loading)
    val list: StateFlow<MemoryListLoad> = _list.asStateFlow()

    private val _memory = MutableStateFlow<MemoryLoad>(MemoryLoad.Loading)
    val memory: StateFlow<MemoryLoad> = _memory.asStateFlow()

    private val _change = MutableStateFlow<MemoryChange>(MemoryChange.Idle)
    val change: StateFlow<MemoryChange> = _change.asStateFlow()

    private var listJob: Job? = null
    private var memoryJob: Job? = null

    init {
        viewModelScope.launch {
            _query.drop(1).debounce(SEARCH_DEBOUNCE_MS).distinctUntilChanged().collect {
                _limit.value = PAGE
                loadList(showLoading = true)
            }
        }
    }

    fun onQueryChange(value: String) {
        _query.value = value
    }

    fun selectKind(kind: MemorySummary.Kind?) {
        if (_kind.value == kind) return
        _kind.value = kind
        _limit.value = PAGE
        loadList(showLoading = true)
    }

    fun refresh() {
        loadList(showLoading = _list.value !is MemoryListLoad.Loaded)
    }

    fun loadMore() {
        _limit.value = (_limit.value + PAGE).coerceAtMost(MAX_LIMIT)
        loadList(showLoading = false)
    }

    fun open(id: String) {
        val profile = profile() ?: run {
            _memory.value = MemoryLoad.Failed("还没有连接服务器")
            return
        }
        _memory.value = MemoryLoad.Loading
        _change.value = MemoryChange.Idle
        memoryJob?.cancel()
        memoryJob = viewModelScope.launch {
            _memory.value = try {
                MemoryLoad.Loaded(withContext(Dispatchers.IO) { repository.get(profile, id) })
            } catch (e: Exception) {
                MemoryLoad.Failed(memoryMessage(e))
            }
        }
    }

    /** Forgets the memory; [onDone] runs once the server has excluded it. */
    fun forget(id: String, onDone: () -> Unit) {
        val profile = profile() ?: return
        _change.value = MemoryChange.Saving
        viewModelScope.launch {
            try {
                withContext(Dispatchers.IO) { repository.forget(profile, id) }
                _change.value = MemoryChange.Idle
                loadList(showLoading = false)
                onDone()
            } catch (e: Exception) {
                _change.value = MemoryChange.Failed("没能忘掉：${memoryMessage(e)}")
            }
        }
    }

    /** Replaces a fact with [content]; [onCorrected] receives the new fact's id. */
    fun correct(memory: MemorySummary, content: String, onCorrected: (String) -> Unit) {
        val profile = profile() ?: return
        _change.value = MemoryChange.Saving
        viewModelScope.launch {
            try {
                val corrected = withContext(Dispatchers.IO) { repository.correct(profile, memory.id, memory.revision, content.trim()) }
                _change.value = MemoryChange.Idle
                loadList(showLoading = false)
                onCorrected(corrected.id)
            } catch (e: Exception) {
                if (e is ClientException && e.statusCode == 409) {
                    _change.value = MemoryChange.Failed("这条记忆刚刚变过，已重新载入，请再改一次")
                    open(memory.id)
                } else {
                    _change.value = MemoryChange.Failed("没能保存：${memoryMessage(e)}")
                }
            }
        }
    }

    fun dismissChangeError() {
        _change.value = MemoryChange.Idle
    }

    private fun loadList(showLoading: Boolean) {
        val profile = profile() ?: run {
            _list.value = MemoryListLoad.Failed("还没有连接服务器")
            return
        }
        if (showLoading) _list.value = MemoryListLoad.Loading
        val kind = _kind.value
        val query = _query.value
        val limit = _limit.value
        listJob?.cancel()
        listJob = viewModelScope.launch {
            _list.value = try {
                val page = withContext(Dispatchers.IO) { repository.list(profile, kind, query, limit) }
                MemoryListLoad.Loaded(page.memories, page.hasMore)
            } catch (e: Exception) {
                MemoryListLoad.Failed(userMessage(e))
            }
        }
    }

    private fun profile() = (authController.authState.value as? AuthState.Connected)?.profile
}

private fun memoryMessage(e: Exception): String =
    if (e is ClientException && e.statusCode == 404) "这条记忆已经被忘掉了" else userMessage(e)
