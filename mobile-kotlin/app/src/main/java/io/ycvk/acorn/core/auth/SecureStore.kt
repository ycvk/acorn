package io.ycvk.acorn.core.auth

import android.content.Context
import androidx.security.crypto.EncryptedSharedPreferences

/**
 * Stores connection profile (server URL, device ID, access token) in EncryptedSharedPreferences.
 */
class SecureStore(context: Context) {
    private val prefs = EncryptedSharedPreferences.create(
        "acorn_connection",
        "acorn_master_key",
        context,
        EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
        EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
    )

    fun saveConnection(serverUrl: String, deviceId: String, accessToken: String) {
        prefs.edit()
            .putString(KEY_SERVER_URL, serverUrl)
            .putString(KEY_DEVICE_ID, deviceId)
            .putString(KEY_ACCESS_TOKEN, accessToken)
            .apply()
    }

    fun getConnection(): ConnectionProfile? {
        val url = prefs.getString(KEY_SERVER_URL, null) ?: return null
        val deviceId = prefs.getString(KEY_DEVICE_ID, null) ?: return null
        val token = prefs.getString(KEY_ACCESS_TOKEN, null) ?: return null
        return ConnectionProfile(url, deviceId, token)
    }

    fun clearConnection() {
        prefs.edit().clear().apply()
    }

    companion object {
        private const val KEY_SERVER_URL = "server_url"
        private const val KEY_DEVICE_ID = "device_id"
        private const val KEY_ACCESS_TOKEN = "access_token"
    }
}

data class ConnectionProfile(
    val serverUrl: String,
    val deviceId: String,
    val accessToken: String,
)
