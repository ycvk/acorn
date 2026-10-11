package io.ycvk.acorn.feature.memory

import io.ycvk.acorn.api.models.MemoryEvidence
import io.ycvk.acorn.api.models.MemorySummary
import io.ycvk.acorn.feature.chat.toolLabel

fun kindLabel(kind: MemorySummary.Kind): String = when (kind) {
    MemorySummary.Kind.fact -> "事实"
    MemorySummary.Kind.insight -> "推断"
    MemorySummary.Kind.thought -> "想法"
}

/** Marks shown next to a memory's kind: the owner asked to keep it, or it is in question. */
fun memoryMarks(memory: MemorySummary): List<String> = listOfNotNull(
    "你让它记住的".takeIf { memory.pinned },
    "有矛盾".takeIf { memory.state == "disputed" },
    "待确认".takeIf { memory.needsReview && memory.state != "disputed" },
)

/** Where a piece of evidence came from, in the owner's terms. */
fun evidenceSource(evidence: MemoryEvidence): String {
    val owner = evidence.speaker == "owner"
    return when (evidence.sourceKind) {
        "message" -> if (owner) "你在对话里说" else "Acorn 在对话里说"
        "request" -> "你在 App 里改的"
        "knowledge" -> "笔记 ${evidence.notePath.orEmpty()}".trim()
        "event" -> evidence.toolName?.let { "${toolLabel(it)}的结果" } ?: "工具的结果"
        "watch" -> "追踪到的更新"
        "thought" -> "Acorn 的想法"
        else -> if (owner) "你说的" else "Acorn 看到的"
    }
}

/** What forgetting does to this kind of memory, for the confirmation. */
fun forgetConsequence(kind: MemorySummary.Kind): String = when (kind) {
    MemorySummary.Kind.fact -> "Acorn 之后不会再用到这条事实、它依据的原话，以及由此得出的推断。原来的对话和笔记还在。"
    MemorySummary.Kind.insight -> "Acorn 之后不会再用到这条推断，它依据的事实会保留。"
    MemorySummary.Kind.thought -> "Acorn 之后不会再用到这个想法。"
}
