package io.ycvk.acorn.feature.chat

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
}
