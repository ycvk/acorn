package io.ycvk.acorn.core.auth

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * Owns connection/auth lifecycle. Exposes connection state as StateFlow.
 * Pairing creates a temporary unauthenticated client, exchanges code for token,
 * then stores the connection profile.
 */
class AuthController(private val secureStore: SecureStore) {
    private val _authState = MutableStateFlow<AuthState>(AuthState.Loading)
    val authState: StateFlow<AuthState> = _authState.asStateFlow()

    fun loadStoredConnection() {
        val profile = secureStore.getConnection()
        _authState.value = if (profile != null) {
            AuthState.Connected(profile)
        } else {
            AuthState.Disconnected
        }
    }

    fun onPaired(profile: ConnectionProfile) {
        secureStore.saveConnection(profile.serverUrl, profile.deviceId, profile.accessToken)
        _authState.value = AuthState.Connected(profile)
    }

    fun disconnect() {
        secureStore.clearConnection()
        _authState.value = AuthState.Disconnected
    }
}

sealed class AuthState {
    data object Loading : AuthState()
    data object Disconnected : AuthState()
    data class Connected(val profile: ConnectionProfile) : AuthState()
}
