package io.ycvk.acorn.feature.skills

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.infrastructure.ClientException
import io.ycvk.acorn.api.models.SkillDetail
import io.ycvk.acorn.api.models.SkillDiff
import io.ycvk.acorn.api.models.SkillRevision
import io.ycvk.acorn.api.models.SkillRevisionSummary
import io.ycvk.acorn.api.models.SkillSummary
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.ui.Load
import io.ycvk.acorn.core.ui.userMessage
import io.ycvk.acorn.data.repository.SkillRepository
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

/** A skill's page: its detail and its revisions. */
data class SkillHistory(val detail: SkillDetail, val revisions: List<SkillRevisionSummary>)

sealed interface SkillChange {
    data object Idle : SkillChange
    data object Saving : SkillChange
    data class Failed(val message: String) : SkillChange
}

/** Backs the skill list, a skill's page and its revisions. */
@HiltViewModel
class SkillsViewModel @Inject constructor(
    private val authController: AuthController,
    private val repository: SkillRepository,
) : ViewModel() {

    private val _list = MutableStateFlow<Load<List<SkillSummary>>>(Load.Loading)
    val list: StateFlow<Load<List<SkillSummary>>> = _list.asStateFlow()

    private val _skill = MutableStateFlow<Load<SkillHistory>>(Load.Loading)
    val skill: StateFlow<Load<SkillHistory>> = _skill.asStateFlow()

    private val _revision = MutableStateFlow<Load<SkillRevision>>(Load.Loading)
    val revision: StateFlow<Load<SkillRevision>> = _revision.asStateFlow()

    private val _diff = MutableStateFlow<Load<SkillDiff>>(Load.Loading)
    val diff: StateFlow<Load<SkillDiff>> = _diff.asStateFlow()

    private val _change = MutableStateFlow<SkillChange>(SkillChange.Idle)
    val change: StateFlow<SkillChange> = _change.asStateFlow()

    private var listJob: Job? = null
    private var skillJob: Job? = null
    private var revisionJob: Job? = null
    private var diffJob: Job? = null

    fun loadList() {
        val profile = profile() ?: run {
            _list.value = Load.Failed("还没有连接服务器")
            return
        }
        if (_list.value !is Load.Loaded) _list.value = Load.Loading
        listJob?.cancel()
        listJob = viewModelScope.launch {
            _list.value = try {
                Load.Loaded(withContext(Dispatchers.IO) { repository.list(profile) })
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                Load.Failed(userMessage(e))
            }
        }
    }

    /** Loads a skill's page with its history; a page already showing the skill stays while it reloads. */
    fun open(name: String) {
        val profile = profile() ?: run {
            _skill.value = Load.Failed("还没有连接服务器")
            return
        }
        val shown = (_skill.value as? Load.Loaded<SkillHistory>)?.value?.detail?.name
        if (shown != name) {
            _skill.value = Load.Loading
            _change.value = SkillChange.Idle
        }
        skillJob?.cancel()
        skillJob = viewModelScope.launch {
            _skill.value = try {
                withContext(Dispatchers.IO) {
                    val detail = async { repository.detail(profile, name) }
                    val revisions = async { repository.revisions(profile, name) }
                    Load.Loaded(SkillHistory(detail.await(), revisions.await()))
                }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                Load.Failed(userMessage(e))
            }
        }
    }

    fun openRevision(name: String, revision: Long) {
        val profile = profile() ?: run {
            _revision.value = Load.Failed("还没有连接服务器")
            return
        }
        _revision.value = Load.Loading
        _change.value = SkillChange.Idle
        revisionJob?.cancel()
        revisionJob = viewModelScope.launch {
            _revision.value = try {
                Load.Loaded(withContext(Dispatchers.IO) { repository.revision(profile, name, revision) })
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                Load.Failed(userMessage(e))
            }
        }
    }

    /** Loads how the skill changes from revision [from] to revision [to]. */
    fun loadDiff(name: String, from: Long, to: Long) {
        val profile = profile() ?: run {
            _diff.value = Load.Failed("还没有连接服务器")
            return
        }
        val shown = (_diff.value as? Load.Loaded<SkillDiff>)?.value
        if (shown != null && shown.name == name && shown.from == from && shown.to == to) return
        _diff.value = Load.Loading
        diffJob?.cancel()
        diffJob = viewModelScope.launch {
            _diff.value = try {
                Load.Loaded(withContext(Dispatchers.IO) { repository.diff(profile, name, from, to) })
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                Load.Failed(userMessage(e))
            }
        }
    }

    /** Writes [revision]'s content as the owner's new revision on top of the one on the page. */
    fun rollback(revision: SkillRevision, onDone: () -> Unit) {
        val current = loadedDetail() ?: return
        submit(current.name, "回滚", onDone) { profile -> repository.rollback(profile, current.name, revision.revision, current.revision) }
    }

    fun retire(onDone: () -> Unit = {}) {
        val current = loadedDetail() ?: return
        submit(current.name, "停用", onDone) { profile -> repository.retire(profile, current.name, current.revision) }
    }

    fun restore(onDone: () -> Unit = {}) {
        val current = loadedDetail() ?: return
        submit(current.name, "恢复", onDone) { profile -> repository.restore(profile, current.name, current.revision) }
    }

    /** Stores a secret's value; the value is never read back. */
    fun setSecret(secret: String, value: String, onDone: () -> Unit) {
        val current = loadedDetail() ?: return
        submit(current.name, "设置密钥", onDone) { profile -> repository.setSecret(profile, current.name, secret, value) }
    }

    fun clearSecret(secret: String, onDone: () -> Unit = {}) {
        val current = loadedDetail() ?: return
        submit(current.name, "清除密钥", onDone) { profile -> repository.clearSecret(profile, current.name, secret) }
    }

    /** Finds the conversation the run that wrote a revision happened in. */
    fun openRun(runId: String, onThread: (String) -> Unit) {
        val profile = profile() ?: return
        viewModelScope.launch {
            try {
                onThread(withContext(Dispatchers.IO) { repository.threadOfRun(profile, runId) })
            } catch (e: Exception) {
                _change.value = SkillChange.Failed(
                    if (e is ClientException && e.statusCode == 404) "写下这一版的对话已经删除了" else "没能打开那次对话：${userMessage(e)}",
                )
            }
        }
    }

    fun dismissChangeError() {
        _change.value = SkillChange.Idle
    }

    private fun submit(name: String, action: String, onDone: () -> Unit, write: (ConnectionProfile) -> Unit) {
        val profile = profile() ?: return
        _change.value = SkillChange.Saving
        viewModelScope.launch {
            try {
                withContext(Dispatchers.IO) { write(profile) }
                _change.value = SkillChange.Idle
                onDone()
            } catch (e: Exception) {
                _change.value = SkillChange.Failed(
                    if (e is ClientException && e.statusCode == 409) {
                        "技能刚被改过，已载入最新的版本，确认后再${action}一次"
                    } else {
                        "没能$action：${userMessage(e)}"
                    },
                )
            }
            open(name)
            loadList()
        }
    }

    private fun loadedDetail(): SkillDetail? = (_skill.value as? Load.Loaded<SkillHistory>)?.value?.detail

    private fun profile() = authController.profile
}
