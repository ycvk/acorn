package io.ycvk.acorn.feature.knowledge

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.models.KnowledgeNote
import io.ycvk.acorn.api.models.KnowledgeNoteSummary
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.core.auth.ConnectionProfile
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

sealed interface KnowledgeLoad {
    data object Loading : KnowledgeLoad
    data class Loaded(val notes: List<KnowledgeNoteSummary>) : KnowledgeLoad
    data class Failed(val message: String) : KnowledgeLoad
}

sealed interface NoteLoad {
    data object Loading : NoteLoad
    data class Loaded(val note: KnowledgeNote) : NoteLoad
    data class Failed(val message: String) : NoteLoad
}

/** What the knowledge list area shows. */
sealed interface KnowledgeContent {
    data object Loading : KnowledgeContent
    data class Failed(val message: String) : KnowledgeContent
    data object Empty : KnowledgeContent
    data object NoMatches : KnowledgeContent
    data class Items(val notes: List<KnowledgeNoteSummary>) : KnowledgeContent
}

fun knowledgeContent(load: KnowledgeLoad, query: String): KnowledgeContent = when (load) {
    KnowledgeLoad.Loading -> KnowledgeContent.Loading
    is KnowledgeLoad.Failed -> KnowledgeContent.Failed(load.message)
    is KnowledgeLoad.Loaded -> when {
        load.notes.isNotEmpty() -> KnowledgeContent.Items(load.notes)
        query.isBlank() -> KnowledgeContent.Empty
        else -> KnowledgeContent.NoMatches
    }
}

/** Backs the Knowledge tab: recent notes, search, and one open note. */
@OptIn(FlowPreview::class)
@HiltViewModel
class KnowledgeViewModel @Inject constructor(
    private val authController: AuthController,
    private val repository: KnowledgeRepository,
) : ViewModel() {

    private val _query = MutableStateFlow("")
    val query: StateFlow<String> = _query.asStateFlow()

    private val _load = MutableStateFlow<KnowledgeLoad>(KnowledgeLoad.Loading)
    val load: StateFlow<KnowledgeLoad> = _load.asStateFlow()

    private val _note = MutableStateFlow<NoteLoad>(NoteLoad.Loading)
    val note: StateFlow<NoteLoad> = _note.asStateFlow()

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
            _note.value = NoteLoad.Failed("Not connected to a server")
            return
        }
        _note.value = NoteLoad.Loading
        noteJob?.cancel()
        noteJob = viewModelScope.launch {
            _note.value = try {
                NoteLoad.Loaded(withContext(Dispatchers.IO) { repository.note(profile, path) })
            } catch (e: Exception) {
                NoteLoad.Failed(e.message ?: "Failed to load the note")
            }
        }
    }

    private fun load(query: String) {
        val profile = profile() ?: run {
            _load.value = KnowledgeLoad.Failed("Not connected to a server")
            return
        }
        _load.value = KnowledgeLoad.Loading
        listJob?.cancel()
        listJob = viewModelScope.launch {
            _load.value = try {
                KnowledgeLoad.Loaded(withContext(Dispatchers.IO) { repository.notes(profile, query) })
            } catch (e: Exception) {
                KnowledgeLoad.Failed(e.message ?: "Failed to load notes")
            }
        }
    }

    /** The paired server, for loading attachments. */
    fun connection(): ConnectionProfile? = profile()

    private fun profile() = (authController.authState.value as? AuthState.Connected)?.profile
}

private val attachmentEmbed = Regex("""!\[([^\]\n]*)]\((attachments/[^)\s]+)\)""")

/** A note body split around the images it embeds. */
sealed interface NoteSegment {
    data class Markdown(val text: String) : NoteSegment
    data class Image(val path: String, val description: String) : NoteSegment
}

/** Splits a note body at its `![description](attachments/...)` embeds. */
fun noteSegments(body: String): List<NoteSegment> {
    val segments = mutableListOf<NoteSegment>()
    var start = 0
    fun text(end: Int) {
        body.substring(start, end).trim().takeIf { it.isNotEmpty() }?.let { segments += NoteSegment.Markdown(it) }
    }
    for (match in attachmentEmbed.findAll(body)) {
        text(match.range.first)
        segments += NoteSegment.Image(match.groupValues[2], attachmentDescription(match))
        start = match.range.last + 1
    }
    text(body.length)
    return segments
}

/** Shows embeds in plain-text snippets as their description. */
fun snippetText(snippet: String): String =
    attachmentEmbed.replace(snippet) { match -> "[${attachmentDescription(match)}]" }

private fun attachmentDescription(match: MatchResult) =
    match.groupValues[1].trim().ifEmpty { match.groupValues[2].substringAfterLast('/') }
