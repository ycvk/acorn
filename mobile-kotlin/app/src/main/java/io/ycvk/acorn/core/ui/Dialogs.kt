package io.ycvk.acorn.core.ui

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.MutableTransitionState
import androidx.compose.animation.fadeIn
import androidx.compose.animation.scaleIn
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import io.ycvk.acorn.core.theme.AcornMotion
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme

/**
 * A centred card that asks one question: a title, what happens, optional
 * content, and two equal buttons with the confirming one on the right.
 * It grows in from slightly smaller as it appears.
 */
@Composable
fun AcornDialog(
    title: String,
    confirm: String,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
    body: String? = null,
    dismiss: String? = "取消",
    destructive: Boolean = false,
    confirmEnabled: Boolean = true,
    confirmLoading: Boolean = false,
    dismissEnabled: Boolean = true,
    content: (@Composable ColumnScope.() -> Unit)? = null,
) {
    val colors = AcornTheme.colors
    Dialog(onDismissRequest = onDismiss, properties = DialogProperties(usePlatformDefaultWidth = false)) {
        val appear = remember { MutableTransitionState(false) }.apply { targetState = true }
        AnimatedVisibility(
            visibleState = appear,
            enter = fadeIn(AcornMotion.enter(AcornMotion.SHORT)) + scaleIn(AcornMotion.glide(), initialScale = 0.92f),
        ) {
            Column(
                modifier = Modifier
                    .padding(horizontal = 28.dp)
                    .widthIn(max = 420.dp)
                    .fillMaxWidth()
                    .clip(AcornShapes.sheet)
                    .background(colors.surface)
                    .padding(start = 24.dp, end = 24.dp, top = 26.dp, bottom = 20.dp),
            ) {
                Text(title, style = AcornTheme.type.title.copy(fontSize = 19.sp), color = colors.ink)
                body?.let {
                    Spacer(Modifier.height(10.dp))
                    Text(it, style = AcornTheme.type.callout, color = colors.inkSoft)
                }
                content?.let {
                    Spacer(Modifier.height(16.dp))
                    it()
                }
                Spacer(Modifier.height(24.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(10.dp), modifier = Modifier.fillMaxWidth()) {
                    dismiss?.let {
                        AcornButton(it, onDismiss, kind = ButtonKind.Secondary, enabled = dismissEnabled, modifier = Modifier.weight(1f))
                    }
                    AcornButton(
                        confirm,
                        onConfirm,
                        kind = if (destructive) ButtonKind.Danger else ButtonKind.Primary,
                        enabled = confirmEnabled,
                        loading = confirmLoading,
                        modifier = Modifier.weight(1f),
                    )
                }
            }
        }
    }
}
