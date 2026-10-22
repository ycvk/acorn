package io.ycvk.acorn.feature.settings

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.FlowRow
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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Icon
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
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
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.OffsetMapping
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.text.input.TransformedText
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.OwnerSettingName
import io.ycvk.acorn.api.models.OwnerSettings
import io.ycvk.acorn.api.models.OwnerSettingsChanges
import io.ycvk.acorn.api.models.QuietHours
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornButton
import io.ycvk.acorn.core.ui.AcornDialog
import io.ycvk.acorn.core.ui.AcornIcons
import io.ycvk.acorn.core.ui.ButtonKind
import io.ycvk.acorn.core.ui.ErrorState
import io.ycvk.acorn.core.ui.FilterPill
import io.ycvk.acorn.core.ui.Hairline
import io.ycvk.acorn.core.ui.ScreenPadding
import io.ycvk.acorn.core.ui.SkeletonRows
import io.ycvk.acorn.core.ui.Tag
import io.ycvk.acorn.core.ui.TopBar
import java.time.ZoneId

/** The most idle thought times a day holds. */
private const val MAX_WANDER_TIMES = 6

/**
 * The owner's time zone, routine times, push quiet hours, daily budgets and
 * approval rules. Each row says what the setting does; tapping it edits that
 * one setting, which applies from its next use.
 */
@Composable
fun OwnerSettingsScreen(onBack: () -> Unit, viewModel: OwnerSettingsViewModel = hiltViewModel()) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val saving by viewModel.saving.collectAsStateWithLifecycle()
    val saveError by viewModel.saveError.collectAsStateWithLifecycle()
    val toolNames by viewModel.toolNames.collectAsStateWithLifecycle()
    val toolsError by viewModel.toolsError.collectAsStateWithLifecycle()
    var editing by remember { mutableStateOf<OwnerSettingName?>(null) }

    LaunchedEffect(Unit) { viewModel.load() }

    Column(modifier = Modifier.fillMaxSize().background(AcornTheme.colors.canvas)) {
        TopBar(onBack = onBack, title = "作息、预算与审批")
        when (val load = state) {
            OwnerSettingsViewModel.Load.Loading -> SkeletonRows(Modifier.padding(horizontal = ScreenPadding, vertical = 16.dp))
            is OwnerSettingsViewModel.Load.Failed -> ErrorState(load.message, onRetry = viewModel::load)
            is OwnerSettingsViewModel.Load.Loaded -> OwnerSettingsContent(load.settings) {
                viewModel.dismissSaveError()
                editing = it
            }
        }
    }

    val settings = (state as? OwnerSettingsViewModel.Load.Loaded)?.settings
    val name = editing
    if (settings != null && name != null) {
        SettingEditor(
            name = name,
            settings = settings,
            toolNames = toolNames,
            toolsError = toolsError,
            saving = saving,
            saveError = saveError,
            onSave = { changes -> viewModel.save(changes) { editing = null } },
            onReset = { viewModel.reset(name) { editing = null } },
            onDismiss = {
                if (!saving) {
                    viewModel.dismissSaveError()
                    editing = null
                }
            },
        )
    }
}

@Composable
private fun OwnerSettingsContent(settings: OwnerSettings, onEdit: (OwnerSettingName) -> Unit) {
    val values = settings.propertyValues
    val usage = settings.usage
    val row: @Composable (OwnerSettingName, String, String?) -> Unit = { name, value, used ->
        OwnerSettingRow(name, value, customized = name in settings.customized, used = used, onClick = { onEdit(name) })
    }
    Column(
        modifier = Modifier
            .verticalScroll(rememberScrollState())
            .navigationBarsPadding()
            .padding(bottom = 32.dp),
    ) {
        Text(
            "这些设置只有你能改，Acorn 自己改不了。保存后不用重启，从下一次用到起生效。",
            style = AcornTheme.type.caption,
            color = AcornTheme.colors.inkFaint,
            modifier = Modifier.padding(horizontal = ScreenPadding, vertical = 8.dp),
        )
        SettingsGroup("作息") {
            row(OwnerSettingName.owner_timezone, values.ownerTimezone, null)
            Hairline(inset = 16.dp)
            row(OwnerSettingName.briefing_at, dailyTimeLabel(values.briefingAt), null)
            Hairline(inset = 16.dp)
            row(OwnerSettingName.thinking_night_at, dailyTimeLabel(values.thinkingNightAt), null)
            Hairline(inset = 16.dp)
            row(OwnerSettingName.thinking_wander_at, wanderLabel(values.thinkingWanderAt), null)
        }
        SettingsGroup("通知") {
            row(OwnerSettingName.notify_quiet_hours, quietHoursLabel(values.notifyQuietHours), null)
            Hairline(inset = 16.dp)
            row(OwnerSettingName.notify_max_per_hour, "${values.notifyMaxPerHour} 条", null)
        }
        SettingsGroup("每日预算") {
            row(OwnerSettingName.wake_daily_limit, wakeLimitLabel(values.wakeDailyLimit), "今天已醒 ${usage.wakes} 次")
            Hairline(inset = 16.dp)
            row(OwnerSettingName.wake_daily_tokens, tokenBudgetLabel(values.wakeDailyTokens), "今天已用 ${tokensLabel(usage.wakeTokens)}")
            Hairline(inset = 16.dp)
            row(OwnerSettingName.memory_daily_tokens, tokenBudgetLabel(values.memoryDailyTokens), "今天已用 ${tokensLabel(usage.memoryTokens)}")
            Hairline(inset = 16.dp)
            row(OwnerSettingName.news_daily_tokens, tokenBudgetLabel(values.newsDailyTokens), "今天已用 ${tokensLabel(usage.newsTokens)}")
        }
        SettingsGroup("审批规则") {
            row(OwnerSettingName.approval_require, patternsLabel(values.approvalRequire), null)
            Hairline(inset = 16.dp)
            row(OwnerSettingName.approval_block, patternsLabel(values.approvalBlock), null)
            Hairline(inset = 16.dp)
            row(OwnerSettingName.approval_allow, patternsLabel(values.approvalAllow), null)
        }
        Text(
            "工具名可以用 * 通配，例如 mcp__* 是所有 MCP 工具。一律拒绝优先于每次等我批准，后者优先于直接执行；都没命中的命令和 MCP 调用交给审查。",
            style = AcornTheme.type.caption,
            color = AcornTheme.colors.inkFaint,
            modifier = Modifier.padding(horizontal = ScreenPadding, vertical = 8.dp),
        )
    }
}

@Composable
private fun OwnerSettingRow(name: OwnerSettingName, value: String, customized: Boolean, used: String?, onClick: () -> Unit) {
    val colors = AcornTheme.colors
    val copy = settingCopy(name)
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 12.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(copy.title, style = AcornTheme.type.callout, color = colors.ink)
            if (!customized) {
                Spacer(Modifier.width(8.dp))
                Tag("默认")
            }
            Spacer(Modifier.width(16.dp))
            Spacer(Modifier.weight(1f))
            Text(value, style = AcornTheme.type.callout, color = colors.inkSoft, maxLines = 1)
            Spacer(Modifier.width(4.dp))
            Icon(AcornIcons.ChevronRight, contentDescription = null, tint = colors.inkFaint, modifier = Modifier.size(18.dp))
        }
        Spacer(Modifier.height(4.dp))
        Text(copy.description, style = AcornTheme.type.caption, color = colors.inkFaint)
        used?.let {
            Spacer(Modifier.height(2.dp))
            Text(it, style = AcornTheme.type.caption, color = colors.accent)
        }
    }
}

@Composable
private fun SettingEditor(
    name: OwnerSettingName,
    settings: OwnerSettings,
    toolNames: List<String>,
    toolsError: String?,
    saving: Boolean,
    saveError: String?,
    onSave: (OwnerSettingsChanges) -> Unit,
    onReset: () -> Unit,
    onDismiss: () -> Unit,
) {
    val values = settings.propertyValues
    val defaults = settings.defaults
    val frame = EditorFrame(name, customized = name in settings.customized, saving, saveError, onReset, onDismiss)
    when (name) {
        OwnerSettingName.owner_timezone -> ZoneEditor(frame, values.ownerTimezone, defaults.ownerTimezone) { onSave(OwnerSettingsChanges(ownerTimezone = it)) }
        OwnerSettingName.briefing_at -> TimeEditor(frame, values.briefingAt, defaults.briefingAt) { onSave(OwnerSettingsChanges(briefingAt = it)) }
        OwnerSettingName.thinking_night_at -> TimeEditor(frame, values.thinkingNightAt, defaults.thinkingNightAt) { onSave(OwnerSettingsChanges(thinkingNightAt = it)) }
        OwnerSettingName.thinking_wander_at -> WanderEditor(frame, values.thinkingWanderAt, defaults.thinkingWanderAt) { onSave(OwnerSettingsChanges(thinkingWanderAt = it)) }
        OwnerSettingName.notify_quiet_hours -> QuietHoursEditor(frame, values.notifyQuietHours, defaults.notifyQuietHours) { onSave(OwnerSettingsChanges(notifyQuietHours = it)) }
        OwnerSettingName.notify_max_per_hour -> NumberEditor(frame, values.notifyMaxPerHour, defaults.notifyMaxPerHour, least = 1, unit = "条", zero = null) { onSave(OwnerSettingsChanges(notifyMaxPerHour = it)) }
        OwnerSettingName.wake_daily_limit -> NumberEditor(frame, values.wakeDailyLimit, defaults.wakeDailyLimit, least = 0, unit = "次", zero = "关闭") { onSave(OwnerSettingsChanges(wakeDailyLimit = it)) }
        OwnerSettingName.wake_daily_tokens -> NumberEditor(frame, values.wakeDailyTokens, defaults.wakeDailyTokens, least = 0, unit = "token", zero = "不限") { onSave(OwnerSettingsChanges(wakeDailyTokens = it)) }
        OwnerSettingName.memory_daily_tokens -> NumberEditor(frame, values.memoryDailyTokens, defaults.memoryDailyTokens, least = 0, unit = "token", zero = "不限") { onSave(OwnerSettingsChanges(memoryDailyTokens = it)) }
        OwnerSettingName.news_daily_tokens -> NumberEditor(frame, values.newsDailyTokens, defaults.newsDailyTokens, least = 0, unit = "token", zero = "不限") { onSave(OwnerSettingsChanges(newsDailyTokens = it)) }
        OwnerSettingName.approval_block -> PatternsEditor(frame, values.approvalBlock, defaults.approvalBlock, ruleTools(toolNames, name), toolsError) { onSave(OwnerSettingsChanges(approvalBlock = it)) }
        OwnerSettingName.approval_require -> PatternsEditor(frame, values.approvalRequire, defaults.approvalRequire, ruleTools(toolNames, name), toolsError) { onSave(OwnerSettingsChanges(approvalRequire = it)) }
        OwnerSettingName.approval_allow -> PatternsEditor(frame, values.approvalAllow, defaults.approvalAllow, ruleTools(toolNames, name), toolsError) { onSave(OwnerSettingsChanges(approvalAllow = it)) }
    }
}

/** What every editor shares: the setting, the save state and the way out. */
private class EditorFrame(
    val name: OwnerSettingName,
    val customized: Boolean,
    val saving: Boolean,
    val saveError: String?,
    val onReset: () -> Unit,
    val onDismiss: () -> Unit,
)

@Composable
private fun EditorDialog(frame: EditorFrame, default: String, canSave: Boolean, onSave: () -> Unit, content: @Composable ColumnScope.() -> Unit) {
    val colors = AcornTheme.colors
    val copy = settingCopy(frame.name)
    AcornDialog(
        title = copy.title,
        body = copy.description,
        confirm = "保存",
        confirmEnabled = canSave && !frame.saving,
        confirmLoading = frame.saving,
        dismissEnabled = !frame.saving,
        onConfirm = onSave,
        onDismiss = frame.onDismiss,
    ) {
        content()
        if (frame.customized) {
            Spacer(Modifier.height(12.dp))
            Text(
                "恢复默认：$default",
                style = AcornTheme.type.callout,
                color = colors.accent,
                modifier = Modifier
                    .clip(AcornShapes.small)
                    .clickable(enabled = !frame.saving, onClick = frame.onReset)
                    .padding(vertical = 6.dp),
            )
        }
        frame.saveError?.let {
            Spacer(Modifier.height(8.dp))
            Text(it, style = AcornTheme.type.caption, color = colors.danger)
        }
    }
}

@Composable
private fun OnOffRow(label: String, on: Boolean, onChange: (Boolean) -> Unit) {
    val colors = AcornTheme.colors
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(AcornShapes.small)
            .toggleable(value = on, role = Role.Switch, onValueChange = onChange)
            .padding(vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(label, style = AcornTheme.type.callout, color = colors.ink, modifier = Modifier.weight(1f))
        Switch(checked = on, onCheckedChange = null, colors = SwitchDefaults.colors(checkedTrackColor = colors.accent, checkedThumbColor = colors.onAccent))
    }
}

@Composable
private fun TimeEditor(frame: EditorFrame, value: String, default: String, onSave: (String) -> Unit) {
    var on by remember { mutableStateOf(value.isNotEmpty()) }
    var digits by remember { mutableStateOf(clockDigits(value.ifEmpty { default })) }
    val time = clockFromDigits(digits)
    EditorDialog(frame, dailyTimeLabel(default), canSave = !on || time != null, onSave = { onSave(if (on) time.orEmpty() else "") }) {
        OnOffRow("开启", on) { on = it }
        if (on) {
            Spacer(Modifier.height(8.dp))
            ClockField(digits, valid = time != null) { digits = it }
        }
    }
}

@Composable
private fun WanderEditor(frame: EditorFrame, value: List<String>, default: List<String>, onSave: (List<String>) -> Unit) {
    val colors = AcornTheme.colors
    var times by remember { mutableStateOf(value) }
    var digits by remember { mutableStateOf("") }
    val time = clockFromDigits(digits)
    EditorDialog(frame, wanderLabel(default), canSave = true, onSave = { onSave(times) }) {
        if (times.isEmpty()) {
            Text("还没有游思时刻。", style = AcornTheme.type.callout, color = colors.inkFaint)
        }
        times.forEach { at ->
            Row(modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(at, style = AcornTheme.type.callout, color = colors.ink, modifier = Modifier.weight(1f))
                Icon(
                    AcornIcons.Close,
                    contentDescription = "去掉 $at",
                    tint = colors.inkSoft,
                    modifier = Modifier.size(28.dp).clip(AcornShapes.small).clickable { times = times - at }.padding(5.dp),
                )
            }
        }
        if (times.size < MAX_WANDER_TIMES) {
            Spacer(Modifier.height(12.dp))
            Row(verticalAlignment = Alignment.CenterVertically) {
                ClockField(digits, valid = time != null || digits.isEmpty(), modifier = Modifier.weight(1f)) { digits = it }
                Spacer(Modifier.width(8.dp))
                AcornButton(
                    "添加",
                    onClick = {
                        time?.let { times = (times + it).distinct().sorted() }
                        digits = ""
                    },
                    kind = ButtonKind.Secondary,
                    compact = true,
                    enabled = time != null && time !in times,
                )
            }
        }
    }
}

@Composable
private fun QuietHoursEditor(frame: EditorFrame, value: QuietHours, default: QuietHours, onSave: (QuietHours) -> Unit) {
    val colors = AcornTheme.colors
    var on by remember { mutableStateOf(value.start.isNotEmpty()) }
    var startDigits by remember { mutableStateOf(clockDigits(value.start.ifEmpty { default.start.ifEmpty { "23:00" } })) }
    var endDigits by remember { mutableStateOf(clockDigits(value.end.ifEmpty { default.end.ifEmpty { "08:00" } })) }
    val start = clockFromDigits(startDigits)
    val end = clockFromDigits(endDigits)
    EditorDialog(
        frame,
        quietHoursLabel(default),
        canSave = !on || (start != null && end != null),
        onSave = { onSave(if (on) QuietHours(start = start.orEmpty(), end = end.orEmpty()) else QuietHours(start = "", end = "")) },
    ) {
        OnOffRow("开启", on) { on = it }
        if (on) {
            Spacer(Modifier.height(8.dp))
            Row(verticalAlignment = Alignment.CenterVertically) {
                ClockField(startDigits, valid = start != null, modifier = Modifier.weight(1f)) { startDigits = it }
                Text("到", style = AcornTheme.type.callout, color = colors.inkSoft, modifier = Modifier.padding(horizontal = 10.dp))
                ClockField(endDigits, valid = end != null, modifier = Modifier.weight(1f)) { endDigits = it }
            }
            if (start != null && start == end) {
                Spacer(Modifier.height(6.dp))
                Text("开始和结束相同时不免打扰。", style = AcornTheme.type.caption, color = colors.inkFaint)
            }
        }
    }
}

/**
 * A time of day typed as four digits; the colon shows itself. Opening it
 * selects what it holds, so typing replaces the whole time.
 */
@Composable
private fun ClockField(digits: String, valid: Boolean, modifier: Modifier = Modifier, onChange: (String) -> Unit) {
    val colors = AcornTheme.colors
    var field by remember { mutableStateOf(TextFieldValue(digits, selection = TextRange(0, digits.length))) }
    if (field.text != digits) field = TextFieldValue(digits, selection = TextRange(digits.length))
    BasicTextField(
        value = field,
        onValueChange = { next ->
            val kept = clockDigits(next.text)
            field = next.copy(text = kept, selection = TextRange(minOf(next.selection.end, kept.length)))
            onChange(kept)
        },
        singleLine = true,
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number, imeAction = ImeAction.Done),
        visualTransformation = ClockColon,
        textStyle = AcornTheme.type.title.copy(color = if (valid) colors.ink else colors.danger),
        cursorBrush = SolidColor(colors.accent),
        decorationBox = { inner ->
            if (digits.isEmpty()) Text("例如 0730", style = AcornTheme.type.callout, color = colors.inkFaint)
            inner()
        },
        modifier = modifier
            .fillMaxWidth()
            .heightIn(min = 48.dp)
            .clip(AcornShapes.medium)
            .background(colors.surfaceMuted)
            .padding(horizontal = 14.dp, vertical = 10.dp),
    )
}

/** Shows "0730" as "07:30" while the field holds only digits. */
private object ClockColon : VisualTransformation {
    override fun filter(text: AnnotatedString): TransformedText {
        if (text.length <= 2) return TransformedText(text, OffsetMapping.Identity)
        val shown = AnnotatedString(text.text.take(2) + ":" + text.text.drop(2))
        return TransformedText(shown, object : OffsetMapping {
            override fun originalToTransformed(offset: Int) = if (offset <= 2) offset else offset + 1
            override fun transformedToOriginal(offset: Int) = if (offset <= 2) offset else (offset - 1).coerceAtMost(text.length)
        })
    }
}

@Composable
private fun NumberEditor(frame: EditorFrame, value: Int, default: Int, least: Int, unit: String, zero: String?, onSave: (Int) -> Unit) {
    val colors = AcornTheme.colors
    var text by remember { mutableStateOf(value.toString()) }
    val number = text.toIntOrNull()
    val defaultLabel = when {
        default == 0 && zero != null -> zero
        unit == "token" -> "${tokensLabel(default)} token"
        else -> "$default $unit"
    }
    EditorDialog(frame, defaultLabel, canSave = number != null && number >= least, onSave = { number?.let(onSave) }) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            BasicTextField(
                value = text,
                onValueChange = { input -> text = input.filter(Char::isDigit).take(9) },
                singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number, imeAction = ImeAction.Done),
                textStyle = AcornTheme.type.callout.copy(color = colors.ink),
                cursorBrush = SolidColor(colors.accent),
                modifier = Modifier
                    .weight(1f)
                    .heightIn(min = 44.dp)
                    .clip(AcornShapes.medium)
                    .background(colors.surfaceMuted)
                    .padding(horizontal = 14.dp, vertical = 12.dp),
            )
            Spacer(Modifier.width(10.dp))
            Text(unit, style = AcornTheme.type.callout, color = colors.inkSoft)
        }
        val hint = when {
            number != null && number < least -> "至少 $least。"
            number == 0 && zero != null -> "0 表示$zero。"
            number != null && unit == "token" -> "约 ${tokensLabel(number)} token。"
            else -> null
        }
        hint?.let {
            Spacer(Modifier.height(6.dp))
            Text(it, style = AcornTheme.type.caption, color = colors.inkFaint)
        }
    }
}

@Composable
private fun ZoneEditor(frame: EditorFrame, value: String, default: String, onSave: (String) -> Unit) {
    val colors = AcornTheme.colors
    var query by remember { mutableStateOf("") }
    var picked by remember { mutableStateOf(value) }
    val phone = remember { ZoneId.systemDefault().id }
    val zones = remember { ZoneId.getAvailableZoneIds() }
    val choices = remember(query) { zoneChoices(zones, phone, query) }
    EditorDialog(frame, default, canSave = picked.isNotEmpty(), onSave = { onSave(picked) }) {
        SearchField(query, "搜索，例如 Shanghai") { query = it }
        Spacer(Modifier.height(8.dp))
        LazyColumn(modifier = Modifier.fillMaxWidth().heightIn(max = 260.dp)) {
            items(choices, key = { it }) { zone ->
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(AcornShapes.small)
                        .clickable { picked = zone }
                        .padding(horizontal = 4.dp, vertical = 10.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        if (zone == phone) "$zone（这台手机）" else zone,
                        style = AcornTheme.type.callout,
                        color = colors.ink,
                        modifier = Modifier.weight(1f),
                    )
                    if (zone == picked) {
                        Icon(AcornIcons.Check, contentDescription = "已选", tint = colors.accent, modifier = Modifier.size(18.dp))
                    }
                }
            }
        }
    }
}

@Composable
private fun PatternsEditor(frame: EditorFrame, value: List<String>, default: List<String>, toolNames: List<String>, toolsError: String?, onSave: (List<String>) -> Unit) {
    val colors = AcornTheme.colors
    var patterns by remember { mutableStateOf(value) }
    var typed by remember { mutableStateOf("") }
    val suggestions = toolSuggestions(toolNames, typed, patterns)
    val add = { pattern: String ->
        val text = pattern.trim()
        if (text.isNotEmpty() && text !in patterns) patterns = patterns + text
        typed = ""
    }
    EditorDialog(frame, patternsLabel(default), canSave = true, onSave = { onSave(patterns) }) {
        if (patterns.isEmpty()) {
            Text("没有规则。", style = AcornTheme.type.callout, color = colors.inkFaint)
        }
        patterns.forEach { pattern ->
            Row(modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(pattern, style = AcornTheme.type.callout, color = colors.ink, modifier = Modifier.weight(1f))
                Icon(
                    AcornIcons.Close,
                    contentDescription = "去掉 $pattern",
                    tint = colors.inkSoft,
                    modifier = Modifier.size(28.dp).clip(AcornShapes.small).clickable { patterns = patterns - pattern }.padding(5.dp),
                )
            }
        }
        Spacer(Modifier.height(10.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            SearchField(typed, "工具名，可用 *", Modifier.weight(1f)) { typed = it }
            Spacer(Modifier.width(8.dp))
            AcornButton("添加", onClick = { add(typed) }, kind = ButtonKind.Secondary, compact = true, enabled = typed.isNotBlank())
        }
        if (suggestions.isNotEmpty()) {
            Spacer(Modifier.height(8.dp))
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                suggestions.forEach { name -> FilterPill(name, selected = false, onClick = { add(name) }) }
            }
        }
        toolsError?.let {
            Spacer(Modifier.height(6.dp))
            Text("没取到工具列表：$it。可以直接输入工具名。", style = AcornTheme.type.caption, color = colors.inkFaint)
        }
    }
}

@Composable
private fun SearchField(value: String, placeholder: String, modifier: Modifier = Modifier, onChange: (String) -> Unit) {
    val colors = AcornTheme.colors
    BasicTextField(
        value = value,
        onValueChange = onChange,
        singleLine = true,
        keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
        textStyle = AcornTheme.type.callout.copy(color = colors.ink),
        cursorBrush = SolidColor(colors.accent),
        decorationBox = { field ->
            if (value.isEmpty()) Text(placeholder, style = AcornTheme.type.callout, color = colors.inkFaint)
            field()
        },
        modifier = modifier
            .fillMaxWidth()
            .heightIn(min = 44.dp)
            .clip(AcornShapes.medium)
            .background(colors.surfaceMuted)
            .padding(horizontal = 14.dp, vertical = 12.dp),
    )
}
