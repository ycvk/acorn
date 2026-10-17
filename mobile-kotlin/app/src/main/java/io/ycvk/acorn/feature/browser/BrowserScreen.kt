package io.ycvk.acorn.feature.browser

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.gestures.detectVerticalDragGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.ime
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Undo
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Tab
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.BrowserInputRequest
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornButton
import io.ycvk.acorn.core.ui.AcornIcons
import io.ycvk.acorn.core.ui.AcornMenu
import io.ycvk.acorn.core.ui.ButtonKind
import io.ycvk.acorn.core.ui.EmptyState
import io.ycvk.acorn.core.ui.ErrorState
import io.ycvk.acorn.core.ui.IconAction
import io.ycvk.acorn.core.ui.InlineError
import io.ycvk.acorn.core.ui.TopBar
import io.ycvk.acorn.core.ui.rememberHaptics

/**
 * The agent's own browser, live: the owner signs in to sites here so the agent
 * can use them. A tap on the picture taps the page, dragging scrolls it, and
 * the bar at the bottom types into the focused field.
 */
@Composable
fun BrowserScreen(openUrl: String?, onBack: () -> Unit, viewModel: BrowserViewModel = hiltViewModel()) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    // The page opens once per screen; a rotation must not reload what the owner is typing into.
    var opened by rememberSaveable { mutableStateOf(false) }
    DisposableEffect(viewModel) {
        viewModel.start(openUrl.takeUnless { opened })
        opened = true
        onDispose { viewModel.stop() }
    }
    val colors = AcornTheme.colors
    Column(modifier = Modifier.fillMaxSize().background(colors.canvas).imePadding()) {
        TopBar(
            onBack = onBack,
            title = state.current?.title?.takeIf { it.isNotBlank() && it != "about:blank" } ?: "Agent 的浏览器",
            subtitle = state.current?.url?.takeIf { it != "about:blank" }?.let(::pageHost),
        ) {
            TabMenu(state, onSelect = viewModel::select)
            IconAction(Icons.AutoMirrored.Outlined.Undo, "后退", viewModel::back, enabled = state.tab != null)
            IconAction(Icons.Outlined.Refresh, "刷新页面", viewModel::reload, enabled = state.tab != null)
        }
        AddressBar(enabled = state.tab != null, onOpen = viewModel::open)
        state.inputError?.let {
            InlineError(it, modifier = Modifier.padding(horizontal = 12.dp, vertical = 4.dp), action = "知道了", onAction = viewModel::dismissInputError)
        }
        // The page is laid out for the room it has with the keyboard down; with
        // the keyboard up the same picture shrinks to fit.
        val density = LocalDensity.current
        val ime = WindowInsets.ime
        Box(
            modifier = Modifier
                .weight(1f)
                .fillMaxWidth()
                .onSizeChanged { size ->
                    if (ime.getBottom(density) == 0) {
                        with(density) { viewModel.screen(phoneScreen(size.width.toDp().value, size.height.toDp().value)) }
                    }
                },
            contentAlignment = Alignment.TopCenter,
        ) {
            val frame = state.frame
            when {
                frame != null -> Frame(frame, onTap = viewModel::tap, onScroll = viewModel::scroll)
                state.status is BrowserStatus.Failed -> ErrorState((state.status as BrowserStatus.Failed).message, onRetry = { viewModel.start(null) })
                state.status == BrowserStatus.NoTabs -> EmptyState(AcornIcons.Globe, "浏览器没有打开的页面", "在上面输入网址打开一个页面")
                else -> EmptyState(AcornIcons.Globe, "正在连接 Agent 的浏览器", "画面出来后，点哪里就点网页的哪里")
            }
        }
        KeyBar(enabled = state.tab != null, onType = viewModel::type, onKey = viewModel::press)
    }
}

@Composable
private fun TabMenu(state: BrowserUiState, onSelect: (String) -> Unit) {
    var open by remember { mutableStateOf(false) }
    Box {
        IconAction(Icons.Outlined.Tab, "切换页面", { open = true }, enabled = state.tabs.size > 1)
        AcornMenu(expanded = open, onDismiss = { open = false }) {
            state.tabs.forEach { tab ->
                DropdownMenuItem(
                    text = {
                        Column {
                            Text(tab.title.ifBlank { tab.url }, style = AcornTheme.type.body, maxLines = 1, overflow = TextOverflow.Ellipsis)
                            Text(pageHost(tab.url), style = AcornTheme.type.caption, color = AcornTheme.colors.inkFaint, maxLines = 1)
                        }
                    },
                    onClick = {
                        open = false
                        onSelect(tab.id)
                    },
                )
            }
        }
    }
}

@Composable
private fun AddressBar(enabled: Boolean, onOpen: (String) -> Boolean) {
    val colors = AcornTheme.colors
    var typed by rememberSaveable { mutableStateOf("") }
    var invalid by remember { mutableStateOf(false) }
    val submit = {
        if (onOpen(typed)) {
            typed = ""
            invalid = false
        } else {
            invalid = true
        }
    }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 12.dp, vertical = 8.dp)
            .clip(AcornShapes.medium)
            .background(colors.surfaceMuted.copy(alpha = 0.6f))
            .border(1.dp, colors.line, AcornShapes.medium)
            .padding(horizontal = 12.dp, vertical = 9.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(modifier = Modifier.weight(1f)) {
            if (typed.isEmpty()) {
                Text(if (invalid) "不是有效的网址" else "输入网址，打开要登录的网站", style = AcornTheme.type.body, color = if (invalid) colors.danger else colors.inkFaint)
            }
            BasicTextField(
                value = typed,
                onValueChange = {
                    typed = it
                    invalid = false
                },
                enabled = enabled,
                singleLine = true,
                textStyle = AcornTheme.type.body.copy(color = colors.ink),
                cursorBrush = SolidColor(colors.accent),
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Go),
                keyboardActions = KeyboardActions(onGo = { submit() }),
                modifier = Modifier.fillMaxWidth(),
            )
        }
        if (typed.isNotBlank()) {
            Spacer(Modifier.width(8.dp))
            AcornButton("打开", onClick = submit, kind = ButtonKind.Quiet, compact = true)
        }
    }
}

@Composable
private fun Frame(frame: ShownFrame, onTap: (ViewportPoint) -> Unit, onScroll: (ViewportPoint, Double) -> Unit) {
    val haptics = rememberHaptics()
    Image(
        bitmap = frame.image,
        contentDescription = "Agent 的浏览器画面",
        contentScale = ContentScale.FillBounds,
        modifier = Modifier
            .aspectRatio(frame.width.toFloat() / frame.height.toFloat())
            .pointerInput(frame.width, frame.height) {
                detectTapGestures { offset ->
                    haptics.tick()
                    onTap(viewportPoint(offset.x, offset.y, size.width.toFloat(), size.height.toFloat(), frame.width, frame.height))
                }
            }
            .pointerInput(frame.width, frame.height) {
                var dragged = 0f
                var at = ViewportPoint(frame.width / 2.0, frame.height / 2.0)
                detectVerticalDragGestures(
                    onDragStart = { offset ->
                        dragged = 0f
                        at = viewportPoint(offset.x, offset.y, size.width.toFloat(), size.height.toFloat(), frame.width, frame.height)
                    },
                    onDragEnd = { if (dragged != 0f) onScroll(at, scrollDistance(dragged, size.height.toFloat(), frame.height)) },
                ) { _, amount -> dragged += amount }
            },
    )
}

@Composable
private fun KeyBar(enabled: Boolean, onType: (String) -> Unit, onKey: (BrowserInputRequest.Key) -> Unit) {
    val colors = AcornTheme.colors
    var text by remember { mutableStateOf("") }
    val shape = AcornShapes.large
    Column(modifier = Modifier.fillMaxWidth().navigationBarsPadding().padding(horizontal = 12.dp, vertical = 6.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.padding(bottom = 6.dp)) {
            listOf("回车" to BrowserInputRequest.Key.Enter, "退格" to BrowserInputRequest.Key.Backspace, "Tab" to BrowserInputRequest.Key.Tab).forEach { (label, key) ->
                AcornButton(label, onClick = { onKey(key) }, kind = ButtonKind.Secondary, compact = true, enabled = enabled)
            }
        }
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clip(shape)
                .background(colors.surface)
                .border(1.dp, colors.lineStrong, shape)
                .padding(start = 12.dp, end = 6.dp, top = 6.dp, bottom = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(modifier = Modifier.weight(1f).heightIn(min = 36.dp).padding(vertical = 7.dp), contentAlignment = Alignment.CenterStart) {
                if (text.isEmpty()) Text("先点网页里的输入框，再在这里打字", style = AcornTheme.type.body, color = colors.inkFaint)
                BasicTextField(
                    value = text,
                    onValueChange = { text = it },
                    enabled = enabled,
                    singleLine = true,
                    textStyle = AcornTheme.type.body.copy(color = colors.ink),
                    cursorBrush = SolidColor(colors.accent),
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Send),
                    keyboardActions = KeyboardActions(onSend = {
                        onType(text)
                        text = ""
                    }),
                    modifier = Modifier.fillMaxWidth(),
                )
            }
            Spacer(Modifier.width(8.dp))
            val canSend = enabled && text.isNotEmpty()
            Box(
                modifier = Modifier
                    .size(30.dp)
                    .clip(AcornShapes.small)
                    .background(if (canSend) colors.accent else colors.surfaceMuted)
                    .clickable(enabled = canSend) {
                        onType(text)
                        text = ""
                    },
                contentAlignment = Alignment.Center,
            ) {
                Icon(AcornIcons.ArrowUp, contentDescription = "输入到网页", tint = if (canSend) colors.onAccent else colors.inkFaint, modifier = Modifier.size(16.dp))
            }
        }
    }
}
