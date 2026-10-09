package io.ycvk.acorn.feature.settings

import android.content.Intent
import android.provider.Settings
import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.selection.toggleable
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.NotificationsActive
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.core.theme.AetherError
import io.ycvk.acorn.core.theme.AetherOnSurfaceVariant

@Composable
fun NotificationAccessSection(viewModel: NotificationAccessViewModel = hiltViewModel()) {
    val context = LocalContext.current
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    val access by viewModel.access.collectAsStateWithLifecycle()
    val allowed by viewModel.allowed.collectAsStateWithLifecycle()
    val upload by viewModel.upload.collectAsStateWithLifecycle()
    val apps by viewModel.apps.collectAsStateWithLifecycle()
    val error by viewModel.error.collectAsStateWithLifecycle()
    val loading by viewModel.loading.collectAsStateWithLifecycle()
    var chooseApps by remember { mutableStateOf(false) }
    var query by remember { mutableStateOf("") }

    DisposableEffect(lifecycle) {
        val observer = LifecycleEventObserver { _, event -> if (event == Lifecycle.Event.ON_RESUME) viewModel.refresh() }
        lifecycle.addObserver(observer)
        viewModel.refresh()
        onDispose { lifecycle.removeObserver(observer) }
    }

    Column {
        SectionHeader("phone notifications", Icons.Filled.NotificationsActive)
        SectionCard {
            Text("Share selected apps' notifications with your server and model provider.", style = MaterialTheme.typography.bodyMedium)
            Text(
                "Raw notifications stay on the server for 7 days. Content included in conversations, context snapshots or notes stays with those records.",
                style = MaterialTheme.typography.bodySmall, color = AetherOnSurfaceVariant,
            )
            Text(if (access) "Notification access enabled" else "Notification access is off", style = MaterialTheme.typography.bodyMedium)
            TextButton(onClick = { context.startActivity(Intent(Settings.ACTION_NOTIFICATION_LISTENER_SETTINGS)) }) {
                Text(if (access) "Manage access" else "Grant access")
            }
            Text(if (allowed.isEmpty()) "No apps selected. Nothing is captured." else "${allowed.size} apps selected")
            TextButton(onClick = { chooseApps = true }) { Text("Choose apps") }
            Text(if (upload.uploading) "Uploading ${upload.pending} notifications…" else "${upload.pending} notifications waiting to upload", style = MaterialTheme.typography.bodySmall)
            if (upload.dropped > 0) Text("${upload.dropped} oldest notifications removed when the 500-item queue was full.", style = MaterialTheme.typography.bodySmall)
            upload.error?.let { Text(it, color = AetherError, style = MaterialTheme.typography.bodySmall) }
            if (upload.pending > 0 || upload.error != null) {
                TextButton(onClick = viewModel::retry, enabled = access && !upload.uploading) { Text("Retry upload") }
            }
        }
    }

    if (chooseApps) {
        AlertDialog(
            onDismissRequest = { chooseApps = false },
            title = { Text("Choose apps") },
            confirmButton = { TextButton(onClick = { chooseApps = false }) { Text("Done") } },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text("Only selected apps are captured. Turning one off also removes its waiting notifications.")
                    OutlinedTextField(value = query, onValueChange = { query = it }, label = { Text("Find an app") }, singleLine = true, modifier = Modifier.fillMaxWidth())
                    if (loading) CircularProgressIndicator(Modifier.size(24.dp))
                    error?.let {
                        Text(it, color = AetherError)
                        TextButton(onClick = viewModel::refresh) { Text("Retry loading apps") }
                    }
                    LazyColumn(Modifier.heightIn(max = 380.dp)) {
                        items(apps.filter { it.name.contains(query, true) || it.packageName.contains(query, true) }, key = { it.packageName }) { app ->
                            Row(
                                modifier = Modifier.fillMaxWidth().toggleable(value = app.packageName in allowed, role = Role.Switch, onValueChange = { viewModel.select(app.packageName, it) }).padding(vertical = 10.dp),
                                verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.spacedBy(10.dp),
                            ) {
                                Image(app.icon, contentDescription = null, modifier = Modifier.size(32.dp))
                                Column(Modifier.weight(1f)) {
                                    Text(app.name, style = MaterialTheme.typography.bodyMedium)
                                    Text(app.packageName, style = MaterialTheme.typography.labelSmall, color = AetherOnSurfaceVariant)
                                }
                                Switch(checked = app.packageName in allowed, onCheckedChange = null)
                            }
                        }
                    }
                }
            },
        )
    }
}
