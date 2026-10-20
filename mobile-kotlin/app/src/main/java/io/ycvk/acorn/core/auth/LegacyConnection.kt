package io.ycvk.acorn.core.auth

import android.content.Context
import androidx.security.crypto.EncryptedSharedPreferences
import java.io.IOException
import java.security.KeyStore

// COMPAT: the owner's phone was paired while the connection lived in EncryptedSharedPreferences — remove this file and the security-crypto dependency after that phone has run a build with ConnectionFile.

/** The connection store of earlier builds. */
interface LegacyConnection {
    /** Whether the old store left anything on disk. */
    fun exists(): Boolean

    fun read(): ConnectionProfile?

    fun delete()
}

/** Moves the old store's profile into [store] and deletes the old store. A failed read leaves it in place. */
fun migrateLegacyConnection(legacy: LegacyConnection, store: ConnectionFile) {
    if (!legacy.exists()) return
    legacy.read()?.let(store::write)
    legacy.delete()
}

class EncryptedPrefsConnection(private val context: Context) : LegacyConnection {
    override fun exists(): Boolean = context.getSharedPreferences(FILE, Context.MODE_PRIVATE).all.isNotEmpty()

    @Suppress("DEPRECATION")
    override fun read(): ConnectionProfile? {
        val prefs = EncryptedSharedPreferences.create(
            FILE,
            MASTER_KEY,
            context,
            EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
            EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
        )
        val url = prefs.getString("server_url", null) ?: return null
        val deviceId = prefs.getString("device_id", null) ?: return null
        val token = prefs.getString("access_token", null) ?: return null
        return ConnectionProfile(url, deviceId, token)
    }

    override fun delete() {
        if (!context.deleteSharedPreferences(FILE)) throw IOException("Could not delete the old connection store $FILE")
        KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }.deleteEntry(MASTER_KEY)
    }

    private companion object {
        const val FILE = "acorn_connection"
        const val MASTER_KEY = "acorn_master_key"
    }
}
