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
import androidx.compose.material.icons.outlined.AccountTree
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
import io.ycvk.acorn.api.models.SubRunData
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornIcons
import io.ycvk.acorn.core.ui.OrbState
import io.ycvk.acorn.core.ui.ThinkingOrb
import io.ycvk.acorn.core.ui.shimmer

/** [tasks] with [update] in place of its task's earlier attempt, in task order. */
fun withSubRun(tasks: List<SubRunData>, update: SubRunData): List<SubRunData> =
    (tasks.filterNot { it.task == update.task } + update).sortedBy { it.task }

/** The tasks that still run. */
fun runningSubRuns(tasks: List<SubRunData>): Int = tasks.count { it.state == SubRunData.State.running }

/** The line the tasks fold into: how many are still at work, or how they ended. */
fun subRunsHeadline(tasks: List<SubRunData>): String {
    val running = runningSubRuns(tasks)
    val failed = tasks.count { it.state == SubRunData.State.failed }
    return when {
        running > 0 -> "${tasks.size} 个子任务，$running 个在做"
        failed > 0 -> "${tasks.size} 个子任务，$failed 个失败"
        else -> "${tasks.size} 个子任务都已交回"
    }
}

/** Where a task stands, with its attempt once it was sent back. */
fun subRunStateLabel(task: SubRunData): String {
    val state = when (task.state) {
        SubRunData.State.running -> "在做"
        SubRunData.State.reported -> "已交回"
        SubRunData.State.failed -> "失败"
        SubRunData.State.accepted -> "已收下"
    }
    return if (task.attempt > 1) "第 ${task.attempt} 次 · $state" else state
}

/**
 * The tasks a reply handed to sub-agents, folded into one line like its
 * steps; unfolded, each shows its objective and what it handed back. While
 * the reply streams, the line says how many are still at work.
 */
@Composable
fun SubRuns(tasks: List<SubRunData>, streaming: Boolean, modifier: Modifier = Modifier) {
    if (tasks.isEmpty()) return
    val colors = AcornTheme.colors
    var expanded by rememberSaveable { mutableStateOf(false) }
    val rotation by animateFloatAsState(if (expanded) 180f else 0f, label = "sub_runs_chevron")
    val working = streaming && runningSubRuns(tasks) > 0
    val failed = tasks.any { it.state == SubRunData.State.failed }
    Column(modifier = modifier) {
        Row(
            modifier = Modifier
                .clip(AcornShapes.small)
                .clickable { expanded = !expanded }
                .padding(vertical = 4.dp, horizontal = 2.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            if (working) {
                ThinkingOrb(OrbState.Working, size = 18.dp)
                Spacer(Modifier.width(8.dp))
            } else {
                Icon(Icons.Outlined.AccountTree, contentDescription = null, tint = if (failed) colors.warning else colors.inkFaint, modifier = Modifier.size(15.dp))
                Spacer(Modifier.width(6.dp))
            }
            Text(
                subRunsHeadline(tasks),
                style = AcornTheme.type.footnote,
                color = if (working) colors.inkSoft else colors.inkFaint,
                modifier = Modifier.shimmer(working),
            )
            Icon(AcornIcons.ChevronDown, contentDescription = if (expanded) "收起子任务" else "展开子任务", tint = colors.inkFaint, modifier = Modifier.size(16.dp).rotate(rotation))
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
                Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    tasks.forEach { SubRunRow(it, streaming) }
                }
            }
        }
    }
}

@Composable
private fun SubRunRow(task: SubRunData, streaming: Boolean) {
    val colors = AcornTheme.colors
    Row(verticalAlignment = Alignment.Top) {
        Box(modifier = Modifier.padding(top = 2.dp).size(15.dp), contentAlignment = Alignment.Center) {
            when (task.state) {
                SubRunData.State.failed -> Icon(Icons.Outlined.ErrorOutline, contentDescription = "失败", tint = colors.danger, modifier = Modifier.size(15.dp))
                SubRunData.State.running -> if (streaming) {
                    ThinkingOrb(OrbState.Working, size = 15.dp)
                } else {
                    Icon(Icons.Outlined.CheckCircle, contentDescription = null, tint = colors.line, modifier = Modifier.size(15.dp))
                }
                SubRunData.State.reported, SubRunData.State.accepted ->
                    Icon(Icons.Outlined.CheckCircle, contentDescription = null, tint = colors.inkFaint, modifier = Modifier.size(15.dp))
            }
        }
        Spacer(Modifier.width(8.dp))
        Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text("子任务 ${task.task} · ${subRunStateLabel(task)}", style = AcornTheme.type.caption, color = colors.inkFaint)
            Text(task.objective, style = AcornTheme.type.footnote, color = colors.ink, maxLines = 2, overflow = TextOverflow.Ellipsis)
            task.summary?.takeIf { it.isNotBlank() }?.let {
                Text(it, style = AcornTheme.type.caption, color = colors.inkSoft, maxLines = 6, overflow = TextOverflow.Ellipsis)
            }
            task.error?.takeIf { it.isNotBlank() }?.let {
                Text(it, style = AcornTheme.type.caption, color = colors.danger, maxLines = 3, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}
