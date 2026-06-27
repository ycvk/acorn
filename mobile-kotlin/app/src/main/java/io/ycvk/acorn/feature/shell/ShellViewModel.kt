package io.ycvk.acorn.feature.shell

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.apis.ClientApi
import io.ycvk.acorn.api.infrastructure.ApiClient
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

@HiltViewModel
class ShellViewModel @Inject constructor(
    val authController: AuthController,
) : ViewModel() {

    private val _selectedTab = MutableStateFlow(0)
    val selectedTab: StateFlow<Int> = _selectedTab.asStateFlow()

    private val _openThreadId = MutableStateFlow<String?>(null)
    val openThreadId: StateFlow<String?> = _openThreadId.asStateFlow()

    private val _pendingCount = MutableStateFlow(0)
    val pendingCount: StateFlow<Int> = _pendingCount.asStateFlow()

    init {
        viewModelScope.launch {
            authController.authState.collect { state ->
                if (state is AuthState.Connected) {
                    pollPendingCount()
                } else {
                    _pendingCount.value = 0
                }
            }
        }
    }

    private fun pollPendingCount() {
        viewModelScope.launch {
            while (true) {
                val profile = getConnectionProfile() ?: break
                try {
                    val resp = withContext(Dispatchers.IO) {
                        ApiClient.accessToken = profile.accessToken
                        ClientApi(profile.serverUrl).clientGetInbox()
                    }
                    _pendingCount.value = resp.pendingActions.size
                } catch (_: Exception) {
                    // badge is best-effort; silent on failure
                }
                kotlinx.coroutines.delay(POLL_INTERVAL_MS)
            }
        }
    }

    private fun getConnectionProfile(): ConnectionProfile? =
        (authController.authState.value as? AuthState.Connected)?.profile

    fun selectTab(index: Int) {
        _selectedTab.value = index
    }

    fun openThread(threadId: String) {
        _openThreadId.value = threadId
    }

    fun closeThread() {
        _openThreadId.value = null
    }

    companion object {
        const val TAB_THREADS = 0
        const val TAB_APPROVALS = 1
        const val TAB_SETTINGS = 2
        private const val POLL_INTERVAL_MS = 30_000L
    }
}
