package io.ycvk.acorn.feature.skills

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.SkillDiff
import io.ycvk.acorn.api.models.SkillDiffLine
import io.ycvk.acorn.api.models.SkillFileDiff
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornButton
import io.ycvk.acorn.core.ui.AcornDialog
import io.ycvk.acorn.core.ui.ButtonKind
import io.ycvk.acorn.core.ui.DiffKind
import io.ycvk.acorn.core.ui.DiffLine
import io.ycvk.acorn.core.ui.DiffView
import io.ycvk.acorn.core.ui.ErrorState
import io.ycvk.acorn.core.ui.InlineError
import io.ycvk.acorn.core.ui.Panel
import io.ycvk.acorn.core.ui.ScreenPadding
import io.ycvk.acorn.core.ui.SegmentedControl
import io.ycvk.acorn.core.ui.SkeletonRows
import io.ycvk.acorn.core.ui.Tag
import io.ycvk.acorn.core.ui.TopBar
import java.time.OffsetDateTime

private enum class RevisionView(val label: String) { Changes("这一版改了什么"), AgainstCurrent("和现在的对比"), Full("全文") }

/** One revision of a skill: what it changed, how it differs from the one in use, its content, and rolling back to it. */
@Composable
fun SkillRevisionScreen(
    name: String,
    revision: Long,
    viewModel: SkillsViewModel,
    onBack: () -> Unit,
    onOpenThread: (String) -> Unit,
) {
    val load by viewModel.revision.collectAsStateWithLifecycle()
    val skill by viewModel.skill.collectAsStateWithLifecycle()
    val diff by viewModel.diff.collectAsStateWithLifecycle()
    val change by viewModel.change.collectAsStateWithLifecycle()
    LaunchedEffect(name, revision) {
        viewModel.openRevision(name, revision)
        if ((skill as? SkillLoad.Loaded)?.detail?.name != name) viewModel.open(name)
    }
    val colors = AcornTheme.colors
    val scroll = rememberScrollState()
    var selected by rememberSaveable { mutableIntStateOf(0) }
    var confirmRollback by rememberSaveable { mutableStateOf(false) }
    val shown = (load as? SkillRevisionLoad.Loaded)?.revision?.takeIf { it.name == name && it.revision == revision }
    val current = (skill as? SkillLoad.Loaded)?.detail?.takeIf { it.name == name }
    val now = OffsetDateTime.now()

    Column(modifier = Modifier.fillMaxSize().background(colors.canvas)) {
        TopBar(
            onBack = onBack,
            title = skillRevisionTitle(revision),
            subtitle = shown?.let { "$name · ${skillRevisionMeta(it.author, it.at, now)}" },
        )
        when {
            load is SkillRevisionLoad.Failed -> ErrorState((load as SkillRevisionLoad.Failed).message, onRetry = { viewModel.openRevision(name, revision) })
            skill is SkillLoad.Failed -> ErrorState((skill as SkillLoad.Failed).message, onRetry = { viewModel.open(name) })
            shown == null || current == null -> SkeletonRows(rows = 4)
            else -> {
                val sameAsCurrent = shown.sha256 == current.sha256
                val views = buildList {
                    if (shown.revision > 1) add(RevisionView.Changes)
                    if (!sameAsCurrent) add(RevisionView.AgainstCurrent)
                    add(RevisionView.Full)
                }
                val view = views[selected.coerceIn(0, views.lastIndex)]
                LaunchedEffect(view, shown.revision, current.revision) {
                    when (view) {
                        RevisionView.Changes -> viewModel.loadDiff(name, shown.revision - 1, shown.revision)
                        RevisionView.AgainstCurrent -> viewModel.loadDiff(name, current.revision, shown.revision)
                        RevisionView.Full -> Unit
                    }
                }
                Column(
                    modifier = Modifier
                        .weight(1f)
                        .verticalScroll(scroll)
                        .padding(horizontal = ScreenPadding, vertical = 4.dp),
                ) {
                    val notes = listOfNotNull(
                        "这一版停用了技能。".takeIf { shown.retired },
                        when {
                            shown.revision == current.revision -> "这是现在的版本。"
                            sameAsCurrent -> "这一版的内容和现在的${skillRevisionTitle(current.revision)}一样。"
                            else -> null
                        },
                    )
                    if (notes.isNotEmpty()) {
                        Text(notes.joinToString(""), style = AcornTheme.type.footnote, color = colors.inkSoft)
                        Spacer(Modifier.height(12.dp))
                    }
                    if (views.size > 1) {
                        SegmentedControl(views.map { it.label }, selected = views.indexOf(view), onSelect = { selected = it })
                        Spacer(Modifier.height(14.dp))
                    }
                    when (view) {
                        RevisionView.Changes -> {
                            Caption("标 + 的是这一版加上的，标 − 的是这一版删掉的。")
                            DiffSection(diff, name, shown.revision - 1, shown.revision, onRetry = { viewModel.loadDiff(name, shown.revision - 1, shown.revision) })
                        }
                        RevisionView.AgainstCurrent -> {
                            Caption("标 + 的是这一版才有的，标 − 的是现在的${skillRevisionTitle(current.revision)}才有的。回滚后，现在的内容会变成这一版。")
                            DiffSection(diff, name, current.revision, shown.revision, onRetry = { viewModel.loadDiff(name, current.revision, shown.revision) })
                        }
                        RevisionView.Full -> {
                            shown.problem?.let {
                                Panel(color = colors.dangerSoft) { Text("这一版解析不了：$it", style = AcornTheme.type.callout, color = colors.ink) }
                                Spacer(Modifier.height(12.dp))
                            }
                            SkillContent(shown.instruction, shown.markdown)
                            if (shown.files.isNotEmpty()) {
                                SkillSectionTitle("附带文件")
                                SkillFiles(shown.files)
                            }
                        }
                    }
                    shown.runId?.let { runId ->
                        Spacer(Modifier.height(16.dp))
                        AcornButton("打开写下这一版的对话", { viewModel.openRun(runId, onOpenThread) }, kind = ButtonKind.Quiet, compact = true)
                    }
                    Spacer(Modifier.height(24.dp))
                }
                if (!sameAsCurrent || current.retired) {
                    Column(
                        modifier = Modifier
                            .navigationBarsPadding()
                            .padding(horizontal = ScreenPadding, vertical = 12.dp),
                        verticalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        (change as? SkillChange.Failed)?.let {
                            InlineError(it.message, action = "知道了", onAction = viewModel::dismissChangeError)
                        }
                        AcornButton(
                            "回滚到这一版",
                            { confirmRollback = true },
                            modifier = Modifier.fillMaxWidth(),
                            kind = ButtonKind.Secondary,
                            loading = change == SkillChange.Saving,
                        )
                    }
                } else {
                    (change as? SkillChange.Failed)?.let {
                        InlineError(it.message, modifier = Modifier.navigationBarsPadding().padding(horizontal = ScreenPadding, vertical = 12.dp), action = "知道了", onAction = viewModel::dismissChangeError)
                    }
                }
            }
        }
    }

    if (confirmRollback && shown != null && current != null) {
        AcornDialog(
            title = "回滚到${skillRevisionTitle(shown.revision)}？",
            body = "这一版的 SKILL.md 和附带文件会另存为新的一版，技能随之启用，接下来的对话用它。现在的${skillRevisionTitle(current.revision)}和其他版本都留在历史里。",
            confirm = "回滚",
            onConfirm = {
                confirmRollback = false
                viewModel.rollback(shown, onDone = onBack)
            },
            onDismiss = { confirmRollback = false },
        )
    }
}

@Composable
private fun Caption(text: String) {
    Text(text, style = AcornTheme.type.footnote, color = AcornTheme.colors.inkSoft)
    Spacer(Modifier.height(10.dp))
}

@Composable
private fun DiffSection(load: SkillDiffLoad, name: String, from: Long, to: Long, onRetry: () -> Unit) {
    val diff = (load as? SkillDiffLoad.Loaded)?.diff?.takeIf { it.name == name && it.from == from && it.to == to }
    when {
        load is SkillDiffLoad.Failed -> InlineError(load.message, action = "重试", onAction = onRetry)
        diff == null -> SkeletonRows(rows = 3)
        else -> SkillDiffView(diff)
    }
}

/** The server's diff, file by file: what changed in each, as hunks of marked lines. */
@Composable
fun SkillDiffView(diff: SkillDiff) {
    val colors = AcornTheme.colors
    if (diff.files.isEmpty()) {
        Text("两版的内容一样。", style = AcornTheme.type.callout, color = colors.inkSoft)
        return
    }
    Column(verticalArrangement = Arrangement.spacedBy(18.dp)) {
        diff.files.forEach { file ->
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(file.path, style = AcornTheme.type.mono, color = colors.ink, modifier = Modifier.weight(1f))
                    Spacer(Modifier.width(8.dp))
                    Tag(fileChangeLabel(file.change))
                }
                when {
                    file.binary -> Text("不是文本文件，不显示差异", style = AcornTheme.type.footnote, color = colors.inkFaint)
                    file.hunks.isEmpty() -> Text("空文件", style = AcornTheme.type.footnote, color = colors.inkFaint)
                    else -> file.hunks.forEach { hunk ->
                        val start = if (file.change == SkillFileDiff.Change.removed) hunk.oldStart else hunk.newStart
                        Text("第 $start 行起", style = AcornTheme.type.caption, color = colors.inkFaint)
                        DiffView(hunk.lines.map { DiffLine(diffKind(it.kind), it.text) })
                    }
                }
            }
        }
    }
}

private fun diffKind(kind: SkillDiffLine.Kind): DiffKind = when (kind) {
    SkillDiffLine.Kind.same -> DiffKind.Same
    SkillDiffLine.Kind.added -> DiffKind.Added
    SkillDiffLine.Kind.removed -> DiffKind.Removed
}
