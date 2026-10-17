package io.ycvk.acorn.feature.share

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.Image
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.delay
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornButton
import io.ycvk.acorn.core.ui.ButtonKind
import io.ycvk.acorn.core.ui.InlineError
import io.ycvk.acorn.core.ui.SheetHandle
import io.ycvk.acorn.core.ui.SheetShape
import io.ycvk.acorn.core.ui.rememberHaptics

private const val SENT_CLOSE_DELAY_MS = 4_000L

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ShareSheet(
    viewModel: ShareViewModel,
    onClose: () -> Unit,
    onOpenApp: () -> Unit,
    onOpenThread: (String) -> Unit,
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    var note by rememberSaveable { mutableStateOf("") }
    val colors = AcornTheme.colors
    ModalBottomSheet(
        onDismissRequest = onClose,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        shape = SheetShape,
        containerColor = colors.surface,
        scrimColor = colors.scrim,
        dragHandle = { SheetHandle() },
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .imePadding()
                .padding(horizontal = 24.dp)
                .padding(bottom = 28.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Text("交给 Acorn", style = AcornTheme.type.title, color = colors.ink)
            when (val current = state) {
                ShareState.Loading -> CircularProgressIndicator(color = colors.inkFaint, strokeWidth = 2.dp, modifier = Modifier.size(22.dp))
                ShareState.Unsupported -> Message("Acorn 可以接收分享的文字、链接和图片。", "关闭", onClose)
                ShareState.NotPaired -> Message("这台手机还没有和 Acorn 配对。", "打开 Acorn", onOpenApp)
                is ShareState.Editing -> Editor(current.content, note, { note = it }, current.error, sending = false) { viewModel.send(note) }
                is ShareState.Sending -> Editor(current.content, note, { note = it }, error = null, sending = true) {}
                is ShareState.Sent -> Sent(onOpenThread = { onOpenThread(current.threadId) }, onClose = onClose)
            }
        }
    }
}

@Composable
private fun Editor(
    content: SharedContent,
    note: String,
    onNote: (String) -> Unit,
    error: String?,
    sending: Boolean,
    onSend: () -> Unit,
) {
    val colors = AcornTheme.colors
    val haptics = rememberHaptics()
    Preview(content)
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(min = 72.dp)
            .clip(AcornShapes.small)
            .border(0.5.dp, colors.lineStrong, AcornShapes.small)
            .padding(horizontal = 14.dp, vertical = 12.dp),
    ) {
        if (note.isEmpty()) Text("想让 Acorn 怎么处理？（可选）", style = AcornTheme.type.callout, color = colors.inkFaint)
        BasicTextField(
            value = note,
            onValueChange = onNote,
            enabled = !sending,
            maxLines = 4,
            textStyle = AcornTheme.type.callout.copy(color = colors.ink),
            cursorBrush = SolidColor(colors.accent),
            modifier = Modifier.fillMaxWidth(),
        )
    }
    error?.let { InlineError(it) }
    AcornButton(
        text = if (error != null) "重新发送" else "发送",
        onClick = {
            haptics.confirm()
            onSend()
        },
        loading = sending,
        modifier = Modifier.fillMaxWidth(),
    )
}

@Composable
private fun Preview(content: SharedContent) {
    val colors = AcornTheme.colors
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(AcornShapes.medium)
            .background(colors.surfaceMuted)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        content.subject?.let { Text(it, style = AcornTheme.type.headline, color = colors.ink) }
        content.text?.let { Text(it, style = AcornTheme.type.footnote, color = colors.inkSoft, maxLines = 6, overflow = TextOverflow.Ellipsis) }
        if (content.imageUri != null) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.Outlined.Image, contentDescription = null, tint = colors.inkSoft, modifier = Modifier.size(16.dp))
                Spacer(Modifier.width(6.dp))
                Text("附带一张图片", style = AcornTheme.type.footnote, color = colors.inkSoft)
            }
        }
    }
}

@Composable
private fun Sent(onOpenThread: () -> Unit, onClose: () -> Unit) {
    LaunchedEffect(Unit) {
        delay(SENT_CLOSE_DELAY_MS)
        onClose()
    }
    val colors = AcornTheme.colors
    Row(verticalAlignment = Alignment.CenterVertically) {
        Icon(Icons.Outlined.CheckCircle, contentDescription = null, tint = colors.success, modifier = Modifier.size(22.dp))
        Spacer(Modifier.width(10.dp))
        Text("已交给 Acorn，它会整理好", style = AcornTheme.type.body, color = colors.ink)
    }
    AcornButton("查看对话", onOpenThread, kind = ButtonKind.Secondary, modifier = Modifier.fillMaxWidth())
}

@Composable
private fun Message(text: String, action: String, onAction: () -> Unit) {
    Text(text, style = AcornTheme.type.callout, color = AcornTheme.colors.inkSoft)
    AcornButton(action, onAction, kind = ButtonKind.Secondary, modifier = Modifier.fillMaxWidth())
}
