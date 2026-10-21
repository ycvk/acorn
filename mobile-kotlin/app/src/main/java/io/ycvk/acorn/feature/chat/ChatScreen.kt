package io.ycvk.acorn.feature.chat

import android.content.ClipData
import android.os.Build
import android.widget.Toast
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.scaleIn
import androidx.compose.animation.scaleOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.LocalIndication
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyItemScope
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Redo
import androidx.compose.material.icons.outlined.AttachFile
import androidx.compose.material.icons.outlined.DarkMode
import androidx.compose.material.icons.outlined.Image
import androidx.compose.material.icons.outlined.Lightbulb
import androidx.compose.material.icons.outlined.NotificationsNone
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material.icons.outlined.Visibility
import androidx.compose.material.icons.outlined.WbSunny
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.nestedscroll.NestedScrollConnection
import androidx.compose.ui.input.nestedscroll.NestedScrollSource
import androidx.compose.ui.input.nestedscroll.nestedScroll
import androidx.compose.ui.platform.ClipEntry
import androidx.compose.ui.platform.LocalClipboard
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.core.sse.ActivityItem
import io.ycvk.acorn.core.sse.ActivityKind
import io.ycvk.acorn.core.sse.ChatHeaderStatus
import io.ycvk.acorn.core.sse.StepStatus
import io.ycvk.acorn.core.sse.ToolStep
import io.ycvk.acorn.core.sse.headerStatus
import io.ycvk.acorn.core.theme.AcornMotion
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornDialog
import io.ycvk.acorn.core.ui.AcornIcons
import io.ycvk.acorn.core.ui.AcornMenu
import io.ycvk.acorn.core.ui.AcornMenuItem
import io.ycvk.acorn.core.ui.IconAction
import io.ycvk.acorn.core.ui.InlineError
import io.ycvk.acorn.core.ui.Markdown
import io.ycvk.acorn.core.ui.OnReturnToForeground
import io.ycvk.acorn.core.ui.OrbState
import io.ycvk.acorn.core.ui.ScreenPadding
import io.ycvk.acorn.core.ui.ThinkingOrb
import io.ycvk.acorn.core.ui.TopBar
import io.ycvk.acorn.core.ui.borderBeam
import io.ycvk.acorn.core.ui.hairline
import io.ycvk.acorn.core.ui.metalRing
import io.ycvk.acorn.core.ui.pressScale
import io.ycvk.acorn.core.ui.raised
import io.ycvk.acorn.core.ui.rememberHaptics
import io.ycvk.acorn.core.ui.shimmer
import io.ycvk.acorn.feature.approvals.ApprovalCard
import io.ycvk.acorn.feature.approvals.ApprovalsViewModel
import io.ycvk.acorn.feature.approvals.PendingActionSheet
import io.ycvk.acorn.feature.knowledge.AttachmentImage
import io.ycvk.acorn.feature.views.ViewActions
import io.ycvk.acorn.feature.views.ViewCards
import kotlinx.coroutines.launch

@Composable
fun ChatScreen(
    threadId: String?,
    onBack: () -> Unit,
    openActionId: String? = null,
    viewModel: ChatViewModel = hiltViewModel(),
    approvals: ApprovalsViewModel = hiltViewModel(),
) {
    val messages by viewModel.messages.collectAsStateWithLifecycle()
    val chatState by viewModel.chatState.collectAsStateWithLifecycle()
    val error by viewModel.error.collectAsStateWithLifecycle()
    val title by viewModel.threadTitle.collectAsStateWithLifecycle()
    val currentThread by viewModel.threadId.collectAsStateWithLifecycle()
    val restored by viewModel.restoredInput.collectAsStateWithLifecycle()
    val attachments by viewModel.attachments.collectAsStateWithLifecycle()
    val uploading by viewModel.uploading.collectAsStateWithLifecycle()
    val fileBusy by viewModel.fileBusy.collectAsStateWithLifecycle()
    val sendingAction by viewModel.sendingAction.collectAsStateWithLifecycle()
    val pending by approvals.pendingActions.collectAsStateWithLifecycle()
    val deciding by approvals.deciding.collectAsStateWithLifecycle()
    val selected by approvals.selectedAction.collectAsStateWithLifecycle()
    val approvalError by approvals.error.collectAsStateWithLifecycle()

    LaunchedEffect(threadId) { viewModel.loadThread(threadId) }
    OnReturnToForeground {
        viewModel.refresh()
        approvals.loadPendingActions()
    }
    // Opened from an approval notification: bring the decision up right away.
    LaunchedEffect(openActionId) { openActionId?.let(approvals::loadActionDetail) }
    // A run that pauses for the owner shows its decision here as well as on Today.
    LaunchedEffect(chatState.activities.size, currentThread) {
        if (currentThread != null) approvals.loadPendingActions()
    }

    var input by rememberSaveable { mutableStateOf("") }
    LaunchedEffect(restored) {
        restored?.let {
            input = it
            viewModel.consumeRestoredInput()
        }
    }

    val listState = rememberLazyListState()
    val scope = rememberCoroutineScope()
    val streaming = chatState.isStreaming
    val threadPending = pending.filter { it.threadId == currentThread }
    // A view's buttons wait, like the composer, while a run of the thread is going.
    val viewPicks = remember(currentThread) { mutableStateMapOf<String, List<String>>() }
    val viewActions = ViewActions(
        idle = !streaming,
        sending = sendingAction,
        picks = viewPicks,
        onPick = { id, picked -> viewPicks[id] = picked },
        onSend = viewModel::sendViewAction,
    )

    // Follow new content only while the reader is at the bottom.
    var follow by remember { mutableStateOf(true) }
    val scrollConnection = remember {
        object : NestedScrollConnection {
            override fun onPreScroll(available: Offset, source: NestedScrollSource): Offset {
                if (available.y > 1f) follow = false
                return Offset.Zero
            }
        }
    }
    val atBottom by remember { derivedStateOf { !listState.canScrollForward } }
    LaunchedEffect(listState) {
        snapshotFlow { !listState.canScrollForward }.collect { if (it) follow = true }
    }
    LaunchedEffect(messages.size, chatState.assistantText, chatState.assistantReasoning, chatState.steps, streaming, chatState.activities.size, threadPending.size) {
        if (follow) {
            val last = listState.layoutInfo.totalItemsCount - 1
            if (last >= 0) listState.scrollToItem(last, scrollOffset = Int.MAX_VALUE / 2)
        }
    }

    var menu by remember { mutableStateOf(false) }
    var confirmDelete by remember { mutableStateOf(false) }
    val context = LocalContext.current
    val pickPhotos = rememberLauncherForActivityResult(ActivityResultContracts.PickMultipleVisualMedia(MAX_MESSAGE_FILES)) { uris ->
        viewModel.addAttachments(uris.map { pendingAttachment(context, it, photo = true) })
    }
    val pickFiles = rememberLauncherForActivityResult(ActivityResultContracts.OpenMultipleDocuments()) { uris ->
        viewModel.addAttachments(uris.map { pendingAttachment(context, it, photo = false) })
    }
    var saving by remember { mutableStateOf<FileView?>(null) }
    val saveFile = rememberLauncherForActivityResult(SaveFileContract()) { destination ->
        val file = saving
        saving = null
        if (destination != null && file != null) {
            viewModel.saveFile(file, destination) { Toast.makeText(context, "已保存 ${file.name}", Toast.LENGTH_SHORT).show() }
        }
    }
    val openThreadFile: (FileView) -> Unit = { file ->
        viewModel.withFile(file) { local ->
            if (!openFile(context, local, file.mimeType)) viewModel.reportError("手机上没有能打开 ${file.name} 的应用，可以点下载按钮保存到手机")
        }
    }
    val saveThreadFile: (FileView) -> Unit = { file ->
        saving = file
        saveFile.launch(file)
    }
    val status = chatState.headerStatus()
    val colors = AcornTheme.colors

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(colors.canvas)
            .imePadding(),
    ) {
        TopBar(
            onBack = onBack,
            title = title ?: if (currentThread == null) "新对话" else "",
            subtitle = status.label.takeIf { it.isNotEmpty() },
            subtitleColor = if (status == ChatHeaderStatus.Waiting) colors.warning else colors.success,
        ) {
            if (currentThread != null) {
                Box {
                    IconAction(AcornIcons.More, "更多", { menu = true })
                    AcornMenu(expanded = menu, onDismiss = { menu = false }) {
                        AcornMenuItem("删除对话", onClick = {
                            menu = false
                            confirmDelete = true
                        }, danger = true, icon = AcornIcons.Trash)
                    }
                }
            }
        }

        Box(modifier = Modifier.weight(1f)) {
            LazyColumn(
                modifier = Modifier
                    .fillMaxSize()
                    .nestedScroll(scrollConnection),
                state = listState,
                contentPadding = PaddingValues(start = ScreenPadding, end = ScreenPadding, top = 16.dp, bottom = 16.dp),
                verticalArrangement = Arrangement.spacedBy(22.dp),
            ) {
                if (messages.isEmpty() && !streaming && currentThread == null) {
                    item(key = "__welcome__") { Welcome(Modifier.fillParentMaxHeight(0.8f)) }
                }
                items(messages, key = { it.id }) { message ->
                    val itemModifier = rowAnimation()
                    when (message) {
                        is ChatMessage.User -> UserBubble(message.text, itemModifier) {
                            MessageFiles(message.files, currentThread, viewModel.connection(), fileBusy, openThreadFile, saveThreadFile, alignment = Alignment.End)
                        }
                        is ChatMessage.Assistant -> AssistantMessage(message.text, message.reasoning, message.steps, streaming = false, modifier = itemModifier) {
                            ViewCards(message.views, currentThread, viewModel.connection(), viewActions, Modifier.padding(top = 10.dp))
                            MessageFiles(message.files, currentThread, viewModel.connection(), fileBusy, openThreadFile, saveThreadFile, Modifier.padding(top = 8.dp))
                        }
                        is ChatMessage.Wake -> InputNote(wakeLabel(message.source), wakeIcon(message.source), message.text, itemModifier)
                        is ChatMessage.Capture -> InputNote("从手机分享", Icons.Outlined.Share, message.text, itemModifier) {
                            val profile = viewModel.connection()
                            if (message.image != null && profile != null) {
                                Spacer(Modifier.height(8.dp))
                                AttachmentImage(message.image, "分享的图片", profile)
                            }
                        }
                    }
                }
                if (streaming || chatState.assistantText.isNotBlank() || chatState.steps.isNotEmpty() || chatState.views.isNotEmpty()) {
                    item(key = "__streaming__") {
                        AssistantMessage(
                            text = chatState.assistantText,
                            reasoning = chatState.assistantReasoning.ifBlank { null },
                            steps = chatState.steps,
                            streaming = streaming,
                            modifier = rowAnimation(),
                        ) {
                            ViewCards(chatState.views, currentThread, viewModel.connection(), viewActions, Modifier.padding(top = 10.dp))
                        }
                    }
                }
                val activities = chatState.activities.filter { it.kind == ActivityKind.ResumeRequested || threadPending.isEmpty() }
                items(activities, key = { it.id }) { ActivityLine(it, rowAnimation()) }
                items(threadPending, key = { "pending:${it.actionId}" }) { action ->
                    ApprovalCard(
                        action = action,
                        deciding = deciding == action.actionId,
                        onDecide = { approvals.decideAction(action.actionId, it) },
                        onOpen = { approvals.loadActionDetail(action.actionId) },
                        modifier = rowAnimation(),
                    )
                }
                approvalError?.let {
                    item(key = "__approval_error__") {
                        InlineError(it, rowAnimation(), action = "知道了", onAction = approvals::dismissError)
                    }
                }
                error?.let {
                    item(key = "__error__") {
                        InlineError(it, rowAnimation(), action = "知道了", onAction = viewModel::dismissError)
                    }
                }
                if (error == null && !streaming) {
                    chatState.failure?.let {
                        item(key = "__failure__") { RunFailure(it, onDismiss = viewModel::dismissError, modifier = rowAnimation()) }
                    }
                }
            }

            // Qualified: the enclosing Column's scoped overload would otherwise win.
            androidx.compose.animation.AnimatedVisibility(
                visible = !follow && !atBottom && messages.isNotEmpty(),
                enter = fadeIn(AcornMotion.enter(AcornMotion.SHORT)) + scaleIn(AcornMotion.pop(), initialScale = 0.6f),
                exit = fadeOut(AcornMotion.exit()) + scaleOut(AcornMotion.exit(), targetScale = 0.6f),
                modifier = Modifier
                    .align(Alignment.BottomCenter)
                    .padding(bottom = 12.dp),
            ) {
                val interaction = remember { MutableInteractionSource() }
                Box(
                    modifier = Modifier
                        .pressScale(interaction, 0.9f)
                        .raised(CircleShape)
                        .size(38.dp)
                        .clip(CircleShape)
                        .background(colors.surface)
                        .clickable(interaction, LocalIndication.current) {
                            follow = true
                            scope.launch {
                                val last = listState.layoutInfo.totalItemsCount - 1
                                if (last >= 0) listState.animateScrollToItem(last, scrollOffset = Int.MAX_VALUE / 2)
                            }
                        },
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(AcornIcons.ArrowDown, contentDescription = "回到底部", tint = colors.inkSoft, modifier = Modifier.size(17.dp))
                }
            }
        }

        if (attachments.isNotEmpty()) {
            PendingAttachments(attachments, enabled = uploading == null, onRemove = viewModel::removeAttachment)
        }
        uploading?.let {
            Text(it, style = AcornTheme.type.caption, color = colors.inkFaint, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(horizontal = 24.dp, vertical = 4.dp))
        }
        Composer(
            input = input,
            onInputChange = { input = it },
            streaming = streaming,
            autoFocus = threadId == null,
            hasAttachments = attachments.isNotEmpty(),
            onPickPhotos = { pickPhotos.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly)) },
            onPickFiles = { pickFiles.launch(arrayOf("*/*")) },
            onSend = {
                val text = input.trim()
                if (text.isNotEmpty() || attachments.isNotEmpty()) {
                    follow = true
                    viewModel.sendMessage(text)
                    input = ""
                }
            },
            onStop = viewModel::interruptRun,
        )
    }

    selected?.let { detail ->
        PendingActionSheet(
            detail = detail,
            deciding = deciding == detail.actionId,
            error = approvalError,
            onDismiss = approvals::clearSelection,
            onDismissError = approvals::dismissError,
            onDecide = { approvals.decideAction(detail.actionId, it) },
        )
    }

    if (confirmDelete) {
        AcornDialog(
            title = "删除这个对话？",
            body = "对话和其中的文件会被删除，Acorn 也会忘掉其中说过的内容，无法恢复。",
            confirm = "删除",
            destructive = true,
            onConfirm = {
                confirmDelete = false
                viewModel.deleteThread(onBack)
            },
            onDismiss = { confirmDelete = false },
        )
    }
}

/**
 * Rows move into place but do not fade. The list follows a streaming reply
 * with scrollToItem, and each such jump resets item animations, so a fade-in
 * replayed on every row with every token and the whole chat flickered.
 */
private fun LazyItemScope.rowAnimation(): Modifier = Modifier.animateItem(fadeInSpec = null, fadeOutSpec = null)

@Composable
private fun Welcome(modifier: Modifier = Modifier) {
    val colors = AcornTheme.colors
    Column(
        modifier = modifier.fillMaxWidth(),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        ThinkingOrb(OrbState.Listening, size = 72.dp, speed = 0.45f)
        Spacer(Modifier.height(20.dp))
        Text("想让我做点什么？", style = AcornTheme.type.title, color = colors.ink)
        Spacer(Modifier.height(8.dp))
        Text(
            "问问题、记一个约定、让我盯着某个网页，\n或者把要整理的东西交给我。",
            style = AcornTheme.type.footnote,
            color = colors.inkSoft,
            textAlign = TextAlign.Center,
        )
    }
}

private fun wakeLabel(source: WakeSource) = when (source) {
    WakeSource.Commitment -> "约定提醒"
    WakeSource.Watch -> "追踪有更新"
    WakeSource.Briefing -> "早安简报"
    WakeSource.Night -> "夜思"
    WakeSource.Wander -> "游思"
}

private fun wakeIcon(source: WakeSource) = when (source) {
    WakeSource.Commitment -> Icons.Outlined.NotificationsNone
    WakeSource.Watch -> Icons.Outlined.Visibility
    WakeSource.Briefing -> Icons.Outlined.WbSunny
    WakeSource.Night -> Icons.Outlined.DarkMode
    WakeSource.Wander -> Icons.Outlined.Lightbulb
}

/** The owner's words on a soft bubble at the right, its corner toward the edge kept tight. */
private val UserBubbleShape = RoundedCornerShape(topStart = 20.dp, topEnd = 20.dp, bottomEnd = 6.dp, bottomStart = 20.dp)

@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun UserBubble(text: String, modifier: Modifier = Modifier, files: @Composable () -> Unit = {}) {
    val colors = AcornTheme.colors
    val copy = rememberCopy()
    Column(
        modifier = modifier.fillMaxWidth().padding(start = 48.dp),
        horizontalAlignment = Alignment.End,
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        files()
        if (text.isNotEmpty()) {
            Text(
                text = text,
                style = AcornTheme.type.body,
                color = colors.ink,
                modifier = Modifier
                    .clip(UserBubbleShape)
                    .background(if (colors.isDark) colors.surfaceMuted else colors.surface)
                    .hairline(UserBubbleShape)
                    .combinedClickable(onClick = {}, onLongClick = { copy(text) })
                    .padding(horizontal = 16.dp, vertical = 11.dp),
            )
        }
    }
}

@Composable
private fun AssistantMessage(
    text: String,
    reasoning: String?,
    steps: List<ToolStep>,
    streaming: Boolean,
    modifier: Modifier = Modifier,
    handedOver: @Composable () -> Unit = {},
) {
    val colors = AcornTheme.colors
    val copy = rememberCopy()
    Column(modifier = modifier.fillMaxWidth().padding(end = 8.dp)) {
        if (steps.isNotEmpty()) {
            ToolSteps(steps, streaming)
            Spacer(Modifier.height(8.dp))
        }
        reasoning?.let {
            Reasoning(it)
            Spacer(Modifier.height(8.dp))
        }
        if (text.isNotBlank()) {
            SelectionContainer { Markdown(text) }
        }
        // While no step runs, the orb at the foot of the reply shows whether Acorn is still thinking or already writing.
        if (streaming && steps.none { it.status == StepStatus.Running }) {
            RunStatus(
                state = if (text.isBlank()) OrbState.Thinking else OrbState.Composing,
                label = if (text.isBlank()) "正在思考" else "正在回复",
                modifier = Modifier.padding(top = if (text.isBlank()) 2.dp else 10.dp),
            )
        }
        handedOver()
        if (!streaming && text.isNotBlank()) {
            IconAction(
                AcornIcons.Copy,
                "复制",
                { copy(text) },
                tint = colors.inkFaint,
                modifier = Modifier
                    .padding(top = 4.dp)
                    .offset(x = (-8).dp)
                    .size(30.dp),
            )
        }
    }
}

/** What a running reply is doing: its orb beside a label a light passes over. */
@Composable
private fun RunStatus(state: OrbState, label: String, modifier: Modifier = Modifier) {
    Row(modifier = modifier, verticalAlignment = Alignment.CenterVertically) {
        ThinkingOrb(state, size = 22.dp)
        Spacer(Modifier.width(10.dp))
        Text(label, style = AcornTheme.type.footnote, color = AcornTheme.colors.inkSoft, modifier = Modifier.shimmer(true))
    }
}

@Composable
private fun Reasoning(reasoning: String) {
    val colors = AcornTheme.colors
    var expanded by rememberSaveable { mutableStateOf(false) }
    val rotation by animateFloatAsState(if (expanded) 180f else 0f, label = "chevron")
    Column {
        Row(
            modifier = Modifier
                .clip(AcornShapes.small)
                .clickable { expanded = !expanded }
                .padding(vertical = 4.dp, horizontal = 2.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text("思考过程", style = AcornTheme.type.footnote, color = colors.inkFaint)
            Icon(AcornIcons.ChevronDown, contentDescription = null, tint = colors.inkFaint, modifier = Modifier.size(16.dp).rotate(rotation))
        }
        AnimatedVisibility(visible = expanded, enter = expandVertically() + fadeIn(), exit = shrinkVertically() + fadeOut()) {
            Row(modifier = Modifier.padding(top = 4.dp).height(IntrinsicSize.Min)) {
                Box(
                    Modifier
                        .width(2.dp)
                        .fillMaxHeight()
                        .background(colors.line, RoundedCornerShape(1.dp)),
                )
                Spacer(Modifier.width(12.dp))
                Text(reasoning, style = AcornTheme.type.footnote, color = colors.inkSoft)
            }
        }
    }
}

/** What started a run when the owner didn't type it: a wake, or something shared from the phone. */
@Composable
private fun InputNote(
    label: String,
    icon: ImageVector,
    text: String,
    modifier: Modifier = Modifier,
    attachment: @Composable () -> Unit = {},
) {
    val colors = AcornTheme.colors
    Column(modifier = modifier.fillMaxWidth()) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Icon(icon, contentDescription = null, tint = colors.inkFaint, modifier = Modifier.size(15.dp))
            Spacer(Modifier.width(6.dp))
            Text(label, style = AcornTheme.type.caption, color = colors.inkFaint)
        }
        Spacer(Modifier.height(6.dp))
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .clip(AcornShapes.large)
                .background(colors.well)
                .hairline(AcornShapes.large)
                .padding(horizontal = 14.dp, vertical = 12.dp),
        ) {
            if (text.isNotEmpty()) {
                Text(text, style = AcornTheme.type.footnote, color = colors.inkSoft, maxLines = 12)
            }
            attachment()
        }
    }
}

@Composable
private fun ActivityLine(activity: ActivityItem, modifier: Modifier = Modifier) {
    val colors = AcornTheme.colors
    val waiting = activity.kind != ActivityKind.ResumeRequested
    Row(modifier = modifier, verticalAlignment = Alignment.CenterVertically) {
        Icon(
            if (waiting) Icons.Outlined.NotificationsNone else Icons.AutoMirrored.Outlined.Redo,
            contentDescription = null,
            tint = if (waiting) colors.warning else colors.inkFaint,
            modifier = Modifier.size(15.dp),
        )
        Spacer(Modifier.width(6.dp))
        Text(activity.label, style = AcornTheme.type.footnote, color = if (waiting) colors.warningInk else colors.inkFaint)
    }
}

@Composable
private fun Composer(
    input: String,
    onInputChange: (String) -> Unit,
    streaming: Boolean,
    autoFocus: Boolean,
    hasAttachments: Boolean,
    onPickPhotos: () -> Unit,
    onPickFiles: () -> Unit,
    onSend: () -> Unit,
    onStop: () -> Unit,
) {
    val colors = AcornTheme.colors
    val haptics = rememberHaptics()
    val focus = remember { FocusRequester() }
    LaunchedEffect(autoFocus) { if (autoFocus) focus.requestFocus() }
    val shape = ComposerShape
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .navigationBarsPadding()
            .padding(start = 12.dp, end = 12.dp, top = 6.dp, bottom = 10.dp)
            .clip(shape)
            .background(colors.surface)
            .hairline(shape)
            .borderBeam(streaming, ComposerRadius)
            .padding(start = 16.dp, end = 8.dp, top = 14.dp, bottom = 8.dp),
    ) {
        Box(
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(min = 22.dp)
                .padding(end = 4.dp),
            contentAlignment = Alignment.CenterStart,
        ) {
            if (input.isEmpty()) Text("回复 Acorn…", style = AcornTheme.type.body, color = colors.inkFaint)
            BasicTextField(
                value = input,
                onValueChange = onInputChange,
                textStyle = AcornTheme.type.body.copy(color = colors.ink),
                cursorBrush = SolidColor(colors.accent),
                maxLines = 6,
                modifier = Modifier
                    .fillMaxWidth()
                    .focusRequester(focus),
            )
        }
        Spacer(Modifier.height(8.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            var attachMenu by remember { mutableStateOf(false) }
            Box {
                IconAction(AcornIcons.Plus, "添加图片或文件", { attachMenu = true }, filled = true, modifier = Modifier.offset(x = (-8).dp).size(36.dp))
                AcornMenu(expanded = attachMenu, onDismiss = { attachMenu = false }) {
                    AcornMenuItem("图片", onClick = {
                        attachMenu = false
                        onPickPhotos()
                    }, icon = Icons.Outlined.Image)
                    AcornMenuItem("文件", onClick = {
                        attachMenu = false
                        onPickFiles()
                    }, icon = Icons.Outlined.AttachFile)
                }
            }
            Spacer(Modifier.weight(1f))
            val canSend = input.isNotBlank() || hasAttachments
            val fill by animateColorAsState(
                when {
                    streaming -> colors.ink
                    canSend -> if (colors.isDark) colors.surfaceMuted else colors.ink
                    else -> colors.surfaceMuted
                },
                AcornMotion.standard(AcornMotion.SHORT),
                label = "send_fill",
            )
            val interaction = remember { MutableInteractionSource() }
            Box(
                modifier = Modifier
                    .pressScale(interaction, 0.9f)
                    .size(36.dp)
                    .then(if (canSend && !streaming) Modifier.metalRing(true) else Modifier)
                    .clip(CircleShape)
                    .background(fill)
                    .clickable(interaction, LocalIndication.current, enabled = streaming || canSend) {
                        if (streaming) {
                            haptics.tick()
                            onStop()
                        } else {
                            haptics.confirm()
                            onSend()
                        }
                    },
                contentAlignment = Alignment.Center,
            ) {
                // The arrow turns into a stop square while a reply runs.
                AnimatedContent(
                    targetState = streaming,
                    transitionSpec = {
                        (fadeIn(AcornMotion.enter(AcornMotion.SHORT)) + scaleIn(AcornMotion.pop(), initialScale = 0.4f)) togetherWith
                            (fadeOut(AcornMotion.exit()) + scaleOut(AcornMotion.exit(), targetScale = 0.4f))
                    },
                    label = "send",
                ) { running ->
                    Icon(
                        if (running) AcornIcons.Stop else AcornIcons.ArrowUp,
                        contentDescription = if (running) "停止" else "发送",
                        tint = when {
                            running -> colors.canvas
                            canSend -> if (colors.isDark) colors.ink else colors.canvas
                            else -> colors.inkFaint
                        },
                        modifier = Modifier.size(if (running) 18.dp else 17.dp),
                    )
                }
            }
        }
    }
}

private val ComposerRadius = 24.dp
private val ComposerShape = RoundedCornerShape(ComposerRadius)

@Composable
private fun rememberCopy(): (String) -> Unit {
    val clipboard = LocalClipboard.current
    val context = LocalContext.current
    val haptics = rememberHaptics()
    val scope = rememberCoroutineScope()
    return remember(clipboard, context, scope) {
        { text: String ->
            scope.launch { clipboard.setClipEntry(ClipEntry(ClipData.newPlainText("Acorn", text))) }
            haptics.confirm()
            // Android 13+ shows its own clipboard confirmation.
            if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) {
                Toast.makeText(context, "已复制", Toast.LENGTH_SHORT).show()
            }
        }
    }
}

/** A failed run: what happened, with the server's detail folded to a few lines until tapped. */
@Composable
private fun RunFailure(detail: String, onDismiss: () -> Unit, modifier: Modifier = Modifier) {
    val colors = AcornTheme.colors
    var expanded by rememberSaveable { mutableStateOf(false) }
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(AcornShapes.card)
            .background(colors.dangerSoft)
            .clickable { expanded = !expanded }
            .padding(start = 14.dp, end = 4.dp, top = 4.dp, bottom = 12.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("这次没能完成", style = AcornTheme.type.headline, color = colors.danger, modifier = Modifier.weight(1f))
            TextButton(onClick = onDismiss) { Text("知道了", style = AcornTheme.type.footnote, color = colors.danger) }
        }
        Text(
            detail.trim(),
            style = AcornTheme.type.mono.copy(fontSize = AcornTheme.type.caption.fontSize, lineHeight = AcornTheme.type.footnote.lineHeight),
            color = colors.danger.copy(alpha = 0.85f),
            maxLines = if (expanded) Int.MAX_VALUE else 3,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.padding(end = 10.dp),
        )
    }
}
