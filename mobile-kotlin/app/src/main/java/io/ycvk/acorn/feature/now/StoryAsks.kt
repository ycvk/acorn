package io.ycvk.acorn.feature.now

/** What the owner asks Acorn about a story on the briefing card. */
enum class StoryAsk { Discuss, Follow }

/** An ask about a story that got no answer: it may or may not have reached the story's conversation. */
data class UnansweredAsk(val storyId: Long, val kind: StoryAsk, val clientId: String)

/** The client id of an ask: the unanswered ask's when this repeats it, else a new one from [newId]. */
fun askClientId(unanswered: UnansweredAsk?, storyId: Long, kind: StoryAsk, newId: () -> String): String =
    unanswered?.takeIf { it.storyId == storyId && it.kind == kind }?.clientId ?: newId()
