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
import androidx.compose.ui.Alignment
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
 * A centred bordered card that asks one question: a title, what happens,
 * optional content, and the buttons at the end with the confirming one last.
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
            enter = fadeIn(AcornMotion.enter(AcornMotion.SHORT)) + scaleIn(AcornMotion.glide(), initialScale = 0.95f),
        ) {
            Column(
                modifier = Modifier
                    .padding(horizontal = 24.dp)
                    .widthIn(max = 420.dp)
                    .fillMaxWidth()
                    .clip(AcornShapes.sheet)
                    .background(colors.surface)
                    .hairline(AcornShapes.sheet)
                    .padding(24.dp),
            ) {
                Text(title, style = AcornTheme.type.title.copy(fontSize = 18.sp), color = colors.ink)
                body?.let {
                    Spacer(Modifier.height(8.dp))
                    Text(it, style = AcornTheme.type.callout, color = colors.inkSoft)
                }
                content?.let {
                    Spacer(Modifier.height(14.dp))
                    it()
                }
                Spacer(Modifier.height(20.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End), modifier = Modifier.fillMaxWidth()) {
                    dismiss?.let {
                        AcornButton(it, onDismiss, kind = ButtonKind.Secondary, compact = true, enabled = dismissEnabled)
                    }
                    AcornButton(
                        confirm,
                        onConfirm,
                        kind = if (destructive) ButtonKind.Destructive else ButtonKind.Primary,
                        compact = true,
                        enabled = confirmEnabled,
                        loading = confirmLoading,
                    )
                }
            }
        }
    }
}
