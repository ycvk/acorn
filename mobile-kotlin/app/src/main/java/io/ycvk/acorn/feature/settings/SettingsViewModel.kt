package io.ycvk.acorn.feature.settings

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.apis.ClientApi
import io.ycvk.acorn.api.infrastructure.ApiClient
import io.ycvk.acorn.api.models.ClientSettings
import io.ycvk.acorn.api.models.SystemStatus
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
 * Backs the Settings tab: loads server [SystemStatus] and [ClientSettings] so
 * the operator can inspect model, readiness, workspace, and the connection, and
 * disconnect the device.
 */
@HiltViewModel
class SettingsViewModel @Inject constructor(
    private val authController: AuthController,
) : ViewModel() {

    private val _systemStatus = MutableStateFlow<SystemStatus?>(null)
    val systemStatus: StateFlow<SystemStatus?> = _systemStatus.asStateFlow()

    private val _settings = MutableStateFlow<ClientSettings?>(null)
    val settings: StateFlow<ClientSettings?> = _settings.asStateFlow()

    private val _error = MutableStateFlow<String?>(null)
    val error: StateFlow<String?> = _error.asStateFlow()

    val profile: ConnectionProfile?
        get() = (authController.authState.value as? AuthState.Connected)?.profile

    fun loadSettings() {
        val profile = profile ?: return
        viewModelScope.launch {
            try {
                val (status, settings) = withContext(Dispatchers.IO) {
                    ApiClient.accessToken = profile.accessToken
                    val api = ClientApi(basePath = profile.serverUrl)
                    val s = api.clientGetSystemStatus()
                    val c = api.clientGetSettings()
                    android.util.Log.d("AcornSettings", "status=${s}, settings=${c}")
                    Pair(s, c)
                }
                _systemStatus.value = status
                _settings.value = settings
                _error.value = null
            } catch (e: Exception) {
                android.util.Log.e("AcornSettings", "loadSettings failed", e)
                _error.value = e.message ?: "Failed to load settings"
            }
        }
    }

    fun disconnect() {
        authController.disconnect()
    }
}
