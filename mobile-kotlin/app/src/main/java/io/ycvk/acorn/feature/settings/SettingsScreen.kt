package io.ycvk.acorn.feature.settings

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.BuildConfig
import io.ycvk.acorn.api.models.RuntimeReadiness
import io.ycvk.acorn.core.push.PushStatus
import io.ycvk.acorn.core.push.pushStatusDetail
import io.ycvk.acorn.core.push.pushStatusLabel
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornButton
import io.ycvk.acorn.core.ui.ButtonKind
import io.ycvk.acorn.core.ui.Hairline
import io.ycvk.acorn.core.ui.InlineError
import io.ycvk.acorn.core.ui.Panel
import io.ycvk.acorn.core.ui.ScreenPadding
import io.ycvk.acorn.core.ui.SectionLabel
import io.ycvk.acorn.core.ui.TopBar

@Composable
fun SettingsScreen(
    onBack: () -> Unit,
    viewModel: SettingsViewModel = hiltViewModel(),
) {
    val status by viewModel.systemStatus.collectAsStateWithLifecycle()
    val error by viewModel.error.collectAsStateWithLifecycle()
    val pushStatus by viewModel.pushStatus.collectAsStateWithLifecycle()
    val profile = viewModel.profile
    val colors = AcornTheme.colors
    val scroll = rememberScrollState()
    var confirmDisconnect by remember { mutableStateOf(false) }

    LaunchedEffect(Unit) { viewModel.loadSettings() }

    Column(modifier = Modifier.fillMaxSize().background(colors.canvas)) {
        TopBar(onBack = onBack, title = "设置", showDivider = scroll.value > 0)
        Column(
            modifier = Modifier
                .verticalScroll(scroll)
                .navigationBarsPadding()
                .padding(bottom = 32.dp),
        ) {
            error?.let { InlineError(it, Modifier.padding(horizontal = ScreenPadding, vertical = 8.dp), action = "重试", onAction = viewModel::loadSettings) }

            SettingsGroup("连接") {
                if (profile != null) {
                    SettingRow("服务器", profile.serverUrl)
                    Hairline(inset = 16.dp)
                    SettingRow("设备", shortId(profile.deviceId))
                } else {
                    SettingRow("状态", "未连接")
                }
            }

            SettingsGroup("推送通知") {
                SettingRow(
                    "状态",
                    pushStatusLabel(pushStatus),
                    valueColor = when (pushStatus) {
                        PushStatus.Registered -> colors.success
                        is PushStatus.Failed, PushStatus.PermissionDenied -> colors.danger
                        else -> colors.inkSoft
                    },
                )
                pushStatusDetail(pushStatus)?.let { detail ->
                    Text(
                        detail,
                        style = AcornTheme.type.caption,
                        color = colors.inkFaint,
                        modifier = Modifier.padding(start = 16.dp, end = 16.dp, bottom = 12.dp),
                    )
                }
                if (pushStatus is PushStatus.Failed || pushStatus == PushStatus.PermissionDenied) {
                    Hairline(inset = 16.dp)
                    SettingRow("重新注册", onClick = viewModel::retryPush, labelColor = colors.accent)
                }
            }

            NotificationAccessSection()

            SettingsGroup("模型") {
                SettingRow("名称", status?.model?.name ?: "—")
                Hairline(inset = 16.dp)
                val readiness = status?.runtimeReadiness
                SettingRow(
                    "状态",
                    when (readiness?.status) {
                        RuntimeReadiness.Status.ready -> "就绪"
                        RuntimeReadiness.Status.blocked -> "不可用"
                        null -> "—"
                    },
                    valueColor = if (readiness?.status == RuntimeReadiness.Status.blocked) colors.danger else colors.inkSoft,
                )
                readiness?.reason?.takeIf { it.isNotBlank() }?.let {
                    Text(it, style = AcornTheme.type.caption, color = colors.inkFaint, modifier = Modifier.padding(start = 16.dp, end = 16.dp, bottom = 12.dp))
                }
            }

            SettingsGroup("能力") {
                val summary = status?.summary
                SettingRow("工具", summary?.let { "${it.enabledToolCount} / ${it.toolCount} 已启用" } ?: "—")
                Hairline(inset = 16.dp)
                SettingRow("MCP 服务", summary?.let { "${it.mcpHealthyProviderCount} / ${it.mcpConfiguredProviderCount} 正常" } ?: "—")
            }

            SettingsGroup("关于") {
                SettingRow("版本", BuildConfig.VERSION_NAME)
            }

            Spacer(Modifier.height(28.dp))
            AcornButton(
                "断开连接",
                onClick = { confirmDisconnect = true },
                kind = ButtonKind.Danger,
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = ScreenPadding),
            )
            Text(
                "断开后需要新的配对码才能重新连上。",
                style = AcornTheme.type.caption,
                color = colors.inkFaint,
                textAlign = TextAlign.Center,
                modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
            )
        }
    }

    if (confirmDisconnect) {
        AlertDialog(
            onDismissRequest = { confirmDisconnect = false },
            title = { Text("断开连接？") },
            text = { Text("这台手机会退出配对，不再收到推送。要重新连接，需要在服务器上生成新的配对码。") },
            confirmButton = {
                TextButton(onClick = {
                    confirmDisconnect = false
                    viewModel.disconnect()
                }) { Text("断开", color = colors.danger) }
            },
            dismissButton = { TextButton(onClick = { confirmDisconnect = false }) { Text("取消") } },
        )
    }
}

/** "device_85995e25…66b0": enough to recognise a device without the whole id. */
fun shortId(id: String): String = if (id.length > 20) id.take(15) + "…" + id.takeLast(4) else id

@Composable
internal fun SettingsGroup(title: String, content: @Composable ColumnScope.() -> Unit) {
    SectionLabel(title)
    Panel(
        modifier = Modifier.padding(horizontal = ScreenPadding),
        contentPadding = PaddingValues(0.dp),
        content = content,
    )
}

@Composable
internal fun SettingRow(
    label: String,
    value: String? = null,
    valueColor: Color = AcornTheme.colors.inkSoft,
    labelColor: Color = AcornTheme.colors.ink,
    onClick: (() -> Unit)? = null,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier)
            .heightIn(min = 50.dp)
            .padding(horizontal = 16.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(label, style = AcornTheme.type.callout, color = labelColor)
        Spacer(Modifier.width(16.dp))
        Spacer(Modifier.weight(1f))
        value?.let {
            Text(it, style = AcornTheme.type.callout, color = valueColor, maxLines = 1, overflow = TextOverflow.Ellipsis, textAlign = TextAlign.End)
        }
        if (onClick != null && labelColor == AcornTheme.colors.ink) {
            Spacer(Modifier.width(4.dp))
            Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = null, tint = AcornTheme.colors.inkFaint, modifier = Modifier.size(18.dp))
        }
    }
}
