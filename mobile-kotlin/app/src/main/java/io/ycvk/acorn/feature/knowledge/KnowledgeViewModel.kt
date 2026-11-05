package io.ycvk.acorn.feature.knowledge

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.models.KnowledgeNote
import io.ycvk.acorn.api.models.KnowledgeNoteSummary
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.ui.Load
import io.ycvk.acorn.core.ui.loadOf
import io.ycvk.acorn.core.ui.userMessage
import io.ycvk.acorn.data.repository.KnowledgeRepository
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

/** What the knowledge list area shows. */
sealed interface KnowledgeContent {
    data object Loading : KnowledgeContent
    data class Failed(val message: String) : KnowledgeContent
    data object Empty : KnowledgeContent
    data object NoMatches : KnowledgeContent
    data class Items(val notes: List<KnowledgeNoteSummary>) : KnowledgeContent
}

/** The list once the note at [path] is deleted. */
fun withoutNote(load: Load<List<KnowledgeNoteSummary>>, path: String): Load<List<KnowledgeNoteSummary>> = when (load) {
    is Load.Loaded -> Load.Loaded(load.value.filterNot { it.path == path })
    else -> load
}

fun knowledgeContent(load: Load<List<KnowledgeNoteSummary>>, query: String): KnowledgeContent = when (load) {
    Load.Loading -> KnowledgeContent.Loading
    is Load.Failed -> KnowledgeContent.Failed(load.message)
    is Load.Loaded -> when {
        load.value.isNotEmpty() -> KnowledgeContent.Items(load.value)
        query.isBlank() -> KnowledgeContent.Empty
        else -> KnowledgeContent.NoMatches
    }
}

/** Backs the knowledge tab: recent notes, search, and one open note. */
@OptIn(FlowPreview::class)
@HiltViewModel
class KnowledgeViewModel @Inject constructor(
    private val authController: AuthController,
    private val repository: KnowledgeRepository,
) : ViewModel() {

    private val _query = MutableStateFlow("")
    val query: StateFlow<String> = _query.asStateFlow()

    private val _load = MutableStateFlow<Load<List<KnowledgeNoteSummary>>>(Load.Loading)
    val load: StateFlow<Load<List<KnowledgeNoteSummary>>> = _load.asStateFlow()

    private val _note = MutableStateFlow<Load<KnowledgeNote>>(Load.Loading)
    val note: StateFlow<Load<KnowledgeNote>> = _note.asStateFlow()

    private val _deleteError = MutableStateFlow<String?>(null)
    val deleteError: StateFlow<String?> = _deleteError.asStateFlow()

    private var listJob: Job? = null
    private var noteJob: Job? = null

    init {
        // The screen loads on entry through refresh(); typing searches after a pause.
        viewModelScope.launch {
            _query.drop(1).debounce(SEARCH_DEBOUNCE_MS).distinctUntilChanged().collect { load(it) }
        }
    }

    fun onQueryChange(value: String) {
        _query.value = value
    }

    fun refresh() {
        load(_query.value)
    }

    fun openNote(path: String) {
        val profile = profile() ?: run {
            _note.value = Load.Failed("还没有连接服务器")
            return
        }
        _note.value = Load.Loading
        noteJob?.cancel()
        noteJob = viewModelScope.launch {
            _note.value = loadOf { repository.note(profile, path) }
        }
    }

    /** Deletes a note, drops it from the list, then calls [onDeleted]; a failure is kept in [deleteError]. */
    fun deleteNote(path: String, onDeleted: () -> Unit) {
        val profile = profile() ?: run {
            _deleteError.value = "还没有连接服务器"
            return
        }
        viewModelScope.launch {
            try {
                withContext(Dispatchers.IO) { repository.delete(profile, path) }
                _load.value = withoutNote(_load.value, path)
                onDeleted()
            } catch (e: Exception) {
                _deleteError.value = userMessage(e)
            }
        }
    }

    fun clearDeleteError() {
        _deleteError.value = null
    }

    private fun load(query: String) {
        val profile = profile() ?: run {
            _load.value = Load.Failed("还没有连接服务器")
            return
        }
        _load.value = Load.Loading
        listJob?.cancel()
        listJob = viewModelScope.launch {
            _load.value = loadOf { repository.notes(profile, query) }
        }
    }

    private fun profile() = authController.profile
}
