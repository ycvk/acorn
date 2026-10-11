package io.ycvk.acorn.feature.chat

import android.os.Build
import android.widget.Toast
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.scaleIn
import androidx.compose.animation.scaleOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
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
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Redo
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.DarkMode
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material.icons.outlined.Lightbulb
import androidx.compose.material.icons.outlined.MoreHoriz
import androidx.compose.material.icons.outlined.NotificationsNone
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material.icons.outlined.Visibility
import androidx.compose.material.icons.outlined.WbSunny
import androidx.compose.material.icons.rounded.ArrowDownward
import androidx.compose.material.icons.rounded.ArrowUpward
import androidx.compose.material.icons.rounded.Stop
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
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
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.core.sse.ActivityItem
import io.ycvk.acorn.core.sse.ActivityKind
import io.ycvk.acorn.core.sse.ChatHeaderStatus
import io.ycvk.acorn.core.sse.StepStatus
import io.ycvk.acorn.core.sse.ToolStep
import io.ycvk.acorn.core.sse.headerStatus
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.IconAction
import io.ycvk.acorn.core.ui.InlineError
import io.ycvk.acorn.core.ui.Markdown
import io.ycvk.acorn.core.ui.OnReturnToForeground
import io.ycvk.acorn.core.ui.ScreenPadding
import io.ycvk.acorn.core.ui.TopBar
import io.ycvk.acorn.core.ui.rememberHaptics
import io.ycvk.acorn.feature.approvals.ApprovalCard
import io.ycvk.acorn.feature.approvals.ApprovalsViewModel
import io.ycvk.acorn.feature.approvals.PendingActionSheet
import io.ycvk.acorn.feature.approvals.optionDecision
import io.ycvk.acorn.feature.knowledge.AttachmentImage
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
    val pending by approvals.pendingActions.collectAsStateWithLifecycle()
    val deciding by approvals.deciding.collectAsStateWithLifecycle()
    val selected by approvals.selectedAction.collectAsStateWithLifecycle()

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
            showDivider = listState.canScrollBackward,
        ) {
            if (currentThread != null) {
                Box {
                    IconAction(Icons.Outlined.MoreHoriz, "更多", { menu = true })
                    DropdownMenu(
                        expanded = menu,
                        onDismissRequest = { menu = false },
                        containerColor = colors.surface,
                        shape = AcornShapes.medium,
                    ) {
                        DropdownMenuItem(
                            text = { Text("删除对话", style = AcornTheme.type.callout, color = colors.danger) },
                            onClick = {
                                menu = false
                                confirmDelete = true
                            },
                        )
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
                contentPadding = PaddingValues(start = ScreenPadding, end = ScreenPadding, top = 8.dp, bottom = 16.dp),
                verticalArrangement = Arrangement.spacedBy(18.dp),
            ) {
                if (messages.isEmpty() && !streaming && currentThread == null) {
                    item(key = "__welcome__") { Welcome(Modifier.fillParentMaxHeight(0.8f)) }
                }
                items(messages, key = { it.id }) { message ->
                    val itemModifier = Modifier.animateItem()
                    when (message) {
                        is ChatMessage.User -> UserBubble(message.text, itemModifier)
                        is ChatMessage.Assistant -> AssistantMessage(message.text, message.reasoning, message.steps, streaming = false, modifier = itemModifier)
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
                if (streaming || chatState.assistantText.isNotBlank() || chatState.steps.isNotEmpty()) {
                    item(key = "__streaming__") {
                        AssistantMessage(
                            text = chatState.assistantText,
                            reasoning = chatState.assistantReasoning.ifBlank { null },
                            steps = chatState.steps,
                            streaming = streaming,
                            modifier = Modifier.animateItem(),
                        )
                    }
                }
                val activities = chatState.activities.filter { it.kind == ActivityKind.ResumeRequested || threadPending.isEmpty() }
                items(activities, key = { it.id }) { ActivityLine(it, Modifier.animateItem()) }
                items(threadPending, key = { "pending:${it.actionId}" }) { action ->
                    ApprovalCard(
                        action = action,
                        deciding = deciding == action.actionId,
                        onDecide = { option -> approvals.decideAction(action.actionId, optionDecision(option), option.id, null) },
                        onOpen = { approvals.loadActionDetail(action.actionId) },
                        modifier = Modifier.animateItem(),
                    )
                }
                error?.let {
                    item(key = "__error__") {
                        InlineError(it, Modifier.animateItem(), action = "知道了", onAction = viewModel::dismissError)
                    }
                }
                if (error == null && !streaming) {
                    chatState.failure?.let {
                        item(key = "__failure__") { RunFailure(it, onDismiss = viewModel::dismissError, modifier = Modifier.animateItem()) }
                    }
                }
            }

            // Qualified: the enclosing Column's scoped overload would otherwise win.
            androidx.compose.animation.AnimatedVisibility(
                visible = !follow && !atBottom && messages.isNotEmpty(),
                enter = fadeIn() + scaleIn(initialScale = 0.8f),
                exit = fadeOut() + scaleOut(targetScale = 0.8f),
                modifier = Modifier
                    .align(Alignment.BottomCenter)
                    .padding(bottom = 12.dp),
            ) {
                Box(
                    modifier = Modifier
                        .size(38.dp)
                        .clip(CircleShape)
                        .background(colors.surface)
                        .border(0.5.dp, colors.lineStrong, CircleShape)
                        .clickable {
                            follow = true
                            scope.launch {
                                val last = listState.layoutInfo.totalItemsCount - 1
                                if (last >= 0) listState.animateScrollToItem(last, scrollOffset = Int.MAX_VALUE / 2)
                            }
                        },
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(Icons.Rounded.ArrowDownward, contentDescription = "回到底部", tint = colors.inkSoft, modifier = Modifier.size(18.dp))
                }
            }
        }

        Composer(
            input = input,
            onInputChange = { input = it },
            streaming = streaming,
            autoFocus = threadId == null,
            onSend = {
                val text = input.trim()
                if (text.isNotEmpty()) {
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
            onDismiss = approvals::clearSelection,
            onDecide = { option -> approvals.decideAction(detail.actionId, optionDecision(option), option.id, null) },
        )
    }

    if (confirmDelete) {
        AlertDialog(
            onDismissRequest = { confirmDelete = false },
            title = { Text("删除这个对话？") },
            text = { Text("对话里的消息会被删除，无法恢复。") },
            confirmButton = {
                TextButton(onClick = {
                    confirmDelete = false
                    viewModel.deleteThread(onBack)
                }) { Text("删除", color = colors.danger) }
            },
            dismissButton = { TextButton(onClick = { confirmDelete = false }) { Text("取消") } },
        )
    }
}

@Composable
private fun Welcome(modifier: Modifier = Modifier) {
    Column(
        modifier = modifier.fillMaxWidth(),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Icon(Icons.Outlined.AutoAwesome, contentDescription = null, tint = AcornTheme.colors.accent, modifier = Modifier.size(28.dp))
        Spacer(Modifier.height(14.dp))
        Text("想让我做点什么？", style = AcornTheme.type.title, color = AcornTheme.colors.ink)
        Spacer(Modifier.height(6.dp))
        Text(
            "问问题、记一个约定、让我盯着某个网页，\n或者把要整理的东西交给我。",
            style = AcornTheme.type.footnote,
            color = AcornTheme.colors.inkSoft,
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

private val UserBubbleShape = RoundedCornerShape(topStart = 18.dp, topEnd = 18.dp, bottomEnd = 6.dp, bottomStart = 18.dp)

@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun UserBubble(text: String, modifier: Modifier = Modifier) {
    val copy = rememberCopy()
    Row(modifier = modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
        Text(
            text = text,
            style = AcornTheme.type.body,
            color = AcornTheme.colors.ink,
            modifier = Modifier
                .widthIn(max = 300.dp)
                .clip(UserBubbleShape)
                .background(AcornTheme.colors.surfaceMuted)
                .combinedClickable(onClick = {}, onLongClick = { copy(text) })
                .padding(horizontal = 14.dp, vertical = 10.dp),
        )
    }
}

@Composable
private fun AssistantMessage(text: String, reasoning: String?, steps: List<ToolStep>, streaming: Boolean, modifier: Modifier = Modifier) {
    val colors = AcornTheme.colors
    val copy = rememberCopy()
    Column(modifier = modifier.fillMaxWidth()) {
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
        } else if (streaming && steps.none { it.status == StepStatus.Running }) {
            TypingDots()
        }
        if (!streaming && text.isNotBlank()) {
            Box(
                modifier = Modifier
                    .padding(top = 4.dp)
                    .offset(x = (-8).dp)
                    .size(32.dp)
                    .clip(CircleShape)
                    .clickable(onClickLabel = "复制") { copy(text) },
                contentAlignment = Alignment.Center,
            ) {
                Icon(Icons.Outlined.ContentCopy, contentDescription = "复制", tint = colors.inkFaint, modifier = Modifier.size(16.dp))
            }
        }
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
            Icon(Icons.Outlined.ExpandMore, contentDescription = null, tint = colors.inkFaint, modifier = Modifier.size(16.dp).rotate(rotation))
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

@Composable
private fun TypingDots() {
    val transition = rememberInfiniteTransition(label = "typing")
    Row(modifier = Modifier.padding(vertical = 10.dp), horizontalArrangement = Arrangement.spacedBy(5.dp)) {
        repeat(3) { i ->
            val alpha by transition.animateFloat(
                initialValue = 0.25f,
                targetValue = 1f,
                animationSpec = infiniteRepeatable(tween(480, delayMillis = i * 150), RepeatMode.Reverse),
                label = "dot_$i",
            )
            Box(
                modifier = Modifier
                    .size(7.dp)
                    .alpha(alpha)
                    .background(AcornTheme.colors.accent, CircleShape),
            )
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
                .clip(AcornShapes.medium)
                .border(0.5.dp, colors.line, AcornShapes.medium)
                .padding(horizontal = 14.dp, vertical = 10.dp),
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
    onSend: () -> Unit,
    onStop: () -> Unit,
) {
    val colors = AcornTheme.colors
    val haptics = rememberHaptics()
    val focus = remember { FocusRequester() }
    LaunchedEffect(autoFocus) { if (autoFocus) focus.requestFocus() }
    val shape = RoundedCornerShape(24.dp)
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .navigationBarsPadding()
            .padding(start = 12.dp, end = 12.dp, top = 6.dp, bottom = 10.dp)
            .clip(shape)
            .background(colors.surface)
            .border(0.5.dp, colors.lineStrong, shape)
            .padding(start = 18.dp, end = 6.dp, top = 6.dp, bottom = 6.dp),
        verticalAlignment = Alignment.Bottom,
    ) {
        Box(
            modifier = Modifier
                .weight(1f)
                .heightIn(min = 36.dp)
                .padding(vertical = 7.dp),
            contentAlignment = Alignment.CenterStart,
        ) {
            if (input.isEmpty()) Text("发消息给 Acorn", style = AcornTheme.type.body, color = colors.inkFaint)
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
        Spacer(Modifier.width(8.dp))
        val canSend = input.isNotBlank()
        AnimatedContent(
            targetState = streaming,
            transitionSpec = { (fadeIn() + scaleIn(initialScale = 0.7f)) togetherWith (fadeOut() + scaleOut(targetScale = 0.7f)) },
            label = "send",
        ) { running ->
            Box(
                modifier = Modifier
                    .size(36.dp)
                    .clip(CircleShape)
                    .background(if (running || canSend) colors.ink else colors.surfaceMuted)
                    .clickable(enabled = running || canSend) {
                        if (running) {
                            haptics.tick()
                            onStop()
                        } else {
                            haptics.confirm()
                            onSend()
                        }
                    },
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    if (running) Icons.Rounded.Stop else Icons.Rounded.ArrowUpward,
                    contentDescription = if (running) "停止" else "发送",
                    tint = if (running || canSend) colors.canvas else colors.inkFaint,
                    modifier = Modifier.size(if (running) 16.dp else 20.dp),
                )
            }
        }
    }
}

@Composable
private fun rememberCopy(): (String) -> Unit {
    val clipboard = LocalClipboardManager.current
    val context = LocalContext.current
    val haptics = rememberHaptics()
    return remember(clipboard, context) {
        { text: String ->
            clipboard.setText(AnnotatedString(text))
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
            .clip(AcornShapes.medium)
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
