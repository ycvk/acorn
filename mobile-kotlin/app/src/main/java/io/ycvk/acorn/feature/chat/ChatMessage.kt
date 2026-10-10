package io.ycvk.acorn.feature.chat

import io.ycvk.acorn.api.models.Message
import java.util.UUID

sealed class ChatMessage {
    abstract val id: String

    data class User(
        val text: String,
    ) : ChatMessage() {
        override val id: String = "user_${UUID.randomUUID()}"
    }

    data class Assistant(
        val text: String,
        val reasoning: String? = null,
    ) : ChatMessage() {
        override val id: String = "assistant_${UUID.randomUUID()}"
    }

    /** The input of a run the backend woke: a commitment, a watch, or the morning briefing. */
    data class Wake(
        val text: String,
        val source: WakeSource,
    ) : ChatMessage() {
        override val id: String = "wake_${UUID.randomUUID()}"
    }

    /** Something the owner shared from their phone; text is ready to show, image is an attachment path. */
    data class Capture(
        val text: String,
        val image: String? = null,
    ) : ChatMessage() {
        override val id: String = "capture_${UUID.randomUUID()}"
    }
}

/** Maps a stored thread message to its chat row. */
fun chatMessageFrom(role: Message.Role, text: String): ChatMessage = when (role) {
    Message.Role.user -> ChatMessage.User(text)
    Message.Role.wake -> ChatMessage.Wake(text, wakeSource(text))
    Message.Role.capture -> captureMessage(text)
    Message.Role.assistant -> ChatMessage.Assistant(text)
    // System / tool messages render as assistant bubbles so the
    // history reads top-to-bottom without gaps.
    Message.Role.system, Message.Role.tool -> ChatMessage.Assistant(text)
}

/**
 * Turns a capture run input into a card: drops the "[capture]" header line and
 * takes the attachment path out of the "Image:" line.
 */
fun captureMessage(input: String): ChatMessage.Capture {
    val lines = input.lines().dropWhile { it.startsWith("[capture]") }
    val imageLine = lines.indexOfFirst { it.startsWith("Image: ") }
    val image = lines.getOrNull(imageLine)?.removePrefix("Image: ")?.substringBefore(" (")
    val text = lines.filterIndexed { index, _ -> index != imageLine }.joinToString("\n").trim()
    return ChatMessage.Capture(text, image)
}

enum class WakeSource { Commitment, Watch, Briefing, Night, Wander }

/** What woke a run, from the prefix the backend gives its input. */
fun wakeSource(input: String): WakeSource = when {
    input.startsWith("[night ") -> WakeSource.Night
    input.startsWith("[wander ") -> WakeSource.Wander
    input.startsWith("[watch") -> WakeSource.Watch
    input.startsWith("[briefing") -> WakeSource.Briefing
    else -> WakeSource.Commitment
}
