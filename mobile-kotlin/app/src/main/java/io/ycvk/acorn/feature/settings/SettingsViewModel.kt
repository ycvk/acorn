package io.ycvk.acorn.feature.settings

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.apis.ClientApi
import io.ycvk.acorn.api.infrastructure.ApiClient
import io.ycvk.acorn.api.models.SystemStatus
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.ui.userMessage
import io.ycvk.acorn.core.push.PushManager
import io.ycvk.acorn.core.push.PushStatus
import io.ycvk.acorn.data.repository.OwnerRepository
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import javax.inject.Inject

/**
 * Backs the settings page: loads server [SystemStatus] so the owner can
 * inspect the model, readiness and the connection, sets what Acorn calls the
 * owner, and disconnects the device.
 */
@HiltViewModel
class SettingsViewModel @Inject constructor(
	private val authController: AuthController,
	private val pushManager: PushManager,
    private val ownerRepository: OwnerRepository,
) : ViewModel() {

	val pushStatus: StateFlow<PushStatus> = pushManager.status

	fun retryPush() {
		if (profile != null) pushManager.register()
	}

	private val _systemStatus = MutableStateFlow<SystemStatus?>(null)
	val systemStatus: StateFlow<SystemStatus?> = _systemStatus.asStateFlow()

	private val _error = MutableStateFlow<String?>(null)
	val error: StateFlow<String?> = _error.asStateFlow()

    /** What Acorn calls the owner: null until loaded, empty when unnamed. */
    private val _ownerName = MutableStateFlow<String?>(null)
    val ownerName: StateFlow<String?> = _ownerName.asStateFlow()

    private val _savingName = MutableStateFlow(false)
    val savingName: StateFlow<Boolean> = _savingName.asStateFlow()

    private val _nameError = MutableStateFlow<String?>(null)
    val nameError: StateFlow<String?> = _nameError.asStateFlow()

    val profile: ConnectionProfile?
        get() = (authController.authState.value as? AuthState.Connected)?.profile

	fun loadSettings() {
		val profile = profile ?: return
		viewModelScope.launch {
			try {
				val status = withContext(Dispatchers.IO) {
					ApiClient.accessToken = profile.accessToken
					val api = ClientApi(basePath = profile.serverUrl)
					api.clientGetSystemStatus()
				}
				_systemStatus.value = status
				_error.value = null
			} catch (e: Exception) {
				_error.value = userMessage(e)
            }
        }
        viewModelScope.launch {
            try {
                _ownerName.value = withContext(Dispatchers.IO) { ownerRepository.current(profile) }.name
            } catch (e: Exception) {
                _error.value = userMessage(e)
            }
        }
    }

    /** Saves [name] as what Acorn calls the owner; an empty name leaves them unnamed. */
    fun saveOwnerName(name: String, onSaved: () -> Unit) {
        val profile = profile ?: return
        _savingName.value = true
        _nameError.value = null
        viewModelScope.launch {
            try {
                _ownerName.value = withContext(Dispatchers.IO) { ownerRepository.setName(profile, name) }.name
                onSaved()
            } catch (e: Exception) {
                _nameError.value = userMessage(e)
            } finally {
                _savingName.value = false
            }
        }
    }

    fun dismissNameError() {
        _nameError.value = null
    }

    fun disconnect() {
        authController.disconnect()
    }
}
