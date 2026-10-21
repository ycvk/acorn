package io.ycvk.acorn.core.auth

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import com.squareup.moshi.Moshi
import com.squareup.moshi.kotlin.reflect.KotlinJsonAdapterFactory
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.nio.file.Files
import java.nio.file.StandardCopyOption
import java.security.KeyStore
import java.security.KeyStoreException
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * Stores the connection profile (server URL, device ID, access token) in a file
 * sealed with an Android Keystore key.
 *
 * Keystore calls go through a system service and the first use migrates the
 * previous store, so callers access the store off the main thread.
 */
class SecureStore(context: Context) {
    private val file by lazy {
        ConnectionFile(File(context.filesDir, "connection.bin"), KeystoreConnectionKey("acorn_connection_key"))
            // COMPAT: carries over a pairing made by an earlier build — remove with LegacyConnection.kt.
            .also { migrateLegacyConnection(EncryptedPrefsConnection(context), it) }
    }

    fun saveConnection(serverUrl: String, deviceId: String, accessToken: String) {
        file.write(ConnectionProfile(serverUrl, deviceId, accessToken))
    }

    fun getConnection(): ConnectionProfile? = file.read()

    fun clearConnection() {
        file.delete()
    }
}

data class ConnectionProfile(
    val serverUrl: String,
    val deviceId: String,
    val accessToken: String,
)

/** The AES key that seals the connection file. */
interface ConnectionKey {
    /** The key a sealed file was written with; a missing key is an error. */
    fun existing(): SecretKey

    fun getOrCreate(): SecretKey
}

/**
 * One profile as AES-GCM ciphertext of its JSON, preceded by the 12-byte IV.
 * The file name is the associated data. A missing file means not paired; a file
 * that cannot be opened throws.
 */
class ConnectionFile(private val file: File, private val key: ConnectionKey) {
    private val adapter = Moshi.Builder().addLast(KotlinJsonAdapterFactory()).build()
        .adapter(ConnectionProfile::class.java)

    @Synchronized
    fun read(): ConnectionProfile? {
        if (!file.exists()) return null
        val sealed = file.readBytes()
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.DECRYPT_MODE, key.existing(), GCMParameterSpec(TAG_BITS, sealed, 0, IV_BYTES))
        cipher.updateAAD(file.name.toByteArray(Charsets.UTF_8))
        val json = cipher.doFinal(sealed, IV_BYTES, sealed.size - IV_BYTES).toString(Charsets.UTF_8)
        return adapter.fromJson(json) ?: throw IOException("Connection file ${file.name} holds no profile")
    }

    @Synchronized
    fun write(profile: ConnectionProfile) {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, key.getOrCreate())
        cipher.updateAAD(file.name.toByteArray(Charsets.UTF_8))
        val sealed = cipher.doFinal(adapter.toJson(profile).toByteArray(Charsets.UTF_8))
        val temporary = File(file.parentFile, "${file.name}.new")
        try {
            FileOutputStream(temporary).use { stream ->
                stream.write(cipher.iv)
                stream.write(sealed)
                stream.fd.sync()
            }
            Files.move(temporary.toPath(), file.toPath(), StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING)
        } finally {
            Files.deleteIfExists(temporary.toPath())
        }
    }

    @Synchronized
    fun delete() {
        Files.deleteIfExists(file.toPath())
    }

    private companion object {
        const val TRANSFORMATION = "AES/GCM/NoPadding"
        const val IV_BYTES = 12
        const val TAG_BITS = 128
    }
}

/** An AES-256-GCM key generated inside the Android Keystore; it never leaves it. */
class KeystoreConnectionKey(private val alias: String) : ConnectionKey {
    override fun existing(): SecretKey =
        stored() ?: throw KeyStoreException("Android Keystore has no key $alias for the stored connection")

    override fun getOrCreate(): SecretKey = stored() ?: KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, ANDROID_KEYSTORE).run {
        init(
            KeyGenParameterSpec.Builder(alias, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build(),
        )
        generateKey()
    }

    private fun stored(): SecretKey? =
        KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }.getKey(alias, null) as SecretKey?
}

internal const val ANDROID_KEYSTORE = "AndroidKeyStore"
