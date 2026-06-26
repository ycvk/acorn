package io.ycvk.acorn.feature.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle

/**
 * Settings tab: grouped cards showing the device connection, model/readiness,
 * workspace, and a disconnect button. Delegates all data work to
 * [SettingsViewModel].
 */
@Composable
fun SettingsScreen(
    modifier: Modifier = Modifier,
    viewModel: SettingsViewModel = hiltViewModel(),
) {
    val status by viewModel.systemStatus.collectAsStateWithLifecycle()
    val error by viewModel.error.collectAsStateWithLifecycle()
    val profile = viewModel.profile

    LaunchedEffect(Unit) { viewModel.loadSettings() }

    LazyColumn(
        modifier = modifier.fillMaxSize().padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        // Connection section
        item {
            SectionHeader("Connection")
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    profile?.let {
                        SettingRow("Server", it.serverUrl)
                        SettingRow("Device ID", it.deviceId)
                    } ?: Text("Not connected", color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        }

        // Model section
        item {
            SectionHeader("Model")
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    SettingRow("Model", status?.model?.name ?: "—")
                    SettingRow("Readiness", status?.runtimeReadiness?.status?.name ?: "—")
                    status?.runtimeReadiness?.reason?.takeIf { it.isNotBlank() }?.let {
                        Text(
                            it,
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }
        }

        // Workspace section
        item {
            SectionHeader("Workspace")
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    SettingRow("Root", status?.workspaceRoot ?: "—")
                    SettingRow(
                        "Tools",
                        "${status?.summary?.enabledToolCount ?: 0} / ${status?.summary?.toolCount ?: 0} enabled",
                    )
                    SettingRow(
                        "MCP providers",
                        "${status?.summary?.mcpHealthyProviderCount ?: 0} / ${status?.summary?.mcpConfiguredProviderCount ?: 0} healthy",
                    )
                }
            }
        }

        // Disconnect
        item {
            Spacer(Modifier.height(8.dp))
            Button(
                onClick = { viewModel.disconnect() },
                modifier = Modifier.fillMaxWidth(),
                colors = ButtonDefaults.buttonColors(containerColor = MaterialTheme.colorScheme.error),
            ) { Text("Disconnect") }
        }

        error?.let {
            item {
                Text(
                    it,
                    color = MaterialTheme.colorScheme.error,
                    style = MaterialTheme.typography.bodySmall,
                    modifier = Modifier.padding(top = 8.dp),
                )
            }
        }
    }
}

@Composable
private fun SectionHeader(title: String) {
    Spacer(Modifier.height(8.dp))
    Text(
        title,
        style = MaterialTheme.typography.titleMedium,
        fontWeight = FontWeight.SemiBold,
    )
    Spacer(Modifier.height(8.dp))
}

@Composable
private fun SettingRow(label: String, value: String) {
    Column {
        Text(
            label,
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(value, style = MaterialTheme.typography.bodyMedium)
    }
}
