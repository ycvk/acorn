package io.ycvk.acorn.feature.skills

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
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
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.SkillDetail
import io.ycvk.acorn.api.models.SkillFile
import io.ycvk.acorn.api.models.SkillPackage
import io.ycvk.acorn.api.models.SkillPackageServer
import io.ycvk.acorn.api.models.SkillPackageTrigger
import io.ycvk.acorn.api.models.SkillRevisionSummary
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornButton
import io.ycvk.acorn.core.ui.AcornDialog
import io.ycvk.acorn.core.ui.AcornIcons
import io.ycvk.acorn.core.ui.ButtonKind
import io.ycvk.acorn.core.ui.ErrorState
import io.ycvk.acorn.core.ui.Hairline
import io.ycvk.acorn.core.ui.InlineError
import io.ycvk.acorn.core.ui.Markdown
import io.ycvk.acorn.core.ui.Panel
import io.ycvk.acorn.core.ui.RevisionRow
import io.ycvk.acorn.core.ui.ScreenPadding
import io.ycvk.acorn.core.ui.SkeletonRows
import io.ycvk.acorn.core.ui.StatusDot
import io.ycvk.acorn.core.ui.TopBar
import java.time.OffsetDateTime

/** One skill: where it stands, what it says, the files and package it carries, and its history. */
@Composable
fun SkillScreen(
    name: String,
    viewModel: SkillsViewModel,
    onBack: () -> Unit,
    onOpenRevision: (Long) -> Unit,
    onOpenThread: (String) -> Unit,
) {
    val load by viewModel.skill.collectAsStateWithLifecycle()
    val change by viewModel.change.collectAsStateWithLifecycle()
    LaunchedEffect(name) { viewModel.open(name) }
    val colors = AcornTheme.colors
    val scroll = rememberScrollState()
    var confirmRetire by rememberSaveable { mutableStateOf(false) }
    var confirmRestore by rememberSaveable { mutableStateOf(false) }
    var settingSecret by rememberSaveable { mutableStateOf<String?>(null) }
    var clearingSecret by rememberSaveable { mutableStateOf<String?>(null) }
    // The value lives only while the dialog is open and is never saved with the screen's state.
    var secretValue by remember { mutableStateOf("") }
    val loaded = (load as? SkillLoad.Loaded)?.takeIf { it.detail.name == name }
    val saving = change == SkillChange.Saving
    val now = OffsetDateTime.now()

    Column(modifier = Modifier.fillMaxSize().background(colors.canvas)) {
        TopBar(
            onBack = onBack,
            title = name,
            subtitle = loaded?.detail?.let { "${skillRevisionTitle(it.revision)} · ${skillRevisionMeta(it.author, it.updatedAt, now)}" },
        )
        when {
            load is SkillLoad.Failed -> ErrorState((load as SkillLoad.Failed).message, onRetry = { viewModel.open(name) })
            loaded == null -> SkeletonRows(rows = 4)
            else -> {
                val detail = loaded.detail
                Column(modifier = Modifier.weight(1f).verticalScroll(scroll)) {
                    SkillBody(
                        detail = detail,
                        revisions = loaded.revisions,
                        now = now,
                        onOpenRun = { runId -> viewModel.openRun(runId, onOpenThread) },
                        onSetSecret = { secretValue = ""; settingSecret = it },
                        onClearSecret = { clearingSecret = it },
                        secretsEnabled = !saving,
                        onOpenRevision = onOpenRevision,
                    )
                }
                Column(
                    modifier = Modifier
                        .navigationBarsPadding()
                        .padding(horizontal = ScreenPadding, vertical = 12.dp),
                    verticalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    (change as? SkillChange.Failed)?.let {
                        InlineError(it.message, action = "知道了", onAction = viewModel::dismissChangeError)
                    }
                    if (detail.retired) {
                        AcornButton("恢复使用", { confirmRestore = true }, modifier = Modifier.fillMaxWidth(), kind = ButtonKind.Secondary, loading = saving && settingSecret == null && clearingSecret == null)
                    } else {
                        AcornButton("停用", { confirmRetire = true }, modifier = Modifier.fillMaxWidth(), kind = ButtonKind.Danger, loading = saving && settingSecret == null && clearingSecret == null)
                    }
                }
            }
        }
    }

    val detail = loaded?.detail
    if (confirmRetire && detail != null) {
        AcornDialog(
            title = "停用 ${detail.name}？",
            body = "对话里不再列出这个技能，它的 MCP 服务器和触发器也随之停下。停用会另存为新的一版，之后可以随时恢复。",
            confirm = "停用",
            destructive = true,
            onConfirm = {
                confirmRetire = false
                viewModel.retire()
            },
            onDismiss = { confirmRetire = false },
        )
    }
    if (confirmRestore && detail != null) {
        AcornDialog(
            title = "恢复 ${detail.name}？",
            body = "用${skillRevisionTitle(detail.revision)}的内容重新启用，另存为新的一版，接下来的对话会列出它。",
            confirm = "恢复",
            onConfirm = {
                confirmRestore = false
                viewModel.restore()
            },
            onDismiss = { confirmRestore = false },
        )
    }
    val secret = settingSecret
    if (secret != null && detail != null) {
        AcornDialog(
            title = "设置 $secret",
            body = "密钥只写不读：保存后手机和服务器都不再显示它的值，只交给这个技能包的 MCP 服务器和触发器，从下一次运行起生效。",
            confirm = "保存",
            confirmEnabled = secretValue.isNotEmpty() && !saving,
            confirmLoading = saving,
            dismissEnabled = !saving,
            onConfirm = {
                viewModel.setSecret(secret, secretValue) {
                    secretValue = ""
                    settingSecret = null
                }
            },
            onDismiss = {
                if (!saving) {
                    secretValue = ""
                    settingSecret = null
                }
            },
        ) {
            BasicTextField(
                value = secretValue,
                onValueChange = { secretValue = it },
                singleLine = true,
                visualTransformation = PasswordVisualTransformation(),
                keyboardOptions = KeyboardOptions(autoCorrectEnabled = false, keyboardType = KeyboardType.Password, imeAction = ImeAction.Done),
                textStyle = AcornTheme.type.mono.copy(color = colors.ink),
                cursorBrush = SolidColor(colors.accent),
                decorationBox = { field ->
                    if (secretValue.isEmpty()) Text("粘贴或输入值", style = AcornTheme.type.callout, color = colors.inkFaint)
                    field()
                },
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(min = 44.dp)
                    .clip(AcornShapes.medium)
                    .background(colors.surfaceMuted)
                    .padding(horizontal = 14.dp, vertical = 12.dp),
            )
        }
    }
    val clearing = clearingSecret
    if (clearing != null && detail != null) {
        AcornDialog(
            title = "清除 $clearing？",
            body = "清除后这个技能包缺少密钥，对话里不再列出它，它的 MCP 服务器和触发器也不会运行，直到重新设置。",
            confirm = "清除",
            destructive = true,
            onConfirm = {
                clearingSecret = null
                viewModel.clearSecret(clearing)
            },
            onDismiss = { clearingSecret = null },
        )
    }
}

@Composable
private fun SkillBody(
    detail: SkillDetail,
    revisions: List<SkillRevisionSummary>,
    now: OffsetDateTime,
    onOpenRun: (String) -> Unit,
    onSetSecret: (String) -> Unit,
    onClearSecret: (String) -> Unit,
    secretsEnabled: Boolean,
    onOpenRevision: (Long) -> Unit,
) {
    val colors = AcornTheme.colors
    Column(modifier = Modifier.padding(start = ScreenPadding, end = ScreenPadding, top = 8.dp)) {
        StandingPanel(detail)
        detail.description?.takeIf { it.isNotBlank() }?.let {
            Spacer(Modifier.height(14.dp))
            Text(it, style = AcornTheme.type.body, color = colors.ink)
        }
        detail.runId?.let { runId ->
            Spacer(Modifier.height(10.dp))
            LinkRow("这一版出自一次对话", onClick = { onOpenRun(runId) })
        }

        SkillSectionTitle("说明")
        SkillContent(detail.instruction, detail.markdown)

        if (detail.files.isNotEmpty()) {
            SkillSectionTitle("附带文件")
            SkillFiles(detail.files)
        }

        detail.`package`?.let { pkg ->
            PackageSection(pkg, now, onSetSecret, onClearSecret, secretsEnabled)
        }

        SkillSectionTitle("历史")
    }
    revisions.forEachIndexed { index, revision ->
        if (index > 0) Hairline(inset = ScreenPadding)
        RevisionRow(
            title = skillRevisionTitle(revision.revision) + if (revision.retired) " · 停用" else "",
            meta = skillRevisionMeta(revision.author, revision.at, now),
            current = revision.revision == detail.revision,
            onClick = { onOpenRevision(revision.revision) },
        )
    }
    Spacer(Modifier.height(24.dp))
}

/** Where the skill stands for the next conversation, and why when it is left out. */
@Composable
private fun StandingPanel(detail: SkillDetail) {
    val colors = AcornTheme.colors
    val standing = skillStanding(detail.retired, detail.eligible, detail.problem)
    val (dot, fill, text) = when (standing) {
        SkillStanding.InUse -> Triple(colors.success, colors.surface, "在用：对话里会列出它，Acorn 遇到对应的事时加载。")
        SkillStanding.NotListed -> Triple(
            colors.warning,
            colors.warningSoft,
            "暂不可用：下一次对话不会列出它。" + (detail.disabledReasons?.joinToString("；") { disabledReasonLabel(it) } ?: ""),
        )
        SkillStanding.Retired -> Triple(colors.inkFaint, colors.surface, "已停用：对话里不会用它，恢复后重新可用。")
        SkillStanding.Invalid -> Triple(colors.danger, colors.dangerSoft, "这一版解析不了，对话里不会用它：${detail.problem}")
    }
    Panel(color = fill) {
        Row(verticalAlignment = Alignment.Top) {
            StatusDot(dot, modifier = Modifier.padding(top = 6.dp))
            Spacer(Modifier.width(10.dp))
            Text(text, style = AcornTheme.type.callout, color = colors.ink)
        }
    }
}

@Composable
internal fun SkillSectionTitle(text: String) {
    Text(
        text,
        style = AcornTheme.type.footnote,
        color = AcornTheme.colors.inkFaint,
        modifier = Modifier.padding(top = 28.dp, bottom = 8.dp),
    )
}

/** SKILL.md's instruction as rendered markdown; the whole file as text when it does not parse. */
@Composable
internal fun SkillContent(instruction: String?, markdown: String) {
    Panel {
        if (instruction != null) {
            Markdown(instruction)
        } else {
            Text(markdown, style = AcornTheme.type.mono, color = AcornTheme.colors.ink)
        }
    }
}

@Composable
private fun LinkRow(text: String, onClick: () -> Unit) {
    val colors = AcornTheme.colors
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(AcornShapes.small)
            .clickable(onClick = onClick)
            .padding(vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(text, style = AcornTheme.type.callout, color = colors.accent, modifier = Modifier.weight(1f))
        Icon(AcornIcons.ChevronRight, contentDescription = null, tint = colors.inkFaint, modifier = Modifier.size(18.dp))
    }
}

/** The files a skill carries; a text file opens in place. */
@Composable
internal fun SkillFiles(files: List<SkillFile>) {
    val colors = AcornTheme.colors
    var open by rememberSaveable { mutableStateOf<String?>(null) }
    Panel(contentPadding = PaddingValues(0.dp)) {
        files.forEachIndexed { index, file ->
            if (index > 0) Hairline(inset = 14.dp)
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clickable { open = if (open == file.path) null else file.path }
                    .padding(horizontal = 14.dp, vertical = 12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(file.path, style = AcornTheme.type.mono, color = colors.ink, modifier = Modifier.weight(1f))
                Spacer(Modifier.width(10.dp))
                Text(fileSize(file.propertySize), style = AcornTheme.type.caption, color = colors.inkFaint)
            }
            AnimatedVisibility(open == file.path) {
                val content = file.content
                if (content != null) {
                    Text(
                        content.ifEmpty { "（空文件）" },
                        style = AcornTheme.type.mono,
                        color = colors.inkSoft,
                        modifier = Modifier
                            .fillMaxWidth()
                            .background(colors.surfaceMuted)
                            .padding(horizontal = 14.dp, vertical = 12.dp),
                    )
                } else {
                    Text(
                        hiddenFileReason(file.propertySize),
                        style = AcornTheme.type.footnote,
                        color = colors.inkFaint,
                        modifier = Modifier.padding(start = 14.dp, end = 14.dp, bottom = 12.dp),
                    )
                }
            }
        }
    }
}

/** What the package runs on the workstation and how it stands, then its secrets. */
@Composable
private fun PackageSection(
    pkg: SkillPackage,
    now: OffsetDateTime,
    onSetSecret: (String) -> Unit,
    onClearSecret: (String) -> Unit,
    secretsEnabled: Boolean,
) {
    val colors = AcornTheme.colors
    pkg.mcp?.let { server ->
        SkillSectionTitle("MCP 服务器")
        Panel {
            Text(server.commandLine, style = AcornTheme.type.mono, color = colors.ink)
            Spacer(Modifier.height(8.dp))
            StateLine(serverStateLabel(server.state, server.error), serverStateColor(server.state))
        }
    }
    if (pkg.triggers.isNotEmpty()) {
        SkillSectionTitle("触发器")
        Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
            pkg.triggers.forEach { trigger ->
                Panel {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(trigger.name, style = AcornTheme.type.callout.copy(fontWeight = FontWeight.Medium), color = colors.ink, modifier = Modifier.weight(1f))
                        Text(trigger.schedule, style = AcornTheme.type.mono, color = colors.inkSoft)
                    }
                    Text(deliverLabel(trigger.deliver), style = AcornTheme.type.caption, color = colors.inkFaint)
                    Spacer(Modifier.height(6.dp))
                    Text(trigger.run, style = AcornTheme.type.mono, color = colors.inkSoft)
                    Spacer(Modifier.height(8.dp))
                    StateLine(triggerStateLabel(trigger, now), triggerStateColor(trigger.state))
                }
            }
        }
    }
    if (pkg.secrets.isNotEmpty()) {
        SkillSectionTitle("密钥")
        Panel(contentPadding = PaddingValues(0.dp)) {
            pkg.secrets.forEachIndexed { index, secret ->
                if (index > 0) Hairline(inset = 14.dp)
                Row(
                    modifier = Modifier.fillMaxWidth().padding(start = 14.dp, end = 8.dp, top = 10.dp, bottom = 10.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Column(modifier = Modifier.weight(1f)) {
                        Text(secret.name, style = AcornTheme.type.mono, color = colors.ink)
                        Text(
                            if (secret.set) "已设置 ••••••" else "未设置",
                            style = AcornTheme.type.caption,
                            color = if (secret.set) colors.success else colors.warningInk,
                        )
                    }
                    if (secret.set) {
                        AcornButton("清除", { onClearSecret(secret.name) }, kind = ButtonKind.Quiet, compact = true, enabled = secretsEnabled)
                    }
                    AcornButton(if (secret.set) "更换" else "设置", { onSetSecret(secret.name) }, kind = ButtonKind.Secondary, compact = true, enabled = secretsEnabled)
                }
            }
        }
        Text(
            "值只写不读，只交给这个技能包的进程，不进入对话和审批。",
            style = AcornTheme.type.caption,
            color = colors.inkFaint,
            modifier = Modifier.padding(top = 6.dp, start = 2.dp),
        )
    }
}

@Composable
private fun StateLine(text: String, dot: Color) {
    Row(verticalAlignment = Alignment.Top) {
        StatusDot(dot, modifier = Modifier.padding(top = 5.dp))
        Spacer(Modifier.width(8.dp))
        Text(text, style = AcornTheme.type.footnote, color = AcornTheme.colors.inkSoft)
    }
}

@Composable
private fun serverStateColor(state: SkillPackageServer.State?): Color {
    val colors = AcornTheme.colors
    return when (state) {
        SkillPackageServer.State.ready -> colors.success
        SkillPackageServer.State.failed, SkillPackageServer.State.unknown -> colors.danger
        SkillPackageServer.State.missing_secrets, SkillPackageServer.State.off -> colors.warning
        SkillPackageServer.State.stopped, null -> colors.inkFaint
    }
}

@Composable
private fun triggerStateColor(state: SkillPackageTrigger.State?): Color {
    val colors = AcornTheme.colors
    return when (state) {
        SkillPackageTrigger.State.scheduled -> colors.success
        SkillPackageTrigger.State.failing -> colors.danger
        SkillPackageTrigger.State.missing_secrets, SkillPackageTrigger.State.off -> colors.warning
        SkillPackageTrigger.State.unscheduled, null -> colors.inkFaint
    }
}
