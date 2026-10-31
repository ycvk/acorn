package io.ycvk.acorn.core.auth

import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.security.KeyStoreException
import javax.crypto.AEADBadTagException
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey

class ConnectionFileTest {
    @get:Rule val folder = TemporaryFolder()
    private val profile = ConnectionProfile("https://acorn.example", "phone", "secret-token")

    /** Stands in for the Keystore key on the JVM. */
    private class SoftwareKey : ConnectionKey {
        var key: SecretKey? = null
        override fun existing(): SecretKey = key ?: throw KeyStoreException("no key")
        override fun getOrCreate(): SecretKey = key ?: KeyGenerator.getInstance("AES").apply { init(256) }.generateKey().also { key = it }
    }

    private fun store(key: ConnectionKey = SoftwareKey()) = ConnectionFile(folder.root.resolve("connection.bin"), key)

    @Test fun sealsTheProfileAndReadsItBack() {
        val key = SoftwareKey()
        store(key).write(profile)
        assertFalse(folder.root.resolve("connection.bin").readBytes().toString(Charsets.ISO_8859_1).contains("secret-token"))
        assertEquals(profile, store(key).read())
        assertEquals(listOf("connection.bin"), folder.root.list()!!.toList())
    }

    @Test fun noFileMeansNotPairedAndDeleteUnpairs() {
        val store = store()
        assertNull(store.read())
        store.write(profile)
        store.delete()
        assertNull(store.read())
    }

    @Test fun aFileWhoseKeyIsGoneFailsLoudly() {
        store().write(profile)
        assertThrows(KeyStoreException::class.java) { store(SoftwareKey()).read() }
    }

    @Test fun aTamperedFileFailsLoudly() {
        val key = SoftwareKey()
        store(key).write(profile)
        val file = folder.root.resolve("connection.bin")
        val bytes = file.readBytes()
        bytes[bytes.size - 1] = (bytes.last().toInt() xor 1).toByte()
        file.writeBytes(bytes)
        assertThrows(AEADBadTagException::class.java) { store(key).read() }
    }
}
