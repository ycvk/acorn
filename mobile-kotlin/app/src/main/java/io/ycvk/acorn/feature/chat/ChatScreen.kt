package io.ycvk.acorn.feature.chat

import androidx.compose.animation.animateContentSize
import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.ime
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.union
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material.icons.filled.Stop
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.nestedscroll.NestedScrollConnection
import androidx.compose.ui.input.nestedscroll.NestedScrollSource
import androidx.compose.ui.input.nestedscroll.nestedScroll
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.text.PlatformTextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.jeziellago.compose.markdowntext.MarkdownText
import io.ycvk.acorn.core.theme.AetherBackground
import io.ycvk.acorn.core.theme.AetherMessageBubble
import io.ycvk.acorn.core.theme.AetherOnPrimary
import io.ycvk.acorn.core.theme.AetherOnSurface
import io.ycvk.acorn.core.theme.AetherOnSurfaceVariant
import io.ycvk.acorn.core.theme.AetherOutlineSoft
import io.ycvk.acorn.core.theme.AetherPrimary
import io.ycvk.acorn.core.theme.AetherSurface
import io.ycvk.acorn.core.theme.AetherSurfaceHigh
import io.ycvk.acorn.core.theme.gradientBackground
// Aether design DNA
private val ChatGptMotionEasing = CubicBezierEasing(0.22f, 0.84f, 0.18f, 1f)
private val MessageBubbleShape = RoundedCornerShape(20.dp)

// ─── Screen ──────────────────────────────────────────────────────────────────
@OptIn(androidx.compose.foundation.ExperimentalFoundationApi::class)
@Composable
fun ChatScreen(
    threadId: String?,
    onBack: () -> Unit,
    onOpenDrawer: (() -> Unit)? = null,
    viewModel: ChatViewModel = hiltViewModel(),
) {
    val messages by viewModel.messages.collectAsStateWithLifecycle()
    val chatState by viewModel.chatState.collectAsStateWithLifecycle()
    val error by viewModel.error.collectAsStateWithLifecycle()
    val threadTitle by viewModel.threadTitle.collectAsStateWithLifecycle()

    LaunchedEffect(threadId) {
        if (threadId != null) viewModel.loadThread(threadId)
    }


    var input by remember { mutableStateOf("") }
    val listState = rememberLazyListState()
    val streaming = chatState.isStreaming
    val keyboard = LocalSoftwareKeyboardController.current

    // Smart auto-scroll: follow new content only when already near bottom.
    var shouldAutoFollow by remember { mutableStateOf(true) }
    val scrollConnection = remember {
        object : NestedScrollConnection {
            override fun onPreScroll(available: Offset, source: NestedScrollSource): Offset {
                if (available.y < -1f) shouldAutoFollow = false
                return Offset.Zero
            }
        }
    }
    // Restore auto-follow when user scrolls back to bottom.
    LaunchedEffect(listState) {
        snapshotFlow {
            val info = listState.layoutInfo
            val total = info.totalItemsCount
            if (total == 0) return@snapshotFlow true
            val lastVisible = info.visibleItemsInfo.lastOrNull()
            val lastIdx = lastVisible?.index ?: -1
            val distFromBottom = info.viewportEndOffset - (
                (lastVisible?.offset ?: 0) + (lastVisible?.size ?: 0)
            )
            lastIdx >= total - 1 && distFromBottom >= -32
        }.collect { atBottom ->
            if (atBottom) shouldAutoFollow = true
        }
    }
    // Scroll to bottom when new content arrives and auto-follow is on.
    LaunchedEffect(messages.size, chatState.assistantText) {
        if (shouldAutoFollow) {
            val last = listState.layoutInfo.totalItemsCount - 1
            if (last >= 0) listState.scrollToItem(last)
        }
    }
    Box(
        modifier = Modifier
            .fillMaxSize()
            .gradientBackground(),
    ) {
        // Message list
        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .nestedScroll(scrollConnection),
            state = listState,
            contentPadding = PaddingValues(
                top = 96.dp,
                bottom = 120.dp,
                start = 16.dp,
                end = 16.dp,
            ),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            items(messages, key = { it.id }) { message ->
                when (message) {
                    is ChatMessage.User -> UserBubble(
                        text = message.text,
                        modifier = Modifier.animateItem(),
                    )
                    is ChatMessage.Assistant -> AssistantBubble(
                        text = message.text,
                        reasoning = message.reasoning,
                        modifier = Modifier.animateItem(),
                    )
                }
            }

            val showStreaming = streaming || chatState.assistantText.isNotBlank()
            if (showStreaming) {
                item(key = "__streaming__") {
                    StreamingAssistantBubble(
                        text = chatState.assistantText,
                        reasoning = chatState.assistantReasoning.ifBlank { null },
                        isStreaming = streaming,
                        modifier = Modifier.animateItem(),
                    )
                }
            }

            items(chatState.activities, key = { it.id }) { activity ->
                ActivityRow(activity.label, modifier = Modifier.animateItem())
            }

            error?.let {
                item(key = "__error__") {
                    ErrorBanner(it, modifier = Modifier.animateItem())
                }
            }
        }

        // Top bar overlay
        ChatTopBar(
            title = threadTitle ?: "thread",
            streaming = streaming,
            onBack = onBack,
            onOpenDrawer = onOpenDrawer,
            modifier = Modifier
                .align(Alignment.TopCenter)
                .fillMaxWidth(),
        )

        // Composer
        Composer(
            input = input,
            onInputChange = { input = it },
            streaming = streaming,
            onSend = {
                if (input.isNotBlank()) {
                    viewModel.sendMessage(input)
                    input = ""
                    keyboard?.hide()
                }
            },
            onStop = { viewModel.interruptRun() },
            modifier = Modifier
                .align(Alignment.BottomCenter)
                .fillMaxWidth()
                .windowInsetsPadding(WindowInsets.ime.union(WindowInsets.navigationBars)),
        )
    }
}

// ─── Top bar ──────────────────────────────────────────────────────────────────

@Composable
private fun ChatTopBar(
    title: String,
    streaming: Boolean,
    onBack: () -> Unit,
    onOpenDrawer: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier.background(
            Brush.verticalGradient(
                colorStops = arrayOf(
                    0.0f to AetherBackground.copy(alpha = 0.92f),
                    0.7f to AetherBackground.copy(alpha = 0.72f),
                    1.0f to Color.Transparent,
                ),
            ),
        ),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .statusBarsPadding()
                .padding(horizontal = 12.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            if (onOpenDrawer != null) {
                CircleButton(
                    icon = Icons.Filled.Menu,
                    contentDescription = "menu",
                    onClick = onOpenDrawer,
                )
                Spacer(Modifier.width(8.dp))
            }
            CircleButton(
                icon = Icons.AutoMirrored.Filled.ArrowBack,
                contentDescription = "back",
                onClick = onBack,
            )
            Spacer(Modifier.width(12.dp))
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    title,
                    style = MaterialTheme.typography.titleMedium.copy(
                        fontWeight = FontWeight.SemiBold,
                    ),
                    color = AetherOnSurface,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Row(verticalAlignment = Alignment.CenterVertically) {
                    val statusColor = if (streaming) AetherPrimary else AetherOnSurfaceVariant
                    val statusText = if (streaming) "running" else "idle"
                    Box(
                        modifier = Modifier
                            .size(6.dp)
                            .background(statusColor, CircleShape),
                    )
                    Spacer(Modifier.width(6.dp))
                    Text(
                        statusText,
                        style = MaterialTheme.typography.labelSmall,
                        color = statusColor,
                    )
                }
            }
        }
    }
}

@Composable
private fun CircleButton(
    icon: ImageVector,
    contentDescription: String,
    onClick: () -> Unit,
    size: Dp = 40.dp,
) {
    Box(
        modifier = Modifier
            .size(size)
            .background(AetherSurface.copy(alpha = 0.96f), CircleShape)
            .clickable(onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            icon,
            contentDescription = contentDescription,
            tint = AetherOnSurface,
            modifier = Modifier.size(20.dp),
        )
    }
}

// ─── Composer ─────────────────────────────────────────────────────────────────

@Composable
private fun Composer(
    input: String,
    onInputChange: (String) -> Unit,
    streaming: Boolean,
    onSend: () -> Unit,
    onStop: () -> Unit,
    modifier: Modifier = Modifier,
) {
    var focused by remember { mutableStateOf(false) }
    val cornerRadius by animateDpAsState(
        targetValue = if (focused) 28.dp else 26.dp,
        animationSpec = tween(260, easing = ChatGptMotionEasing),
        label = "composer_corner",
    )
    Surface(
        modifier = modifier
            .padding(horizontal = 12.dp, vertical = 8.dp),
        shape = RoundedCornerShape(cornerRadius),
        color = AetherSurface,
        shadowElevation = 8.dp,
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            androidx.compose.foundation.text.BasicTextField(
                value = input,
                onValueChange = onInputChange,
                modifier = Modifier
                    .weight(1f)
                    .padding(horizontal = 12.dp, vertical = 10.dp)
                    .onFocusChanged { focused = it.isFocused },
                enabled = !streaming,
                textStyle = MaterialTheme.typography.bodyLarge.copy(
                    color = AetherOnSurface,
                    platformStyle = PlatformTextStyle(includeFontPadding = false),
                ),
                cursorBrush = Brush.verticalGradient(listOf(AetherPrimary, AetherPrimary)),
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Send),
                keyboardActions = KeyboardActions(onSend = { onSend() }),
                decorationBox = { innerTextField ->
                    if (input.isEmpty()) {
                        Text(
                            "message…",
                            style = MaterialTheme.typography.bodyLarge,
                            color = AetherOnSurfaceVariant.copy(alpha = 0.6f),
                        )
                    }
                    innerTextField()
                },
            )
            Spacer(Modifier.width(8.dp))
            if (streaming) {
                Box(
                    modifier = Modifier
                        .size(36.dp)
                        .background(AetherOnSurfaceVariant.copy(alpha = 0.15f), CircleShape)
                        .clickable(onClick = onStop),
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(
                        Icons.Filled.Stop,
                        contentDescription = "stop",
                        tint = AetherOnSurface,
                        modifier = Modifier.size(16.dp),
                    )
                }
            } else {
                val sendEnabled = input.isNotBlank()
                Box(
                    modifier = Modifier
                        .size(36.dp)
                        .background(
                            if (sendEnabled) AetherPrimary else AetherOutlineSoft,
                            CircleShape,
                        )
                        .clickable(enabled = sendEnabled, onClick = onSend),
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(
                        Icons.AutoMirrored.Filled.Send,
                        contentDescription = "send",
                        tint = if (sendEnabled) AetherOnPrimary else AetherOnSurfaceVariant,
                        modifier = Modifier.size(16.dp),
                    )
                }
            }
        }
    }
}

// ─── Message bubbles ──────────────────────────────────────────────────────────

@Composable
private fun UserBubble(text: String, modifier: Modifier = Modifier) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.End,
    ) {
        Surface(
            color = AetherMessageBubble,
            shape = MessageBubbleShape,
            modifier = modifier
                .widthIn(max = 280.dp)
                .animateContentSize(
                    animationSpec = tween(280, easing = ChatGptMotionEasing),
                ),
        ) {
            SelectionContainer {
                Text(
                    text = text,
                    modifier = Modifier.padding(14.dp),
                    style = MaterialTheme.typography.bodyLarge,
                    color = AetherOnSurface,
                )
            }
        }
    }
}

@Composable
private fun AssistantBubble(text: String, reasoning: String? = null, modifier: Modifier = Modifier) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.Start,
    ) {
        Column(
            modifier = modifier
                .widthIn(max = 320.dp)
                .animateContentSize(
                    animationSpec = tween(280, easing = ChatGptMotionEasing),
                ),
        ) {
            Text(
                "acorn",
                style = MaterialTheme.typography.labelSmall,
                color = AetherOnSurfaceVariant.copy(alpha = 0.6f),
            )
            Spacer(Modifier.height(4.dp))
            reasoning?.let {
                ReasoningBlock(it)
                Spacer(Modifier.height(8.dp))
            }
            SelectionContainer {
                MarkdownText(
                    markdown = text,
                    style = MaterialTheme.typography.bodyLarge.copy(color = AetherOnSurface),
                )
            }
        }
    }
}

@Composable
private fun StreamingAssistantBubble(text: String, reasoning: String?, isStreaming: Boolean, modifier: Modifier = Modifier) {
    if (text.isBlank() && !isStreaming) return

    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.Start,
    ) {
        Column(
            modifier = modifier
                .widthIn(max = 320.dp)
                .animateContentSize(
                    animationSpec = tween(280, easing = ChatGptMotionEasing),
                ),
        ) {
            Text(
                "acorn",
                style = MaterialTheme.typography.labelSmall,
                color = AetherOnSurfaceVariant.copy(alpha = 0.6f),
            )
            Spacer(Modifier.height(4.dp))
            reasoning?.let {
                ReasoningBlock(it)
                Spacer(Modifier.height(8.dp))
            }
            if (text.isNotBlank()) {
                SelectionContainer {
                    MarkdownText(
                        markdown = text,
                        style = MaterialTheme.typography.bodyLarge.copy(color = AetherOnSurface),
                    )
                }
            } else if (isStreaming) {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(14.dp),
                        strokeWidth = 2.dp,
                        color = AetherPrimary,
                    )
                    Text(
                        "thinking",
                        style = MaterialTheme.typography.labelSmall,
                        color = AetherOnSurfaceVariant.copy(alpha = 0.6f),
                    )
                }
            }
        }
    }
}

@Composable
private fun ReasoningBlock(reasoning: String) {
    var expanded by remember { mutableStateOf(false) }
    Column {
        Row(
            modifier = Modifier
                .clickable { expanded = !expanded }
                .padding(vertical = 2.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                "thinking",
                style = MaterialTheme.typography.labelSmall,
                color = AetherOnSurfaceVariant.copy(alpha = 0.6f),
            )
        }
        if (expanded) {
            Text(
                reasoning,
                style = MaterialTheme.typography.bodySmall,
                color = AetherOnSurfaceVariant,
            )
        }
    }
}

// ─── Activity / Error ─────────────────────────────────────────────────────────

@Composable
private fun ActivityRow(label: String, modifier: Modifier = Modifier) {
    Surface(
        color = AetherSurfaceHigh,
        shape = RoundedCornerShape(16.dp),
        modifier = modifier.fillMaxWidth(),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                modifier = Modifier
                    .size(width = 3.dp, height = 16.dp)
                    .background(AetherPrimary),
            )
            Spacer(Modifier.width(12.dp))
            Text(
                text = label,
                style = MaterialTheme.typography.bodySmall,
                color = AetherOnSurface,
            )
        }
    }
}

@Composable
private fun ErrorBanner(message: String, modifier: Modifier = Modifier) {
    Surface(
        color = AetherSurfaceHigh,
        shape = RoundedCornerShape(16.dp),
        modifier = modifier.fillMaxWidth(),
    ) {
        Text(
            message,
            modifier = Modifier.padding(12.dp),
            style = MaterialTheme.typography.bodySmall,
            color = AetherPrimary,
        )
    }
}
