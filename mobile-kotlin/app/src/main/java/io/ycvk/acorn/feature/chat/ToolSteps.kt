package io.ycvk.acorn.feature.chat

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.ycvk.acorn.core.sse.StepStatus
import io.ycvk.acorn.core.sse.ToolStep
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornIcons
import io.ycvk.acorn.core.ui.OrbState
import io.ycvk.acorn.core.ui.ThinkingOrb
import io.ycvk.acorn.core.ui.shimmer

/** What a tool does, in the words the owner would use. */
fun toolLabel(name: String): String = when (name) {
    "recall" -> "回忆"
    "memory_read" -> "查看记忆"
    "memory_correct" -> "修正记忆"
    "memory_forget" -> "忘掉一段记忆"
    "keep" -> "记下一件事"
    "think" -> "想一想"
    "settle" -> "了结一件事"
    "concern" -> "跟进一件事"
    "schedule_wake" -> "约定提醒"
    "notify_owner" -> "发通知"
    "ask_operator" -> "问你"
    "knowledge_write" -> "写笔记"
    "knowledge_edit" -> "改笔记"
    "knowledge_read" -> "读笔记"
    "knowledge_search" -> "搜索知识库"
    "knowledge_list" -> "浏览知识库"
    "watch_create" -> "新建追踪"
    "watch_update" -> "调整追踪"
    "watch_list" -> "查看追踪"
    "web_search" -> "搜索网页"
    "web_fetch" -> "读取网页"
    "browser" -> "用浏览器打开"
    "shell" -> "运行命令"
    "files" -> "读写文件"
    "send_file" -> "发送文件"
    "show_view" -> "整理成视图"
    "update_view" -> "更新视图"
    "artifact_write" -> "保存文件"
    "artifact_read" -> "读取文件"
    "artifact_list" -> "查看文件"
    "tool_search" -> "加载工具"
    "skill" -> "使用技能"
    "skill_write" -> "写技能"
    "skill_retire" -> "停用技能"
    "skill_inspect" -> "检查技能包"
    "skill_import" -> "导入技能"
    else -> name
}

/** The orb a running tool shows: looking things up, working on the machine, reaching a skill, reasoning or writing down. */
fun toolOrb(name: String): OrbState = when {
    name.startsWith("mcp__") -> OrbState.Connecting
    else -> when (name) {
        "recall", "memory_read", "knowledge_read", "knowledge_search", "knowledge_list", "watch_list",
        "web_search", "web_fetch", "artifact_read", "artifact_list", "tool_search" -> OrbState.Searching
        "shell", "files", "browser", "send_file", "skill_inspect" -> OrbState.Working
        "skill", "skill_write", "skill_import", "skill_retire", "watch_create", "watch_update" -> OrbState.Connecting
        "think", "concern", "settle", "schedule_wake", "ask_operator", "notify_owner" -> OrbState.Solving
        "keep", "memory_correct", "memory_forget", "knowledge_write", "knowledge_edit", "artifact_write", "show_view", "update_view" -> OrbState.Weaving
        else -> OrbState.Working
    }
}

/** The step's detail line; a tool search names the tools it loads. */
fun stepDetail(step: ToolStep): String = when {
    step.toolName == "tool_search" && step.summary.startsWith("select:") ->
        step.summary.removePrefix("select:").split(',').map { toolLabel(it.trim()) }.joinToString("、")
    else -> step.summary
}

/** One line per step: while a call runs it shows what is happening; afterwards it folds into a count. */
@Composable
fun ToolSteps(steps: List<ToolStep>, streaming: Boolean, modifier: Modifier = Modifier) {
    if (steps.isEmpty()) return
    val colors = AcornTheme.colors
    var expanded by rememberSaveable { mutableStateOf(false) }
    val rotation by animateFloatAsState(if (expanded) 180f else 0f, label = "steps_chevron")
    val running = steps.lastOrNull { it.status == StepStatus.Running }.takeIf { streaming }
    val failures = steps.count { it.status == StepStatus.Failed }
    Column(modifier = modifier) {
        Row(
            modifier = Modifier
                .clip(AcornShapes.small)
                .clickable { expanded = !expanded }
                .padding(vertical = 4.dp, horizontal = 2.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            if (running != null) {
                ThinkingOrb(toolOrb(running.toolName), size = 18.dp)
                Spacer(Modifier.width(8.dp))
                val detail = stepDetail(running)
                Text(
                    if (detail.isEmpty()) "正在${toolLabel(running.toolName)}" else "正在${toolLabel(running.toolName)}：$detail",
                    style = AcornTheme.type.footnote,
                    color = colors.inkSoft,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f, fill = false).shimmer(true),
                )
            } else {
                Icon(
                    if (failures > 0) Icons.Outlined.ErrorOutline else Icons.Outlined.CheckCircle,
                    contentDescription = null,
                    tint = if (failures > 0) colors.warning else colors.inkFaint,
                    modifier = Modifier.size(15.dp),
                )
                Spacer(Modifier.width(6.dp))
                Text("用了 ${steps.size} 步", style = AcornTheme.type.footnote, color = colors.inkFaint)
                if (failures > 0) {
                    Text("，$failures 步出错", style = AcornTheme.type.footnote, color = colors.warningInk)
                }
            }
            Icon(AcornIcons.ChevronDown, contentDescription = if (expanded) "收起步骤" else "展开步骤", tint = colors.inkFaint, modifier = Modifier.size(16.dp).rotate(rotation))
        }
        AnimatedVisibility(visible = expanded, enter = expandVertically() + fadeIn(), exit = shrinkVertically() + fadeOut()) {
            Row(modifier = Modifier.padding(start = 7.dp, top = 4.dp, bottom = 2.dp).height(IntrinsicSize.Min)) {
                Box(
                    Modifier
                        .width(1.5.dp)
                        .fillMaxHeight()
                        .background(colors.line, RoundedCornerShape(1.dp)),
                )
                Spacer(Modifier.width(12.dp))
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    steps.forEach { StepRow(it, streaming) }
                }
            }
        }
    }
}

@Composable
private fun StepRow(step: ToolStep, streaming: Boolean) {
    val colors = AcornTheme.colors
    Row(verticalAlignment = Alignment.Top) {
        Box(modifier = Modifier.padding(top = 2.dp).size(15.dp), contentAlignment = Alignment.Center) {
            when {
                step.status == StepStatus.Failed -> Icon(Icons.Outlined.ErrorOutline, contentDescription = "出错", tint = colors.danger, modifier = Modifier.size(15.dp))
                step.status == StepStatus.Running && streaming -> ThinkingOrb(toolOrb(step.toolName), size = 15.dp)
                step.status == StepStatus.Running -> Icon(Icons.Outlined.CheckCircle, contentDescription = null, tint = colors.line, modifier = Modifier.size(15.dp))
                else -> Icon(Icons.Outlined.CheckCircle, contentDescription = "完成", tint = colors.inkFaint, modifier = Modifier.size(15.dp))
            }
        }
        Spacer(Modifier.width(8.dp))
        Column {
            Text(toolLabel(step.toolName), style = AcornTheme.type.footnote, color = colors.ink)
            stepDetail(step).takeIf { it.isNotEmpty() }?.let {
                Text(it, style = AcornTheme.type.caption, color = colors.inkSoft, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
            step.error?.takeIf { it.isNotEmpty() }?.let {
                Text(it, style = AcornTheme.type.caption, color = colors.danger, maxLines = 3, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}
