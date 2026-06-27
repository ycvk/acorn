package io.ycvk.acorn.feature.settings

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.core.theme.Bg
import io.ycvk.acorn.core.theme.Border
import io.ycvk.acorn.core.theme.Danger
import io.ycvk.acorn.core.theme.Surface
import io.ycvk.acorn.core.theme.TextPrimary
import io.ycvk.acorn.core.theme.TextSecondary

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
        modifier = modifier
            .fillMaxSize()
            .background(Bg)
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        // Connection
        item {
            SectionHeader("connection")
            SectionCard {
                profile?.let {
                    SettingRow("server", it.serverUrl)
                    SettingRow("device_id", it.deviceId)
                } ?: Text(
                    "not connected",
                    style = MaterialTheme.typography.bodySmall,
                    color = TextSecondary,
                )
            }
        }

        // Model
        item {
            SectionHeader("model")
            SectionCard {
                SettingRow("model", status?.model?.name ?: "—")
                SettingRow("readiness", status?.runtimeReadiness?.status?.name ?: "—")
                status?.runtimeReadiness?.reason?.takeIf { it.isNotBlank() }?.let {
                    Text(
                        it,
                        style = MaterialTheme.typography.bodySmall,
                        color = TextSecondary,
                    )
                }
            }
        }

        // Workspace
        item {
            SectionHeader("workspace")
            SectionCard {
                SettingRow("root", status?.workspaceRoot ?: "—")
                SettingRow(
                    "tools",
                    "${status?.summary?.enabledToolCount ?: 0}/${status?.summary?.toolCount ?: 0} enabled",
                )
                SettingRow(
                    "mcp",
                    "${status?.summary?.mcpHealthyProviderCount ?: 0}/${status?.summary?.mcpConfiguredProviderCount ?: 0} healthy",
                )
            }
        }

        // Disconnect
        item {
            Spacer(Modifier.height(8.dp))
            Button(
                onClick = { viewModel.disconnect() },
                modifier = Modifier.fillMaxWidth(),
                shape = RoundedCornerShape(4.dp),
                colors = ButtonDefaults.outlinedButtonColors(
                    contentColor = Danger,
                ),
            ) {
                Text(
                    "disconnect",
                    style = MaterialTheme.typography.labelLarge.copy(
                        fontFamily = FontFamily.Monospace,
                    ),
                )
            }
        }

        error?.let {
            item {
                Text(
                    "! $it",
                    style = MaterialTheme.typography.bodySmall.copy(
                        fontFamily = FontFamily.Monospace,
                    ),
                    color = Danger,
                    modifier = Modifier.padding(top = 8.dp),
                )
            }
        }
    }
}

@Composable
private fun SectionHeader(title: String) {
    Text(
        title,
        style = MaterialTheme.typography.labelSmall,
        color = TextSecondary,
    )
}

@Composable
private fun SectionCard(content: @Composable () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(Surface)
            .border(1.dp, Border, RoundedCornerShape(4.dp))
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        content()
    }
}

@Composable
private fun SettingRow(label: String, value: String) {
    Column {
        Text(
            label,
            style = MaterialTheme.typography.labelSmall,
            color = TextSecondary,
        )
        Text(
            value,
            style = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
            color = TextPrimary,
        )
    }
}
