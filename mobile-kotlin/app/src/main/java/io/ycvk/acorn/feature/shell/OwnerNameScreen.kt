package io.ycvk.acorn.feature.shell

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornButton
import io.ycvk.acorn.core.ui.AcornDialog
import io.ycvk.acorn.core.ui.AcornMark
import io.ycvk.acorn.core.ui.ButtonKind
import io.ycvk.acorn.core.ui.FormField
import io.ycvk.acorn.core.ui.InlineError
import io.ycvk.acorn.data.repository.ownerNameDraft

/**
 * Asks what Acorn should call the owner. Runs and the memories Acorn writes
 * about the owner use this name, so the app opens only once one is saved.
 */
@Composable
internal fun OwnerNameScreen(saving: Boolean, error: String?, onSave: (String) -> Unit) {
    var name by rememberSaveable { mutableStateOf("") }
    val focus = LocalFocusManager.current
    val canSave = name.isNotBlank() && !saving
    val save = {
        focus.clearFocus()
        onSave(name.trim())
    }
    val field = remember { FocusRequester() }
    LaunchedEffect(Unit) { field.requestFocus() }
    GateLayout(
        title = "Acorn 该怎么称呼你？",
        lead = "它在对话里这样叫你，记下关于你的事时也用这个名字。以后可以在设置里改。",
    ) {
        FormField(
            label = "称呼",
            value = name,
            onValueChange = { name = ownerNameDraft(it) },
            placeholder = "你的名字或昵称",
            enabled = !saving,
            keyboard = KeyboardOptions(imeAction = ImeAction.Done),
            onImeAction = { if (canSave) save() },
            modifier = Modifier.focusRequester(field),
        )
        AnimatedVisibility(visible = error != null) {
            InlineError(error.orEmpty(), Modifier.padding(top = 18.dp))
        }
        Spacer(Modifier.height(28.dp))
        AcornButton("继续", onClick = save, loading = saving, enabled = canSave, modifier = Modifier.fillMaxWidth())
    }
}

/** The owner's name could not be read; the app stays closed until it can be, or the owner disconnects. */
@Composable
internal fun OwnerCheckFailedScreen(message: String, onRetry: () -> Unit, onDisconnect: () -> Unit) {
    var confirmDisconnect by remember { mutableStateOf(false) }
    GateLayout(title = "连不上 Acorn", lead = "要先读到 Acorn 怎么称呼你，才能打开。") {
        InlineError(message, action = "重试", onAction = onRetry)
        Spacer(Modifier.height(28.dp))
        AcornButton("断开连接", onClick = { confirmDisconnect = true }, kind = ButtonKind.Quiet, modifier = Modifier.fillMaxWidth())
    }
    if (confirmDisconnect) {
        AcornDialog(
            title = "断开连接？",
            body = "这台手机会退出配对，不再收到推送。要重新连接，需要在服务器上生成新的配对码。",
            confirm = "断开",
            destructive = true,
            onConfirm = {
                confirmDisconnect = false
                onDisconnect()
            },
            onDismiss = { confirmDisconnect = false },
        )
    }
}

@Composable
private fun GateLayout(title: String, lead: String, content: @Composable ColumnScope.() -> Unit) {
    val colors = AcornTheme.colors
    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(colors.canvas)
            .statusBarsPadding()
            .navigationBarsPadding()
            .imePadding()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 24.dp),
    ) {
        Spacer(Modifier.height(72.dp))
        AcornMark(size = 44.dp)
        Spacer(Modifier.height(24.dp))
        Text(title, style = AcornTheme.type.largeTitle, color = colors.ink)
        Spacer(Modifier.height(8.dp))
        Text(lead, style = AcornTheme.type.callout, color = colors.inkSoft)
        Spacer(Modifier.height(36.dp))
        content()
        Spacer(Modifier.height(32.dp))
    }
}
