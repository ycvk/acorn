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
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.BuildConfig
import io.ycvk.acorn.api.models.RuntimeReadiness
import io.ycvk.acorn.core.push.PushStatus
import io.ycvk.acorn.core.push.pushStatusDetail
import io.ycvk.acorn.core.push.pushStatusLabel
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornButton
import io.ycvk.acorn.core.ui.AcornDialog
import io.ycvk.acorn.core.ui.AcornIcons
import io.ycvk.acorn.core.ui.ButtonKind
import io.ycvk.acorn.core.ui.Hairline
import io.ycvk.acorn.core.ui.InlineError
import io.ycvk.acorn.core.ui.Panel
import io.ycvk.acorn.core.ui.ScreenPadding
import io.ycvk.acorn.core.theme.ThemeMode
import io.ycvk.acorn.core.ui.SectionLabel
import io.ycvk.acorn.core.ui.SegmentedControl
import io.ycvk.acorn.core.ui.TopBar
import io.ycvk.acorn.data.repository.ownerNameDraft

@Composable
fun SettingsScreen(
    onBack: () -> Unit,
    onOpenPersona: () -> Unit,
    onOpenSkills: () -> Unit,
    onOpenOwnerSettings: () -> Unit,
    viewModel: SettingsViewModel = hiltViewModel(),
) {
    val status by viewModel.systemStatus.collectAsStateWithLifecycle()
    val error by viewModel.error.collectAsStateWithLifecycle()
    val pushStatus by viewModel.pushStatus.collectAsStateWithLifecycle()
    val profile = viewModel.profile
    val colors = AcornTheme.colors
    val scroll = rememberScrollState()
    var confirmDisconnect by remember { mutableStateOf(false) }
    val ownerName by viewModel.ownerName.collectAsStateWithLifecycle()
    val savingName by viewModel.savingName.collectAsStateWithLifecycle()
    val nameError by viewModel.nameError.collectAsStateWithLifecycle()
    val themeMode by viewModel.themeMode.collectAsStateWithLifecycle()
    var editingName by remember { mutableStateOf<String?>(null) }

    LaunchedEffect(Unit) { viewModel.loadSettings() }

    Column(modifier = Modifier.fillMaxSize().background(colors.canvas)) {
        TopBar(onBack = onBack, title = "设置")
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

            SettingsGroup("Acorn") {
                SettingRow(
                    "怎么称呼你",
                    when (val name = ownerName) {
                        null -> "—"
                        "" -> "未设置"
                        else -> name
                    },
                    onClick = ownerName?.let { name -> { editingName = name } },
                )
                Hairline(inset = 16.dp)
                SettingRow("人格", "它是谁、怎么说话", onClick = onOpenPersona)
                Hairline(inset = 16.dp)
                SettingRow("作息、预算与审批", "时区、早安卡、预算和规则", onClick = onOpenOwnerSettings)
                Hairline(inset = 16.dp)
                SettingRow("技能", "它会做的事和技能包", onClick = onOpenSkills)
            }

            SettingsGroup("外观") {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .heightIn(min = 52.dp)
                        .padding(start = 16.dp, end = 10.dp, top = 8.dp, bottom = 8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text("主题", style = AcornTheme.type.callout, color = colors.ink, modifier = Modifier.weight(1f))
                    SegmentedControl(
                        options = ThemeMode.entries.map { it.label },
                        selected = themeMode.ordinal,
                        onSelect = { viewModel.setThemeMode(ThemeMode.entries[it]) },
                    )
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

    editingName?.let { draft ->
        AcornDialog(
            title = "Acorn 怎么称呼你",
            body = "Acorn 会这样称呼你，记下关于你的事时也用这个名字，从下一次对话起生效。",
            confirm = "保存",
            confirmEnabled = !savingName && draft.isNotBlank(),
            confirmLoading = savingName,
            dismissEnabled = !savingName,
            onConfirm = { viewModel.saveOwnerName(draft) { editingName = null } },
            onDismiss = {
                if (!savingName) {
                    viewModel.dismissNameError()
                    editingName = null
                }
            },
        ) {
            BasicTextField(
                value = draft,
                onValueChange = { editingName = ownerNameDraft(it) },
                singleLine = true,
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                textStyle = AcornTheme.type.callout.copy(color = colors.ink),
                cursorBrush = SolidColor(colors.accent),
                decorationBox = { field ->
                    if (draft.isEmpty()) Text("你的名字或昵称", style = AcornTheme.type.callout, color = colors.inkFaint)
                    field()
                },
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(min = 44.dp)
                    .clip(AcornShapes.medium)
                    .background(colors.surfaceMuted)
                    .padding(horizontal = 14.dp, vertical = 12.dp),
            )
            nameError?.let {
                Spacer(Modifier.height(8.dp))
                Text(it, style = AcornTheme.type.caption, color = colors.danger)
            }
        }
    }

    if (confirmDisconnect) {
        AcornDialog(
            title = "断开连接？",
            body = "这台手机会退出配对，不再收到推送。要重新连接，需要在服务器上生成新的配对码。",
            confirm = "断开",
            destructive = true,
            onConfirm = {
                confirmDisconnect = false
                viewModel.disconnect()
            },
            onDismiss = { confirmDisconnect = false },
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
            .heightIn(min = 52.dp)
            .padding(horizontal = 16.dp, vertical = 13.dp),
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
            Icon(AcornIcons.ChevronRight, contentDescription = null, tint = AcornTheme.colors.inkFaint, modifier = Modifier.size(18.dp))
        }
    }
}
