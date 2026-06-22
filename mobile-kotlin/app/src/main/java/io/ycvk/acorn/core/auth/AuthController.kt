package io.ycvk.acorn.core.auth

import io.ycvk.acorn.data.repository.AuthRepository
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import javax.inject.Inject

/**
 * Owns connection/auth lifecycle as an application-scoped singleton. Exposes
 * connection state and pairing state as [StateFlow]s.
 *
 * Pairing creates a temporary unauthenticated client, exchanges code for token,
 * then stores the connection profile. The generated [DevicesApi] uses synchronous
 * OkHttp `execute`, so [pair] offloads to [Dispatchers.IO].
 *
 * Not a [ViewModel] — plain `@Singleton` so it can be injected into multiple
 * `@HiltViewModel`s (ShellViewModel, ThreadsViewModel) without violating Hilt's
 * view-model injection rules. Uses a [SupervisorJob] scope that lives for the
 * app lifetime; failures in one pairing attempt do not cancel the scope.
 */
class AuthController @Inject constructor(
    private val secureStore: SecureStore,
    private val authRepository: AuthRepository,
) {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)

    private val _authState = MutableStateFlow<AuthState>(AuthState.Loading)
    val authState: StateFlow<AuthState> = _authState.asStateFlow()

    private val _pairingState = MutableStateFlow<PairingState>(PairingState.Idle)
    val pairingState: StateFlow<PairingState> = _pairingState.asStateFlow()

    fun loadStoredConnection() {
        val profile = secureStore.getConnection()
        _authState.value = if (profile != null) {
            AuthState.Connected(profile)
        } else {
            AuthState.Disconnected
        }
    }

    fun pair(serverUrl: String, pairingCode: String, deviceName: String) {
        scope.launch {
            _pairingState.value = PairingState.Pairing
            try {
                val profile = withContext(Dispatchers.IO) {
                    authRepository.pair(serverUrl, pairingCode, deviceName)
                }
                onPaired(profile)
                _pairingState.value = PairingState.Success
            } catch (e: Exception) {
                _pairingState.value = PairingState.Error(e.message ?: "Pairing failed")
            }
        }
    }

    fun onPaired(profile: ConnectionProfile) {
        secureStore.saveConnection(profile.serverUrl, profile.deviceId, profile.accessToken)
        _authState.value = AuthState.Connected(profile)
    }

    fun disconnect() {
        secureStore.clearConnection()
        _authState.value = AuthState.Disconnected
        _pairingState.value = PairingState.Idle
    }

    /**
     * Clears a terminal pairing error so the UI returns to the idle input form.
     */
    fun resetPairingState() {
        _pairingState.value = PairingState.Idle
    }
}

sealed class AuthState {
    data object Loading : AuthState()
    data object Disconnected : AuthState()
    data class Connected(val profile: ConnectionProfile) : AuthState()
}

sealed class PairingState {
    data object Idle : PairingState()
    data object Pairing : PairingState()
    data object Success : PairingState()
    data class Error(val message: String) : PairingState()
}
