package io.ycvk.acorn.feature.persona

import io.ycvk.acorn.core.ui.ago
import java.time.OffsetDateTime
import java.util.Locale

/** The server's bound on a trimmed persona, in UTF-8 bytes. */
const val MAX_PERSONA_BYTES = 8 * 1024

enum class DraftState { Ready, Unchanged, Empty, TooLong }

/** Whether [text] can be saved over [current]: the server trims it, refuses it empty or over [MAX_PERSONA_BYTES]. */
fun draftState(text: String, current: String): DraftState {
    val trimmed = text.trim()
    return when {
        trimmed.isEmpty() -> DraftState.Empty
        trimmed.toByteArray(Charsets.UTF_8).size > MAX_PERSONA_BYTES -> DraftState.TooLong
        trimmed == current.trim() -> DraftState.Unchanged
        else -> DraftState.Ready
    }
}

/** "3.2 / 8 KB": how much of the bound the trimmed draft takes. */
fun draftSize(text: String): String {
    val bytes = text.trim().toByteArray(Charsets.UTF_8).size
    return String.format(Locale.ROOT, "%.1f / %d KB", bytes / 1024.0, MAX_PERSONA_BYTES / 1024)
}

/** Who saved a revision, in the owner's terms; [thisDevice] is the phone reading it. */
fun personaSource(author: String, deviceId: String?, thisDevice: String?): String = when {
    author == "release" -> "默认人格"
    deviceId == null -> "你在服务器上改的"
    deviceId == thisDevice -> "你在这台手机上改的"
    else -> "你在另一台设备上改的"
}

fun revisionTitle(revision: Long): String = "第 $revision 版"

fun revisionMeta(author: String, deviceId: String?, thisDevice: String?, savedAt: OffsetDateTime, now: OffsetDateTime): String =
    "${personaSource(author, deviceId, thisDevice)} · ${ago(savedAt, now)}"
