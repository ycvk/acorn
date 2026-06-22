package io.ycvk.acorn.core.state

import io.ycvk.acorn.core.auth.ConnectionProfile

sealed class ConnectionState {
    data object Disconnected : ConnectionState()
    data class Connecting(val profile: ConnectionProfile) : ConnectionState()
    data class Connected(val profile: ConnectionProfile) : ConnectionState()
    data class Error(val message: String) : ConnectionState()
}
