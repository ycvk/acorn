package io.ycvk.acorn.feature.share

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Image
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.core.theme.AetherError
import io.ycvk.acorn.core.theme.AetherOnPrimary
import io.ycvk.acorn.core.theme.AetherOnSurface
import io.ycvk.acorn.core.theme.AetherOnSurfaceVariant
import io.ycvk.acorn.core.theme.AetherPrimary
import io.ycvk.acorn.core.theme.AetherSurface
import io.ycvk.acorn.core.theme.AetherSurfaceHigh
import kotlinx.coroutines.delay

private const val SENT_CLOSE_DELAY_MS = 2_000L

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
    ModalBottomSheet(
        onDismissRequest = onClose,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        containerColor = AetherSurface,
        shape = RoundedCornerShape(topStart = 28.dp, topEnd = 28.dp),
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .imePadding()
                .padding(horizontal = 24.dp)
                .padding(bottom = 32.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text("Share to Acorn", style = MaterialTheme.typography.titleLarge, color = AetherOnSurface)
            when (val current = state) {
                ShareState.Loading -> CircularProgressIndicator(color = AetherPrimary, modifier = Modifier.size(24.dp))
                ShareState.Unsupported -> Message(
                    text = "Acorn takes shared text, links and images.",
                    action = "Close",
                    onAction = onClose,
                )
                ShareState.NotPaired -> Message(
                    text = "Pair this phone in Acorn first.",
                    action = "Open Acorn",
                    onAction = onOpenApp,
                )
                is ShareState.Editing -> Editor(current.content, note, { note = it }, current.error, sending = false) {
                    viewModel.send(note)
                }
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
    Preview(content)
    OutlinedTextField(
        value = note,
        onValueChange = onNote,
        label = { Text("Add a note (optional)") },
        enabled = !sending,
        modifier = Modifier.fillMaxWidth(),
        maxLines = 4,
    )
    error?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = AetherError) }
    Button(
        onClick = onSend,
        enabled = !sending,
        colors = ButtonDefaults.buttonColors(containerColor = AetherPrimary, contentColor = AetherOnPrimary),
        modifier = Modifier.fillMaxWidth(),
    ) {
        if (sending) {
            CircularProgressIndicator(color = AetherOnPrimary, strokeWidth = 2.dp, modifier = Modifier.size(18.dp))
        } else {
            Text(if (error != null) "Retry" else "Send")
        }
    }
}

@Composable
private fun Preview(content: SharedContent) {
    Surface(color = AetherSurfaceHigh, shape = RoundedCornerShape(16.dp), modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            content.subject?.let {
                Text(it, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.SemiBold, color = AetherOnSurface)
            }
            content.text?.let {
                Text(it, style = MaterialTheme.typography.bodySmall, color = AetherOnSurfaceVariant, maxLines = 6, overflow = TextOverflow.Ellipsis)
            }
            if (content.imageUri != null) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Icon(Icons.Filled.Image, contentDescription = null, tint = AetherOnSurfaceVariant, modifier = Modifier.size(16.dp))
                    Spacer(Modifier.width(6.dp))
                    Text("Image attached", style = MaterialTheme.typography.bodySmall, color = AetherOnSurfaceVariant)
                }
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
    Row(verticalAlignment = Alignment.CenterVertically) {
        Icon(Icons.Filled.CheckCircle, contentDescription = null, tint = AetherPrimary, modifier = Modifier.size(20.dp))
        Spacer(Modifier.width(8.dp))
        Text("Sent to Acorn", style = MaterialTheme.typography.bodyLarge, color = AetherOnSurface)
    }
    TextButton(onClick = onOpenThread) { Text("Open conversation", color = AetherPrimary) }
}

@Composable
private fun Message(text: String, action: String, onAction: () -> Unit) {
    Text(text, style = MaterialTheme.typography.bodyMedium, color = AetherOnSurfaceVariant)
    Spacer(Modifier.height(4.dp))
    TextButton(onClick = onAction) { Text(action, color = AetherPrimary) }
}
