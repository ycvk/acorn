package io.ycvk.acorn.feature.chat

/**
 * UI-facing chat message. Sealed so the message list can render user and
 * assistant bubbles through a single [LazyColumn] item type.
 *
 * Not the wire [io.ycvk.acorn.api.models.Message]; this is the local view model
 * the chat screen renders. Reasoning is optional and only present for assistant
 * messages that carried reasoning parts (or live streaming deltas).
 */
sealed class ChatMessage {
    abstract val id: String

    data class User(
        val text: String,
        val timestamp: Long = System.currentTimeMillis(),
    ) : ChatMessage() {
        override val id: String = "user_$timestamp"
    }

    data class Assistant(
        val text: String,
        val reasoning: String? = null,
        val timestamp: Long = System.currentTimeMillis(),
    ) : ChatMessage() {
        override val id: String = "assistant_$timestamp"
    }
}
