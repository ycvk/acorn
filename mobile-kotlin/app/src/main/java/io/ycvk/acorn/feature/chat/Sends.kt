package io.ycvk.acorn.feature.chat

/**
 * A send that did not get the server's answer: it may or may not have
 * reached the thread. Sent again unchanged, the message keeps its client id
 * and the files it already uploaded, so the server keeps one copy of it.
 * [threadId] is the thread it went to, null when it failed before it had one.
 */
data class UnansweredSend(
    val threadId: String?,
    val text: String,
    val attachmentKeys: List<String>,
    val clientId: String,
    val uploaded: Map<String, String>,
)

/** The client id and uploaded file ids (by attachment key) a send starts from. */
data class SendIdentity(val clientId: String, val uploaded: Map<String, String>)

/**
 * The identity of a send of [text] and the attachments keyed [attachmentKeys]
 * to [threadId]: the unanswered send's when this repeats it, else a new
 * client id from [newId].
 */
fun sendIdentity(
    unanswered: UnansweredSend?,
    threadId: String?,
    text: String,
    attachmentKeys: List<String>,
    newId: () -> String,
): SendIdentity {
    val repeats = unanswered != null &&
        unanswered.text == text &&
        unanswered.attachmentKeys == attachmentKeys &&
        (unanswered.threadId == null || unanswered.threadId == threadId)
    return if (repeats) SendIdentity(unanswered!!.clientId, unanswered.uploaded) else SendIdentity(newId(), emptyMap())
}
