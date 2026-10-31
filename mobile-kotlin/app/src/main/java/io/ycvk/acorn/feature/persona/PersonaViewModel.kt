package io.ycvk.acorn.feature.persona

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.infrastructure.ClientException
import io.ycvk.acorn.api.models.Persona
import io.ycvk.acorn.api.models.PersonaRevisionSummary
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.ui.Load
import io.ycvk.acorn.core.ui.userMessage
import io.ycvk.acorn.data.repository.PersonaRepository
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import javax.inject.Inject

/** The persona page: the current persona and its revisions. */
data class PersonaHistory(val current: Persona, val revisions: List<PersonaRevisionSummary>)

/**
 * The persona being edited, on top of [baseRevision]. [mine] is what the owner
 * had written when the persona changed elsewhere; [text] then starts over from
 * the newer revision.
 */
data class PersonaDraft(val text: String, val baseRevision: Long, val mine: String? = null)

sealed interface PersonaSave {
    data object Idle : PersonaSave
    data object Saving : PersonaSave
    data class Failed(val message: String) : PersonaSave
}

/** Backs the persona page and its revisions. */
@HiltViewModel
class PersonaViewModel @Inject constructor(
    private val authController: AuthController,
    private val repository: PersonaRepository,
) : ViewModel() {

    private val _persona = MutableStateFlow<Load<PersonaHistory>>(Load.Loading)
    val persona: StateFlow<Load<PersonaHistory>> = _persona.asStateFlow()

    private val _revision = MutableStateFlow<Load<Persona>>(Load.Loading)
    val revision: StateFlow<Load<Persona>> = _revision.asStateFlow()

    private val _draft = MutableStateFlow<PersonaDraft?>(null)
    val draft: StateFlow<PersonaDraft?> = _draft.asStateFlow()

    private val _save = MutableStateFlow<PersonaSave>(PersonaSave.Idle)
    val save: StateFlow<PersonaSave> = _save.asStateFlow()

    private var loadJob: Job? = null
    private var revisionJob: Job? = null

    /** The paired device, to tell the owner's edits from this phone apart from others. */
    val deviceId: String? get() = profile()?.deviceId

    fun load() {
        val profile = profile() ?: run {
            _persona.value = Load.Failed("还没有连接服务器")
            return
        }
        if (_persona.value !is Load.Loaded) _persona.value = Load.Loading
        loadJob?.cancel()
        loadJob = viewModelScope.launch {
            _persona.value = try {
                withContext(Dispatchers.IO) {
                    val current = async { repository.current(profile) }
                    val revisions = async { repository.revisions(profile) }
                    Load.Loaded(PersonaHistory(current.await(), revisions.await()))
                }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                Load.Failed(userMessage(e))
            }
        }
    }

    fun openRevision(revision: Long) {
        val profile = profile() ?: run {
            _revision.value = Load.Failed("还没有连接服务器")
            return
        }
        _revision.value = Load.Loading
        _save.value = PersonaSave.Idle
        revisionJob?.cancel()
        revisionJob = viewModelScope.launch {
            _revision.value = try {
                Load.Loaded(withContext(Dispatchers.IO) { repository.revision(profile, revision) })
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                Load.Failed(userMessage(e))
            }
        }
    }

    fun startEditing() {
        val current = (_persona.value as? Load.Loaded<PersonaHistory>)?.value?.current ?: return
        _save.value = PersonaSave.Idle
        _draft.value = PersonaDraft(current.content, current.revision)
    }

    fun onDraftChange(text: String) {
        _draft.value = _draft.value?.copy(text = text)
    }

    /** Puts back what the owner had written, now on top of the newer revision. */
    fun useMine() {
        val draft = _draft.value ?: return
        _draft.value = draft.copy(text = draft.mine ?: return, mine = null)
        _save.value = PersonaSave.Idle
    }

    fun cancelEditing() {
        _draft.value = null
        _save.value = PersonaSave.Idle
    }

    fun saveDraft() {
        val draft = _draft.value ?: return
        submit(draft.text, draft.baseRevision, onSaved = { _draft.value = null }, onChanged = { latest ->
            _draft.value = PersonaDraft(latest.content, latest.revision, mine = draft.text)
            _save.value = PersonaSave.Failed("人格刚在别处改过。上面已换成最新的版本，你写的内容在下面，可以在新版本上重新改")
        })
    }

    /** Saves [revision]'s content as a new revision on top of the current one; [onRestored] runs once it is saved. */
    fun restore(revision: Persona, onRestored: () -> Unit) {
        val current = (_persona.value as? Load.Loaded<PersonaHistory>)?.value?.current ?: return
        submit(revision.content, current.revision, onSaved = { onRestored() }, onChanged = {
            _save.value = PersonaSave.Failed("人格刚在别处改过，已载入最新的版本，确认后再恢复一次")
        })
    }

    fun dismissSaveError() {
        _save.value = PersonaSave.Idle
    }

    private fun submit(content: String, base: Long, onSaved: () -> Unit, onChanged: (Persona) -> Unit) {
        val profile = profile() ?: return
        _save.value = PersonaSave.Saving
        viewModelScope.launch {
            try {
                withContext(Dispatchers.IO) { repository.save(profile, content.trim(), base) }
                _save.value = PersonaSave.Idle
                onSaved()
                load()
            } catch (e: Exception) {
                if (e is ClientException && e.statusCode == 409) {
                    try {
                        val latest = withContext(Dispatchers.IO) { repository.current(profile) }
                        onChanged(latest)
                        load()
                    } catch (reload: Exception) {
                        _save.value = PersonaSave.Failed("人格刚在别处改过，没能载入最新的版本：${userMessage(reload)}")
                    }
                } else {
                    _save.value = PersonaSave.Failed("没能保存：${userMessage(e)}")
                }
            }
        }
    }

    private fun profile() = authController.profile
}
