package io.ycvk.acorn.feature.settings

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import io.ycvk.acorn.api.models.OwnerSettingName
import io.ycvk.acorn.api.models.OwnerSettings
import io.ycvk.acorn.api.models.OwnerSettingsChanges
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.ui.userMessage
import io.ycvk.acorn.data.repository.OwnerSettingsRepository
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import javax.inject.Inject

/**
 * Backs the owner's settings page: loads the settings with what the budgets
 * used today, saves one setting at a time, and lists the agent's tools for
 * approval rules.
 */
@HiltViewModel
class OwnerSettingsViewModel @Inject constructor(
    private val authController: AuthController,
    private val repository: OwnerSettingsRepository,
) : ViewModel() {

    sealed interface Load {
        data object Loading : Load
        data class Loaded(val settings: OwnerSettings) : Load
        data class Failed(val message: String) : Load
    }

    private val _state = MutableStateFlow<Load>(Load.Loading)
    val state: StateFlow<Load> = _state.asStateFlow()

    private val _toolNames = MutableStateFlow<List<String>>(emptyList())
    val toolNames: StateFlow<List<String>> = _toolNames.asStateFlow()

    /** Why the tool list did not load; rules can still be typed by hand. */
    private val _toolsError = MutableStateFlow<String?>(null)
    val toolsError: StateFlow<String?> = _toolsError.asStateFlow()

    private val _saving = MutableStateFlow(false)
    val saving: StateFlow<Boolean> = _saving.asStateFlow()

    /** Why the last save failed, shown in the editor that made it. */
    private val _saveError = MutableStateFlow<String?>(null)
    val saveError: StateFlow<String?> = _saveError.asStateFlow()

    private val profile: ConnectionProfile?
        get() = (authController.authState.value as? AuthState.Connected)?.profile

    fun load() {
        val profile = profile ?: return
        viewModelScope.launch {
            _state.value = try {
                Load.Loaded(withContext(Dispatchers.IO) { repository.current(profile) })
            } catch (e: Exception) {
                Load.Failed(userMessage(e))
            }
        }
        viewModelScope.launch {
            try {
                _toolNames.value = withContext(Dispatchers.IO) { repository.toolNames(profile) }
                _toolsError.value = null
            } catch (e: Exception) {
                _toolsError.value = userMessage(e)
            }
        }
    }

    /** Saves [changes], one setting, and calls [onSaved] once the server took it. */
    fun save(changes: OwnerSettingsChanges, onSaved: () -> Unit) = write(onSaved) { repository.save(it, changes) }

    /** Returns [name] to its default. */
    fun reset(name: OwnerSettingName, onSaved: () -> Unit) = write(onSaved) { repository.reset(it, name) }

    fun dismissSaveError() {
        _saveError.value = null
    }

    private fun write(onSaved: () -> Unit, call: (ConnectionProfile) -> OwnerSettings) {
        val profile = profile ?: return
        _saving.value = true
        _saveError.value = null
        viewModelScope.launch {
            try {
                _state.value = Load.Loaded(withContext(Dispatchers.IO) { call(profile) })
                onSaved()
            } catch (e: Exception) {
                _saveError.value = userMessage(e)
            } finally {
                _saving.value = false
            }
        }
    }
}
