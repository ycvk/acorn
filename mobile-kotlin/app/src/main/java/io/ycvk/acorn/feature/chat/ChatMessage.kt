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
}

/** Maps a stored thread message to its chat row. */
fun chatMessageFrom(role: Message.Role, text: String): ChatMessage = when (role) {
    Message.Role.user -> ChatMessage.User(text)
    Message.Role.wake -> ChatMessage.Wake(text)
    Message.Role.assistant -> ChatMessage.Assistant(text)
    // System / tool messages render as assistant bubbles so the
    // history reads top-to-bottom without gaps.
    Message.Role.system, Message.Role.tool -> ChatMessage.Assistant(text)
}
