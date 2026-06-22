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
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import javax.inject.Inject

/**
 * Backs the Approvals tab: lists pending actions and lets the operator decide
 * (accept/decline/answer) on a selected action.
 *
 * The generated [ClientApi] is synchronous (OkHttp execute), so calls are
 * offloaded to [Dispatchers.IO]. Auth is via the companion-level
 * [ApiClient.accessToken] set immediately before each call.
 */
@HiltViewModel
class ApprovalsViewModel @Inject constructor(
    private val authController: AuthController,
) : ViewModel() {

    private val _pendingActions = MutableStateFlow<List<PendingActionSummary>>(emptyList())
    val pendingActions: StateFlow<List<PendingActionSummary>> = _pendingActions.asStateFlow()

    private val _selectedAction = MutableStateFlow<PendingActionDetail?>(null)
    val selectedAction: StateFlow<PendingActionDetail?> = _selectedAction.asStateFlow()

    private val _error = MutableStateFlow<String?>(null)
    val error: StateFlow<String?> = _error.asStateFlow()

    private val _deciding = MutableStateFlow(false)
    val deciding: StateFlow<Boolean> = _deciding.asStateFlow()

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
                _error.value = e.message ?: "Failed to load pending actions"
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
                _error.value = e.message ?: "Failed to load action detail"
            }
        }
    }

    /**
     * Submits a decision for the selected pending action. The backend enum is
     * accept/decline/answer; an [optionId] selects a specific option (sent as
     * `selected_option_id`) and [answer] carries free-text input for elicitation.
     */
    fun decideAction(
        actionId: String,
        decision: DecidePendingActionRequest.Decision,
        selectedOptionId: String? = null,
        answer: String? = null,
    ) {
        val profile = getConnectionProfile() ?: return
        _deciding.value = true
        viewModelScope.launch {
            try {
                withContext(Dispatchers.IO) {
                    ApiClient.accessToken = profile.accessToken
                    ClientApi(basePath = profile.serverUrl).clientDecidePendingAction(
                        actionId,
                        DecidePendingActionRequest(
                            decision = decision,
                            selectedOptionId = selectedOptionId,
                            answer = answer,
                        ),
                    )
                }
                _selectedAction.value = null
                loadPendingActions() // refresh list
            } catch (e: Exception) {
                _error.value = e.message ?: "Failed to decide action"
            } finally {
                _deciding.value = false
            }
        }
    }

    fun clearSelection() {
        _selectedAction.value = null
    }

    fun clearError() {
        _error.value = null
    }

    private fun getConnectionProfile(): ConnectionProfile? =
        (authController.authState.value as? AuthState.Connected)?.profile
}
