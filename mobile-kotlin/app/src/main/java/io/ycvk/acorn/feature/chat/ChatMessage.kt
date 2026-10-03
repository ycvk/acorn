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

    /** The input of a run woken by one of the agent's commitments. */
    data class Wake(
        val text: String,
    ) : ChatMessage() {
        override val id: String = "wake_${UUID.randomUUID()}"
    }

    /** Something the owner shared from their phone; text is ready to show. */
    data class Capture(
        val text: String,
    ) : ChatMessage() {
        override val id: String = "capture_${UUID.randomUUID()}"
    }
}

/** Maps a stored thread message to its chat row. */
fun chatMessageFrom(role: Message.Role, text: String): ChatMessage = when (role) {
    Message.Role.user -> ChatMessage.User(text)
    Message.Role.wake -> ChatMessage.Wake(text)
    Message.Role.capture -> ChatMessage.Capture(captureCardText(text))
    Message.Role.assistant -> ChatMessage.Assistant(text)
    // System / tool messages render as assistant bubbles so the
    // history reads top-to-bottom without gaps.
    Message.Role.system, Message.Role.tool -> ChatMessage.Assistant(text)
}

/**
 * Turns a capture run input into card text: drops the "[capture]" header line
 * and shows attachments by file name.
 */
fun captureCardText(input: String): String =
    input.lines()
        .dropWhile { it.startsWith("[capture]") }
        .joinToString("\n") { line ->
            val image = line.removePrefix("Image: ")
            if (image != line) "Image: " + image.substringBefore(" (").substringAfterLast('/') else line
        }
        .trim()
