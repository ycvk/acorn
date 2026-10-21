package io.ycvk.acorn.feature.approvals

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.apis.ClientApi
import io.ycvk.acorn.api.infrastructure.ApiClient
import io.ycvk.acorn.api.models.DecidePendingActionRequest
import io.ycvk.acorn.api.models.PendingActionDetail
import io.ycvk.acorn.api.models.PendingActionSummary
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.ui.userMessage
import io.ycvk.acorn.feature.live.RunFollower
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import javax.inject.Inject

/**
 * Lists pending actions and lets the owner decide (accept/decline/answer) on
 * one, inline on Today or from the detail sheet.
 *
 * The generated [ClientApi] is synchronous (OkHttp execute), so calls are
 * offloaded to [Dispatchers.IO]. Auth is via the companion-level
 * [ApiClient.accessToken] set immediately before each call.
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

    fun loadPendingActions() {
        val profile = getConnectionProfile() ?: return
        viewModelScope.launch {
            try {
                val response = withContext(Dispatchers.IO) {
                    ApiClient.accessToken = profile.accessToken
                    ClientApi(basePath = profile.serverUrl).clientListPendingActions(limit = 50)
                }
                _pendingActions.value = response.items
                _error.value = null
            } catch (e: Exception) {
                _error.value = userMessage(e)
            }
        }
    }

    fun loadActionDetail(actionId: String) {
        val profile = getConnectionProfile() ?: return
        viewModelScope.launch {
            try {
                val detail = withContext(Dispatchers.IO) {
                    ApiClient.accessToken = profile.accessToken
                    ClientApi(basePath = profile.serverUrl).clientGetPendingAction(actionId)
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
     * Update until it ends or waits again.
     */
    fun decideAction(actionId: String, request: DecidePendingActionRequest) {
        val profile = getConnectionProfile() ?: return
        val run = _pendingActions.value.firstOrNull { it.actionId == actionId }?.let { it.runId to it.threadId }
            ?: _selectedAction.value?.takeIf { it.actionId == actionId }?.let { it.runId to it.threadId }
        _deciding.value = actionId
        _error.value = null
        viewModelScope.launch {
            try {
                withContext(Dispatchers.IO) {
                    ApiClient.accessToken = profile.accessToken
                    ClientApi(basePath = profile.serverUrl).clientDecidePendingAction(actionId, request)
                }
                run?.let { (runId, threadId) -> follower.follow(profile, runId, threadId, title = null, live = true, decided = actionId) }
                _selectedAction.value = null
                notifications.cancel(actionId)
                loadPendingActions() // refresh list
            } catch (e: Exception) {
                _error.value = userMessage(e)
            } finally {
                _deciding.value = null
            }
        }
    }

    fun dismissError() {
        _error.value = null
    }

    fun clearSelection() {
        _selectedAction.value = null
    }

    private fun getConnectionProfile(): ConnectionProfile? =
        (authController.authState.value as? AuthState.Connected)?.profile
}
