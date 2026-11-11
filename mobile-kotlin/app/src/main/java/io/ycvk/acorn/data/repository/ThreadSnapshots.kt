package io.ycvk.acorn.data.repository

import com.squareup.moshi.JsonDataException
import com.squareup.moshi.JsonEncodingException
import io.ycvk.acorn.api.infrastructure.Serializer
import io.ycvk.acorn.api.models.Message
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.io.writeAtomically
import java.io.File
import java.io.IOException

/** A conversation as the server last gave it: who it was read for, its title and its messages. */
data class ThreadSnapshot(val owner: String, val title: String?, val messages: List<Message>)

/** The copies to drop once the server has listed [listed]: those of conversations it no longer has. */
fun unlisted(copies: Set<String>, listed: Set<String>): Set<String> = copies - listed

/**
 * Copies of the conversations opened most recently, as the server last gave
 * them, so that one shows at once when it is opened again while the server is
 * asked afresh. The server's answer always replaces the copy; nothing is ever
 * sent from one. The copies sit in [dir], in the app's cache, with the last
 * few also in memory. A conversation's copy goes when it is deleted or the
 * server no longer lists it, and all of them go when the device disconnects.
 */
class ThreadSnapshots(private val dir: File) {
    private val adapter = Serializer.moshi.adapter(ThreadSnapshot::class.java)
    private val recent = object : LinkedHashMap<String, ThreadSnapshot>(16, 0.75f, true) {
        override fun removeEldestEntry(eldest: MutableMap.MutableEntry<String, ThreadSnapshot>?): Boolean = size > IN_MEMORY
    }

    /** The copy of a conversation if it is in memory, to show without waiting for the disk. */
    @Synchronized
    fun remembered(profile: ConnectionProfile, threadId: String): ThreadSnapshot? =
        recent[threadId]?.takeIf { it.owner == owner(profile) }

    /**
     * Reads the copy of a conversation; blocking. A copy that cannot be read,
     * written by a version of the app that shaped it differently, or kept for
     * another server is no copy, and is removed.
     */
    @Synchronized
    fun read(profile: ConnectionProfile, threadId: String): ThreadSnapshot? {
        remembered(profile, threadId)?.let { return it }
        val file = file(threadId)
        if (!file.exists()) return null
        val snapshot = try {
            adapter.fromJson(file.readText())
        } catch (_: JsonDataException) {
            null
        } catch (_: JsonEncodingException) {
            null
        } catch (_: IOException) {
            null
        }
        if (snapshot == null || snapshot.owner != owner(profile)) {
            file.delete()
            return null
        }
        recent[threadId] = snapshot
        return snapshot
    }

    /** Keeps what the server just gave for a conversation; blocking. */
    @Synchronized
    fun write(profile: ConnectionProfile, threadId: String, title: String?, messages: List<Message>) {
        val snapshot = ThreadSnapshot(owner(profile), title, messages)
        recent[threadId] = snapshot
        dir.mkdirs()
        writeAtomically(file(threadId)) { it.write(adapter.toJson(snapshot).toByteArray(Charsets.UTF_8)) }
        files().sortedByDescending { it.lastModified() }.drop(ON_DISK).forEach { it.delete() }
    }

    @Synchronized
    fun forget(threadId: String) {
        recent.remove(threadId)
        file(threadId).delete()
    }

    /** Drops the copies of conversations that are not among [listed], every conversation the server has. */
    @Synchronized
    fun keepOnly(listed: Set<String>) {
        val names = listed.associateBy(::name)
        unlisted(files().map { it.nameWithoutExtension }.toSet(), names.keys).forEach { File(dir, "$it.json").delete() }
        recent.keys.retainAll(listed)
    }

    @Synchronized
    fun clear() {
        recent.clear()
        files().forEach { it.delete() }
    }

    private fun files(): List<File> = dir.listFiles { file -> file.extension == "json" }?.toList().orEmpty()

    private fun file(threadId: String) = File(dir, "${name(threadId)}.json")

    private fun owner(profile: ConnectionProfile) = "${profile.serverUrl.trimEnd('/')}\n${profile.deviceId}"

    companion object {
        const val ON_DISK = 30
        const val IN_MEMORY = 8

        /** A conversation's file name: its id, with anything a file name cannot hold replaced. */
        fun name(threadId: String): String = threadId.replace(Regex("[^A-Za-z0-9_-]"), "_")
    }
}
