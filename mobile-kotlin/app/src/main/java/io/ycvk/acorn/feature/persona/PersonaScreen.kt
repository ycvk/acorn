package io.ycvk.acorn.feature.persona

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.Persona
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornButton
import io.ycvk.acorn.core.ui.AcornDialog
import io.ycvk.acorn.core.ui.ButtonKind
import io.ycvk.acorn.core.ui.DiffView
import io.ycvk.acorn.core.ui.ErrorState
import io.ycvk.acorn.core.ui.Hairline
import io.ycvk.acorn.core.ui.InlineError
import io.ycvk.acorn.core.ui.Markdown
import io.ycvk.acorn.core.ui.Panel
import io.ycvk.acorn.core.ui.RevisionRow
import io.ycvk.acorn.core.ui.ScreenPadding
import io.ycvk.acorn.core.ui.SegmentedControl
import io.ycvk.acorn.core.ui.SkeletonRows
import io.ycvk.acorn.core.ui.TopBar
import io.ycvk.acorn.core.ui.lineDiff
import java.time.OffsetDateTime

/** The persona: who Acorn is and how it talks, its history, and the editor. */
@Composable
fun PersonaScreen(
    viewModel: PersonaViewModel,
    onBack: () -> Unit,
    onOpenRevision: (Long) -> Unit,
) {
    val load by viewModel.persona.collectAsStateWithLifecycle()
    val draft by viewModel.draft.collectAsStateWithLifecycle()
    val save by viewModel.save.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { viewModel.load() }

    val current = (load as? PersonaLoad.Loaded)?.current
    val editing = draft
    if (editing != null && current != null) {
        PersonaEditor(editing, current, save, viewModel)
        return
    }

    val scroll = rememberScrollState()
    Column(modifier = Modifier.fillMaxSize().background(AcornTheme.colors.canvas)) {
        TopBar(onBack = onBack, title = "人格")
        when (val state = load) {
            PersonaLoad.Loading -> SkeletonRows(rows = 4)
            is PersonaLoad.Failed -> ErrorState(state.message, onRetry = viewModel::load)
            is PersonaLoad.Loaded -> {
                Column(modifier = Modifier.weight(1f).verticalScroll(scroll)) {
                    PersonaBody(state, viewModel.deviceId, onOpenRevision)
                }
                AcornButton(
                    "编辑",
                    viewModel::startEditing,
                    modifier = Modifier
                        .fillMaxWidth()
                        .navigationBarsPadding()
                        .padding(horizontal = ScreenPadding, vertical = 12.dp),
                )
            }
        }
    }
}

@Composable
private fun PersonaBody(state: PersonaLoad.Loaded, thisDevice: String?, onOpenRevision: (Long) -> Unit) {
    val colors = AcornTheme.colors
    val now = OffsetDateTime.now()
    val current = state.current
    Column(modifier = Modifier.padding(start = ScreenPadding, end = ScreenPadding, top = 4.dp)) {
        Text(
            "Acorn 每次开始工作时都会读这段话，它决定 Acorn 是谁、怎么跟你说话。只有你能改它。",
            style = AcornTheme.type.footnote,
            color = colors.inkSoft,
        )
        Spacer(Modifier.height(14.dp))
        Panel {
            Markdown(current.content)
        }
        Spacer(Modifier.height(8.dp))
        Text(
            "${revisionTitle(current.revision)} · ${revisionMeta(current.author.value, current.deviceId, thisDevice, current.savedAt, now)}",
            style = AcornTheme.type.caption,
            color = colors.inkFaint,
        )
        Text("历史", style = AcornTheme.type.footnote, color = colors.inkFaint, modifier = Modifier.padding(top = 28.dp, bottom = 4.dp))
    }
    state.revisions.forEachIndexed { index, revision ->
        if (index > 0) Hairline(inset = ScreenPadding)
        RevisionRow(
            title = revisionTitle(revision.revision),
            meta = revisionMeta(revision.author.value, revision.deviceId, thisDevice, revision.savedAt, now),
            current = revision.revision == current.revision,
            onClick = { onOpenRevision(revision.revision) },
        )
    }
    Spacer(Modifier.height(24.dp))
}

@Composable
private fun PersonaEditor(draft: PersonaDraft, current: Persona, save: PersonaSave, viewModel: PersonaViewModel) {
    val colors = AcornTheme.colors
    val state = draftState(draft.text, current.content)
    var confirmDiscard by rememberSaveable { mutableStateOf(false) }
    val leave = { if (state == DraftState.Unchanged && draft.mine == null) viewModel.cancelEditing() else confirmDiscard = true }
    BackHandler(onBack = leave)

    Column(modifier = Modifier.fillMaxSize().background(colors.canvas).imePadding()) {
        TopBar(onBack = leave, title = "编辑人格", subtitle = "基于${revisionTitle(draft.baseRevision)}")
        Column(
            modifier = Modifier
                .weight(1f)
                .verticalScroll(rememberScrollState())
                .padding(horizontal = ScreenPadding),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            BasicTextField(
                value = draft.text,
                onValueChange = viewModel::onDraftChange,
                textStyle = AcornTheme.type.reading.copy(color = colors.ink),
                cursorBrush = SolidColor(colors.accent),
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(min = 240.dp)
                    .clip(AcornShapes.small)
                    .background(colors.surfaceMuted)
                    .padding(14.dp),
            )
            Text(
                when (state) {
                    DraftState.Empty -> "人格不能为空"
                    DraftState.TooLong -> "超过 8 KB 了，删短一些才能保存（${draftSize(draft.text)}）"
                    else -> draftSize(draft.text)
                },
                style = AcornTheme.type.caption,
                color = if (state == DraftState.Empty || state == DraftState.TooLong) colors.danger else colors.inkFaint,
            )
            draft.mine?.let { mine ->
                Panel(color = colors.warningSoft) {
                    Text("你刚才写的", style = AcornTheme.type.footnote, color = colors.warningInk)
                    Spacer(Modifier.height(6.dp))
                    Text(mine, style = AcornTheme.type.callout, color = colors.ink, maxLines = 8, overflow = TextOverflow.Ellipsis)
                    Spacer(Modifier.height(10.dp))
                    AcornButton("用我写的替换上面的内容", viewModel::useMine, kind = ButtonKind.Secondary, compact = true)
                }
            }
            Spacer(Modifier.height(8.dp))
        }
        Column(
            modifier = Modifier
                .navigationBarsPadding()
                .padding(horizontal = ScreenPadding, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            (save as? PersonaSave.Failed)?.let {
                InlineError(it.message, action = "知道了", onAction = viewModel::dismissSaveError)
            }
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                AcornButton("取消", leave, modifier = Modifier.weight(1f), kind = ButtonKind.Secondary, enabled = save != PersonaSave.Saving)
                AcornButton(
                    "保存",
                    viewModel::saveDraft,
                    modifier = Modifier.weight(1f),
                    enabled = state == DraftState.Ready,
                    loading = save == PersonaSave.Saving,
                )
            }
        }
    }

    if (confirmDiscard) {
        AcornDialog(
            title = "放弃这次修改？",
            body = "改过的内容不会保存，人格保持${revisionTitle(current.revision)}。",
            confirm = "放弃",
            dismiss = "继续编辑",
            destructive = true,
            onConfirm = {
                confirmDiscard = false
                viewModel.cancelEditing()
            },
            onDismiss = { confirmDiscard = false },
        )
    }
}

/** One saved revision: its content, how it differs from the one in use, and restoring it. */
@Composable
fun PersonaRevisionScreen(
    revision: Long,
    viewModel: PersonaViewModel,
    onBack: () -> Unit,
) {
    val load by viewModel.revision.collectAsStateWithLifecycle()
    val persona by viewModel.persona.collectAsStateWithLifecycle()
    val save by viewModel.save.collectAsStateWithLifecycle()
    LaunchedEffect(revision) {
        viewModel.openRevision(revision)
        if (persona !is PersonaLoad.Loaded) viewModel.load()
    }
    val colors = AcornTheme.colors
    val scroll = rememberScrollState()
    var view by rememberSaveable { mutableIntStateOf(0) }
    var confirmRestore by rememberSaveable { mutableStateOf(false) }
    val shown = (load as? RevisionLoad.Loaded)?.revision?.takeIf { it.revision == revision }
    val current = (persona as? PersonaLoad.Loaded)?.current
    val now = OffsetDateTime.now()

    Column(modifier = Modifier.fillMaxSize().background(colors.canvas)) {
        TopBar(
            onBack = onBack,
            title = revisionTitle(revision),
            subtitle = shown?.let { revisionMeta(it.author.value, it.deviceId, viewModel.deviceId, it.savedAt, now) },
        )
        when {
            load is RevisionLoad.Failed -> ErrorState((load as RevisionLoad.Failed).message, onRetry = { viewModel.openRevision(revision) })
            persona is PersonaLoad.Failed -> ErrorState((persona as PersonaLoad.Failed).message, onRetry = viewModel::load)
            shown == null || current == null -> SkeletonRows(rows = 4)
            else -> {
                val inUse = shown.sha256 == current.sha256
                Column(
                    modifier = Modifier
                        .weight(1f)
                        .verticalScroll(scroll)
                        .padding(horizontal = ScreenPadding, vertical = 4.dp),
                ) {
                    if (inUse) {
                        Text(
                            if (shown.revision == current.revision) "这是现在用的版本。" else "这一版的内容和现在用的${revisionTitle(current.revision)}一样。",
                            style = AcornTheme.type.footnote,
                            color = colors.inkSoft,
                        )
                        Spacer(Modifier.height(12.dp))
                        Panel { Markdown(shown.content) }
                    } else {
                        SegmentedControl(listOf("和现在的对比", "全文"), selected = view, onSelect = { view = it })
                        Spacer(Modifier.height(14.dp))
                        if (view == 0) {
                            Text(
                                "标 + 的是这一版才有的，标 − 的是现在的${revisionTitle(current.revision)}才有的。",
                                style = AcornTheme.type.footnote,
                                color = colors.inkSoft,
                            )
                            Spacer(Modifier.height(10.dp))
                            DiffView(lineDiff(current.content, shown.content))
                        } else {
                            Panel { Markdown(shown.content) }
                        }
                    }
                    Spacer(Modifier.height(24.dp))
                }
                if (!inUse) {
                    Column(
                        modifier = Modifier
                            .navigationBarsPadding()
                            .padding(horizontal = ScreenPadding, vertical = 12.dp),
                        verticalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        (save as? PersonaSave.Failed)?.let {
                            InlineError(it.message, action = "知道了", onAction = viewModel::dismissSaveError)
                        }
                        AcornButton(
                            "恢复到这一版",
                            { confirmRestore = true },
                            modifier = Modifier.fillMaxWidth(),
                            kind = ButtonKind.Secondary,
                            loading = save == PersonaSave.Saving,
                        )
                    }
                }
            }
        }
    }

    if (confirmRestore && shown != null && current != null) {
        AcornDialog(
            title = "恢复到${revisionTitle(shown.revision)}？",
            body = "这一版的内容会另存为新的一版，接下来的对话用它。现在的${revisionTitle(current.revision)}和其他版本都留在历史里。",
            confirm = "恢复",
            onConfirm = {
                confirmRestore = false
                viewModel.restore(shown, onRestored = onBack)
            },
            onDismiss = { confirmRestore = false },
        )
    }
}
