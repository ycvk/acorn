package io.ycvk.acorn.feature.runs

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.apis.ClientApi
import io.ycvk.acorn.api.infrastructure.ApiClient
import io.ycvk.acorn.api.models.RunDetail
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
 * Loads a single run's detail (run, thread, events, artifacts). The chat screen
 * already streams live run events, so this is the secondary "inspect a finished
 * run" view.
 */
@HiltViewModel
class RunDetailViewModel @Inject constructor(
    private val authController: AuthController,
) : ViewModel() {

    private val _runDetail = MutableStateFlow<RunDetail?>(null)
    val runDetail: StateFlow<RunDetail?> = _runDetail.asStateFlow()

    private val _error = MutableStateFlow<String?>(null)
    val error: StateFlow<String?> = _error.asStateFlow()

    fun loadRunDetail(runId: String) {
        val profile = getConnectionProfile() ?: return
        viewModelScope.launch {
            try {
                val detail = withContext(Dispatchers.IO) {
                    ApiClient.accessToken = profile.accessToken
                    ClientApi(basePath = profile.serverUrl).clientGetRunDetail(runId)
                }
                _runDetail.value = detail
                _error.value = null
            } catch (e: Exception) {
                _error.value = e.message ?: "Failed to load run detail"
            }
        }
    }

    private fun getConnectionProfile(): ConnectionProfile? =
        (authController.authState.value as? AuthState.Connected)?.profile
}
