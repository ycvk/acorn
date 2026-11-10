package io.ycvk.acorn.feature.approvals

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.apis.ClientApi
import io.ycvk.acorn.api.models.DecidePendingActionRequest
import io.ycvk.acorn.api.models.PendingActionDetail
import io.ycvk.acorn.api.models.PendingActionSummary
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.auth.authorized
import io.ycvk.acorn.core.ui.userMessage
import io.ycvk.acorn.feature.live.RunFollower
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import javax.inject.Inject

/**
 * The pending list after a load: what the server says waits, with the actions
 * in [settled] that it no longer lists kept where they were, since their
 * cards are still showing what was decided.
 */
fun withSettled(previous: List<PendingActionSummary>, loaded: List<PendingActionSummary>, settled: Set<String>): List<PendingActionSummary> {
    val result = loaded.toMutableList()
    previous.forEachIndexed { index, action ->
        if (action.actionId in settled && loaded.none { it.actionId == action.actionId }) result.add(index.coerceAtMost(result.size), action)
    }
    return result
}

/**
 * Lists pending actions and lets the owner decide (accept/decline/answer) on
 * one, inline on Today or from the detail sheet. A decided action stays in
 * the list for a moment, its card saying what was decided, and then leaves.
 *
 * The generated [ClientApi] is synchronous (OkHttp execute), so calls are
 * offloaded to [Dispatchers.IO].
 */
@HiltViewModel
class ApprovalsViewModel @Inject constructor(
    private val authController: AuthController,
    private val notifications: ApprovalNotifications,
    private val follower: RunFollower,
) : ViewModel() {

    private val _pendingActions = MutableStateFlow<List<PendingActionSummary>>(emptyList())
    val pendingActions: StateFlow<List<PendingActionSummary>> = _pendingActions.asStateFlow()

    private val _selectedAction = MutableStateFlow<PendingActionDetail?>(null)
    val selectedAction: StateFlow<PendingActionDetail?> = _selectedAction.asStateFlow()

    private val _error = MutableStateFlow<String?>(null)
    val error: StateFlow<String?> = _error.asStateFlow()

    /** The action whose decision is being sent. */
    private val _deciding = MutableStateFlow<String?>(null)
    val deciding: StateFlow<String?> = _deciding.asStateFlow()

    /** The actions just decided here, by what was decided, while their cards show it. */
    private val _settled = MutableStateFlow<Map<String, DecidePendingActionRequest.Decision>>(emptyMap())
    val settled: StateFlow<Map<String, DecidePendingActionRequest.Decision>> = _settled.asStateFlow()

    fun loadPendingActions() {
        val profile = profile() ?: return
        viewModelScope.launch { readPendingActions(profile) }
    }

    private suspend fun readPendingActions(profile: ConnectionProfile) {
        try {
            val response = withContext(Dispatchers.IO) {
                ClientApi(basePath = profile.serverUrl).authorized(profile).clientListPendingActions(limit = 50)
            }
            _pendingActions.value = withSettled(_pendingActions.value, response.items, _settled.value.keys)
            _error.value = null
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            _error.value = userMessage(e)
        }
    }

    fun loadActionDetail(actionId: String) {
        val profile = profile() ?: return
        viewModelScope.launch {
            try {
                val detail = withContext(Dispatchers.IO) {
                    ClientApi(basePath = profile.serverUrl).authorized(profile).clientGetPendingAction(actionId)
                }
                _selectedAction.value = detail
                _error.value = null
            } catch (e: Exception) {
                _error.value = userMessage(e)
            }
        }
    }

    /**
     * Sends [request] as the decision on [actionId]; its shape depends on the
     * action's kind, see [optionRequest]. The run it resumes shows as a Live
     * Update until it ends or waits again. Once the server has taken the
     * decision the action's card says so for a moment, then the list is read
     * again; a decision that failed leaves the list and its error as they are.
     */
    fun decideAction(actionId: String, request: DecidePendingActionRequest) {
        val profile = profile() ?: return
        val run = _pendingActions.value.firstOrNull { it.actionId == actionId }?.let { it.runId to it.threadId }
            ?: _selectedAction.value?.takeIf { it.actionId == actionId }?.let { it.runId to it.threadId }
        _deciding.value = actionId
        _error.value = null
        viewModelScope.launch {
            try {
                withContext(Dispatchers.IO) {
                    ClientApi(basePath = profile.serverUrl).authorized(profile).clientDecidePendingAction(actionId, request)
                }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                _error.value = userMessage(e)
                return@launch
            } finally {
                if (_deciding.value == actionId) _deciding.value = null
            }
            run?.let { (runId, threadId) -> follower.follow(profile, runId, threadId, title = null, live = true, decided = actionId) }
            _selectedAction.value = null
            notifications.cancel(actionId)
            _settled.update { it + (actionId to request.decision) }
            try {
                delay(SETTLED_MS)
            } finally {
                _settled.update { it - actionId }
            }
            readPendingActions(profile)
        }
    }

    fun dismissError() {
        _error.value = null
    }

    fun clearSelection() {
        _selectedAction.value = null
    }

    private fun profile() = authController.profile

    private companion object {
        /** How long a decided action's card says what was decided. */
        const val SETTLED_MS = 900L
    }
}
