package io.ycvk.acorn.feature.chat

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.background
import androidx.compose.foundation.border
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
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.Stop
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.jeziellago.compose.markdowntext.MarkdownText
import io.ycvk.acorn.core.theme.Accent
import io.ycvk.acorn.core.theme.AccentDim
import io.ycvk.acorn.core.theme.Bg
import io.ycvk.acorn.core.theme.Border
import io.ycvk.acorn.core.theme.Danger
import io.ycvk.acorn.core.theme.Info
import io.ycvk.acorn.core.theme.OnAccent
import io.ycvk.acorn.core.theme.Surface
import io.ycvk.acorn.core.theme.SurfaceVariant
import io.ycvk.acorn.core.theme.TextPrimary
import io.ycvk.acorn.core.theme.TextSecondary

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ChatScreen(
    threadId: String?,
    onBack: () -> Unit,
    viewModel: ChatViewModel = hiltViewModel(),
) {
    val messages by viewModel.messages.collectAsStateWithLifecycle()
    val chatState by viewModel.chatState.collectAsStateWithLifecycle()
    val error by viewModel.error.collectAsStateWithLifecycle()

    LaunchedEffect(threadId) {
        if (threadId != null) viewModel.loadThread(threadId)
    }

    var input by remember { mutableStateOf("") }
    val listState = rememberLazyListState()

    val tailIndex = remember(messages, chatState) {
        val streamingShown = chatState.isStreaming || chatState.assistantText.isNotBlank()
        (messages.lastIndex + if (streamingShown) 1 else 0).coerceAtLeast(0)
    }
    LaunchedEffect(tailIndex, chatState.assistantText, chatState.assistantReasoning) {
        if (tailIndex >= 0) listState.animateScrollToItem(tailIndex)
    }

    val streaming = chatState.isStreaming

    Scaffold(
        containerColor = Bg,
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(
                            "chat",
                            style = MaterialTheme.typography.headlineSmall,
                            color = TextPrimary,
                        )
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            val statusColor = if (streaming) Accent else TextSecondary
                            val statusText = if (streaming) "running" else "idle"
                            Box(
                                modifier = Modifier
                                    .size(6.dp)
                                    .clip(CircleShape)
                                    .background(statusColor),
                            )
                            Spacer(Modifier.width(4.dp))
                            Text(
                                statusText,
                                style = MaterialTheme.typography.labelSmall,
                                color = statusColor,
                            )
                        }
                    }
                },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(
                            Icons.AutoMirrored.Filled.ArrowBack,
                            contentDescription = "back",
                            tint = TextSecondary,
                        )
                    }
                },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = Surface,
                    titleContentColor = TextPrimary,
                ),
            )
        },
        bottomBar = {
            Composer(
                input = input,
                onInputChange = { input = it },
                streaming = streaming,
                onSend = {
                    if (input.isNotBlank()) {
                        viewModel.sendMessage(input)
                        input = ""
                    }
                },
                onStop = { viewModel.interruptRun() },
            )
        },
    ) { padding ->
        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .background(Bg)
                .padding(padding),
            state = listState,
            contentPadding = PaddingValues(horizontal = 12.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            items(messages, key = { it.id }) { message ->
                when (message) {
                    is ChatMessage.User -> UserBubble(message.text)
                    is ChatMessage.Assistant -> AssistantBubble(
                        text = message.text,
                        reasoning = message.reasoning,
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
                    )
                }
            }

            items(chatState.activities, key = { it.id }) { activity ->
                ActivityRow(activity.label)
            }

            error?.let {
                item(key = "__error__") {
                    Text(
                        "! $it",
                        style = MaterialTheme.typography.bodySmall.copy(
                            fontFamily = FontFamily.Monospace,
                        ),
                        color = Danger,
                    )
                }
            }
        }
    }
}

@Composable
private fun Composer(
    input: String,
    onInputChange: (String) -> Unit,
    streaming: Boolean,
    onSend: () -> Unit,
    onStop: () -> Unit,
) {
    Surface(
        color = Surface,
        tonalElevation = 0.dp,
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            OutlinedTextField(
                value = input,
                onValueChange = onInputChange,
                modifier = Modifier.weight(1f),
                placeholder = {
                    Text(
                        "message…",
                        style = MaterialTheme.typography.bodyMedium.copy(
                            fontFamily = FontFamily.Monospace,
                        ),
                        color = TextSecondary,
                    )
                },
                maxLines = 4,
                enabled = !streaming,
                shape = RoundedCornerShape(4.dp),
                textStyle = MaterialTheme.typography.bodyMedium.copy(color = TextPrimary),
                colors = OutlinedTextFieldDefaults.colors(
                    focusedBorderColor = Accent,
                    unfocusedBorderColor = Border,
                    cursorColor = Accent,
                    focusedTextColor = TextPrimary,
                    unfocusedTextColor = TextPrimary,
                ),
            )
            Spacer(Modifier.width(8.dp))
            if (streaming) {
                IconButton(onClick = onStop) {
                    Icon(Icons.Filled.Stop, contentDescription = "stop", tint = Danger)
                }
            } else {
                IconButton(
                    onClick = onSend,
                    enabled = input.isNotBlank(),
                ) {
                    Icon(
                        Icons.AutoMirrored.Filled.Send,
                        contentDescription = "send",
                        tint = if (input.isNotBlank()) Accent else TextSecondary,
                    )
                }
            }
        }
    }
}

@Composable
private fun UserBubble(text: String) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.End,
    ) {
        Surface(
            color = AccentDim,
            shape = RoundedCornerShape(topStart = 4.dp, topEnd = 0.dp, bottomStart = 4.dp, bottomEnd = 4.dp),
            modifier = Modifier.widthIn(max = 280.dp),
        ) {
            SelectionContainer {
                Text(
                    text = text,
                    modifier = Modifier.padding(horizontal = 12.dp, vertical = 10.dp),
                    style = MaterialTheme.typography.bodyLarge,
                    color = OnAccent,
                )
            }
        }
    }
}

@Composable
private fun AssistantBubble(text: String, reasoning: String? = null) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.Start,
    ) {
        Surface(
            color = Surface,
            shape = RoundedCornerShape(topStart = 0.dp, topEnd = 4.dp, bottomStart = 4.dp, bottomEnd = 4.dp),
            modifier = Modifier.widthIn(max = 320.dp),
            border = androidx.compose.foundation.BorderStroke(1.dp, Border),
        ) {
            Column(modifier = Modifier.padding(12.dp)) {
                reasoning?.let { ReasoningBlock(it) }
                if (reasoning != null) Spacer(Modifier.size(4.dp))
                MarkdownText(
                    markdown = text,
                    style = MaterialTheme.typography.bodyLarge.copy(
                        color = TextPrimary,
                    ),
                )
            }
        }
    }
}

@Composable
private fun StreamingAssistantBubble(text: String, reasoning: String?, isStreaming: Boolean) {
    if (text.isBlank() && !isStreaming) return

    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.Start,
    ) {
        Surface(
            color = Surface,
            shape = RoundedCornerShape(topStart = 0.dp, topEnd = 4.dp, bottomStart = 4.dp, bottomEnd = 4.dp),
            modifier = Modifier.widthIn(max = 320.dp),
            border = androidx.compose.foundation.BorderStroke(1.dp, Border),
        ) {
            Column(modifier = Modifier.padding(12.dp)) {
                reasoning?.let {
                    Text(
                        "▸ thinking",
                        style = MaterialTheme.typography.labelSmall,
                        color = TextSecondary,
                    )
                    Text(
                        it,
                        style = MaterialTheme.typography.bodySmall,
                        color = TextSecondary,
                        maxLines = 3,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Spacer(Modifier.size(4.dp))
                }
                if (text.isNotBlank()) {
                    val display = if (isStreaming) "$text ▌" else text
                    MarkdownText(
                        markdown = display,
                        style = MaterialTheme.typography.bodyLarge.copy(
                            color = TextPrimary,
                        ),
                    )
                }
                if (isStreaming && text.isBlank()) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(16.dp),
                        strokeWidth = 2.dp,
                        color = Accent,
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
            modifier = Modifier.clickable { expanded = !expanded },
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                if (expanded) "▾ thinking" else "▸ thinking",
                style = MaterialTheme.typography.labelSmall,
                color = TextSecondary,
            )
        }
        AnimatedVisibility(visible = expanded) {
            Text(
                reasoning,
                style = MaterialTheme.typography.bodySmall,
                color = TextSecondary,
            )
        }
    }
}

@Composable
private fun ActivityRow(label: String) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .background(SurfaceVariant)
            .padding(horizontal = 12.dp, vertical = 8.dp),
    ) {
        Box(
            modifier = Modifier
                .width(3.dp)
                .height(16.dp)
                .background(Info),
        )
        Spacer(Modifier.width(8.dp))
        Text(
            text = label,
            style = MaterialTheme.typography.bodySmall,
            color = TextPrimary,
        )
    }
}
