package io.ycvk.acorn.feature.share

import io.ycvk.acorn.api.infrastructure.ClientError
import io.ycvk.acorn.api.infrastructure.ClientException
import io.ycvk.acorn.api.infrastructure.Serializer
import java.io.InputStream
import java.io.OutputStream

/** The server accepts images up to this size. */
const val MAX_SHARED_IMAGE_BYTES = 10L * 1024 * 1024

private const val ACTION_SEND = "android.intent.action.SEND"

/** What another app shared with Acorn. [imageUri] is a content URI string. */
data class SharedContent(
    val text: String?,
    val subject: String?,
    val imageUri: String?,
)

/**
 * Reads a share intent's parts. Returns null for anything Acorn does not take:
 * other actions, other types, or a share without its text or image.
 */
fun parseShare(action: String?, type: String?, text: String?, subject: String?, stream: String?): SharedContent? {
    if (action != ACTION_SEND || type == null) return null
    val cleanText = text?.trim()?.takeIf { it.isNotEmpty() }
    val cleanSubject = subject?.trim()?.takeIf { it.isNotEmpty() }
    return when {
        type == "text/plain" && cleanText != null -> SharedContent(cleanText, cleanSubject, null)
        type.startsWith("image/") && stream != null -> SharedContent(cleanText, cleanSubject, stream)
        else -> null
    }
}

/** The text sent to the server: the shared text, then the owner's note. */
fun captureText(shared: String?, note: String): String? =
    listOfNotNull(shared, note.trim().takeIf { it.isNotEmpty() })
        .joinToString("\n\n")
        .takeIf { it.isNotEmpty() }

/**
 * Copies [input] to [output] and returns false as soon as more than [limit]
 * bytes have been read, so an oversized image is never fully copied.
 */
fun copyWithin(input: InputStream, output: OutputStream, limit: Long): Boolean {
    val buffer = ByteArray(64 * 1024)
    var total = 0L
    while (true) {
        val read = input.read(buffer)
        if (read < 0) return true
        total += read
        if (total > limit) return false
        output.write(buffer, 0, read)
    }
}

/** The server's error message when there is one, else the exception's. */
fun apiErrorMessage(e: Exception): String {
    val body = ((e as? ClientException)?.response as? ClientError<*>)?.body as? String
    if (body != null) {
        runCatching {
            @Suppress("UNCHECKED_CAST")
            val error = Serializer.moshi.adapter(Map::class.java).fromJson(body)?.get("error") as? Map<String, Any?>
            (error?.get("message") as? String)?.let { return it }
        }
    }
    return e.message ?: e.javaClass.simpleName
}
