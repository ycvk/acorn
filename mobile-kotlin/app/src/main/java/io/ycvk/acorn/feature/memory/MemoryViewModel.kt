package io.ycvk.acorn.feature.memory

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.infrastructure.ClientException
import io.ycvk.acorn.api.models.MemoryDetail
import io.ycvk.acorn.api.models.MemoryListResponse
import io.ycvk.acorn.api.models.MemorySummary
import io.ycvk.acorn.api.models.Portrait
import io.ycvk.acorn.api.models.PortraitLine
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.ui.Load
import io.ycvk.acorn.core.ui.Save
import io.ycvk.acorn.core.ui.loadOf
import io.ycvk.acorn.core.ui.userMessage
import io.ycvk.acorn.data.repository.MemoryRepository
import io.ycvk.acorn.data.repository.PortraitRepository
import kotlinx.coroutines.CancellationException
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

/** What the memory list area shows. */
sealed interface MemoryListContent {
    data object Loading : MemoryListContent
    data class Failed(val message: String) : MemoryListContent
    data object Empty : MemoryListContent
    data object NoMatches : MemoryListContent
    data class Items(val memories: List<MemorySummary>, val canLoadMore: Boolean) : MemoryListContent
}

fun memoryListContent(load: Load<MemoryListResponse>, query: String, kind: MemorySummary.Kind?, limit: Int): MemoryListContent = when (load) {
    Load.Loading -> MemoryListContent.Loading
    is Load.Failed -> MemoryListContent.Failed(load.message)
    is Load.Loaded -> when {
        load.value.memories.isNotEmpty() -> MemoryListContent.Items(load.value.memories, load.value.hasMore && limit < MAX_LIMIT)
        query.isBlank() && kind == null -> MemoryListContent.Empty
        else -> MemoryListContent.NoMatches
    }
}

/** What the memory list shows of the portrait above the memories. */
sealed interface PortraitSection {
    data object Hidden : PortraitSection
    data class Failed(val message: String) : PortraitSection
    data class Lines(val lines: List<PortraitLine>) : PortraitSection
}

/** The portrait shows on the unfiltered list only, and only once it has lines. */
fun portraitSection(load: Load<Portrait>, query: String, kind: MemorySummary.Kind?): PortraitSection = when {
    query.isNotBlank() || kind != null -> PortraitSection.Hidden
    load is Load.Failed -> PortraitSection.Failed(load.message)
    load is Load.Loaded && load.value.lines.isNotEmpty() -> PortraitSection.Lines(load.value.lines)
    else -> PortraitSection.Hidden
}

/** Backs the memory list in the knowledge tab and the memory page. */
@OptIn(FlowPreview::class)
@HiltViewModel
class MemoryViewModel @Inject constructor(
    private val authController: AuthController,
    private val repository: MemoryRepository,
    private val portraitRepository: PortraitRepository,
) : ViewModel() {

    private val _query = MutableStateFlow("")
    val query: StateFlow<String> = _query.asStateFlow()

    private val _kind = MutableStateFlow<MemorySummary.Kind?>(null)
    val kind: StateFlow<MemorySummary.Kind?> = _kind.asStateFlow()

    private val _limit = MutableStateFlow(PAGE)
    val limit: StateFlow<Int> = _limit.asStateFlow()

    private val _list = MutableStateFlow<Load<MemoryListResponse>>(Load.Loading)
    val list: StateFlow<Load<MemoryListResponse>> = _list.asStateFlow()

    private val _memory = MutableStateFlow<Load<MemoryDetail>>(Load.Loading)
    val memory: StateFlow<Load<MemoryDetail>> = _memory.asStateFlow()

    private val _change = MutableStateFlow<Save>(Save.Idle)
    val change: StateFlow<Save> = _change.asStateFlow()

    private val _portrait = MutableStateFlow<Load<Portrait>>(Load.Loading)
    val portrait: StateFlow<Load<Portrait>> = _portrait.asStateFlow()

    private val _portraitError = MutableStateFlow<String?>(null)
    val portraitError: StateFlow<String?> = _portraitError.asStateFlow()

    private var listJob: Job? = null
    private var memoryJob: Job? = null
    private var portraitJob: Job? = null

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
        loadList(showLoading = _list.value !is Load.Loaded)
        loadPortrait()
    }

    /** Takes a line off the portrait; later refreshes do not write it again. */
    fun removePortraitLine(id: Long) {
        val profile = profile() ?: return
        _portraitError.value = null
        viewModelScope.launch {
            try {
                withContext(Dispatchers.IO) { portraitRepository.removeLine(profile, id) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                _portraitError.value = if (e is ClientException && e.statusCode == 404) {
                    "这一条已经不在画像里了，已重新载入"
                } else {
                    "没能删掉：${userMessage(e)}"
                }
            }
            loadPortrait()
        }
    }

    fun dismissPortraitError() {
        _portraitError.value = null
    }

    fun loadMore() {
        _limit.value = (_limit.value + PAGE).coerceAtMost(MAX_LIMIT)
        loadList(showLoading = false)
    }

    fun open(id: String) {
        val profile = profile() ?: run {
            _memory.value = Load.Failed("还没有连接服务器")
            return
        }
        _memory.value = Load.Loading
        _change.value = Save.Idle
        memoryJob?.cancel()
        memoryJob = viewModelScope.launch {
            _memory.value = try {
                Load.Loaded(withContext(Dispatchers.IO) { repository.get(profile, id) })
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                Load.Failed(memoryMessage(e))
            }
        }
    }

    /** Forgets the memory; [onDone] runs once the server has excluded it. */
    fun forget(id: String, onDone: () -> Unit) {
        val profile = profile() ?: return
        _change.value = Save.Saving
        viewModelScope.launch {
            try {
                withContext(Dispatchers.IO) { repository.forget(profile, id) }
                _change.value = Save.Idle
                loadList(showLoading = false)
                onDone()
            } catch (e: Exception) {
                _change.value = Save.Failed("没能忘掉：${memoryMessage(e)}")
            }
        }
    }

    /** Replaces a fact with [content]; [onCorrected] receives the new fact's id. */
    fun correct(memory: MemorySummary, content: String, onCorrected: (String) -> Unit) {
        val profile = profile() ?: return
        _change.value = Save.Saving
        viewModelScope.launch {
            try {
                val corrected = withContext(Dispatchers.IO) { repository.correct(profile, memory.id, memory.revision, content.trim()) }
                _change.value = Save.Idle
                loadList(showLoading = false)
                onCorrected(corrected.id)
            } catch (e: Exception) {
                if (e is ClientException && e.statusCode == 409) {
                    _change.value = Save.Failed("这条记忆刚刚变过，已重新载入，请再改一次")
                    open(memory.id)
                } else {
                    _change.value = Save.Failed("没能保存：${memoryMessage(e)}")
                }
            }
        }
    }

    fun dismissChangeError() {
        _change.value = Save.Idle
    }

    private fun loadList(showLoading: Boolean) {
        val profile = profile() ?: run {
            _list.value = Load.Failed("还没有连接服务器")
            return
        }
        if (showLoading) _list.value = Load.Loading
        val kind = _kind.value
        val query = _query.value
        val limit = _limit.value
        listJob?.cancel()
        listJob = viewModelScope.launch {
            _list.value = loadOf { repository.list(profile, kind, query, limit) }
        }
    }

    private fun loadPortrait() {
        val profile = profile() ?: run {
            _portrait.value = Load.Failed("还没有连接服务器")
            return
        }
        portraitJob?.cancel()
        portraitJob = viewModelScope.launch {
            _portrait.value = loadOf { portraitRepository.get(profile) }
        }
    }

    private fun profile() = authController.profile
}

private fun memoryMessage(e: Exception): String =
    if (e is ClientException && e.statusCode == 404) "这条记忆已经被忘掉了" else userMessage(e)
