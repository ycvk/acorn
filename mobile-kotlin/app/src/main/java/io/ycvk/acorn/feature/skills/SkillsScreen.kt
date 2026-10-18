package io.ycvk.acorn.feature.skills

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.SkillSummary
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.EmptyState
import io.ycvk.acorn.core.ui.ErrorState
import io.ycvk.acorn.core.ui.Hairline
import io.ycvk.acorn.core.ui.ListRow
import io.ycvk.acorn.core.ui.ListSection
import io.ycvk.acorn.core.ui.ScreenPadding
import io.ycvk.acorn.core.ui.SkeletonRows
import io.ycvk.acorn.core.ui.Tag
import io.ycvk.acorn.core.ui.TopBar
import java.time.OffsetDateTime

/** Every skill Acorn has, in use first, with whether the next conversation lists it. */
@Composable
fun SkillsScreen(
    viewModel: SkillsViewModel,
    onBack: () -> Unit,
    onOpenSkill: (String) -> Unit,
) {
    val load by viewModel.list.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { viewModel.loadList() }

    Column(modifier = Modifier.fillMaxSize().background(AcornTheme.colors.canvas)) {
        TopBar(onBack = onBack, title = "技能")
        when (val state = load) {
            SkillsLoad.Loading -> SkeletonRows(rows = 5)
            is SkillsLoad.Failed -> ErrorState(state.message, onRetry = viewModel::loadList)
            is SkillsLoad.Loaded -> if (state.items.isEmpty()) {
                EmptyState(
                    icon = Icons.Outlined.AutoAwesome,
                    title = "还没有技能",
                    body = "Acorn 做成一件多步骤的事之后会把做法记成技能，也可以让它导入现成的技能包。",
                )
            } else {
                SkillList(state.items, onOpenSkill)
            }
        }
    }
}

@Composable
private fun SkillList(items: List<SkillSummary>, onOpenSkill: (String) -> Unit) {
    val now = OffsetDateTime.now()
    val (inUse, retired) = items.partition { !it.retired }
    LazyColumn(modifier = Modifier.fillMaxSize().navigationBarsPadding(), contentPadding = PaddingValues(bottom = 32.dp)) {
        item {
            Text(
                "Acorn 遇到对应的事时会加载这些技能。新建和修改交给 Acorn，在对话里告诉它就行；这里可以查看历史、回滚、停用，并给技能包设置密钥。",
                style = AcornTheme.type.footnote,
                color = AcornTheme.colors.inkSoft,
                modifier = Modifier.padding(start = ScreenPadding, end = ScreenPadding, top = 4.dp, bottom = 14.dp),
            )
        }
        if (inUse.isNotEmpty()) {
            item { ListSection("在用", count = inUse.size) }
            items(inUse, key = { "use:" + it.name }) { skill ->
                SkillRow(skill, now, onClick = { onOpenSkill(skill.name) })
                Hairline(inset = ScreenPadding)
            }
        }
        if (retired.isNotEmpty()) {
            item { ListSection("已停用", count = retired.size) }
            items(retired, key = { "retired:" + it.name }) { skill ->
                SkillRow(skill, now, onClick = { onOpenSkill(skill.name) })
                Hairline(inset = ScreenPadding)
            }
        }
    }
}

@Composable
private fun SkillRow(skill: SkillSummary, now: OffsetDateTime, onClick: () -> Unit) {
    val colors = AcornTheme.colors
    val standing = skillStanding(skill.retired, skill.eligible, skill.problem)
    ListRow(
        onClick = onClick,
        trailing = {
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                if (skill.hasManifest) Tag("包")
                when (standing) {
                    SkillStanding.NotListed -> Tag(standingLabel(standing), color = colors.warningInk, container = colors.warningSoft)
                    SkillStanding.Invalid -> Tag(standingLabel(standing), color = colors.danger, container = colors.dangerSoft)
                    else -> Unit
                }
            }
        },
    ) {
        Text(skill.name, style = AcornTheme.type.callout.copy(fontWeight = FontWeight.Medium), color = if (skill.retired) colors.inkSoft else colors.ink)
        skill.description?.takeIf { it.isNotBlank() }?.let {
            Text(it, style = AcornTheme.type.footnote, color = colors.inkSoft, maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
        if (standing == SkillStanding.NotListed) {
            skill.disabledReasons?.takeIf { it.isNotEmpty() }?.let { reasons ->
                Text(reasons.joinToString("；") { disabledReasonLabel(it) }, style = AcornTheme.type.caption, color = colors.warningInk)
            }
        }
        Text("${skillRevisionTitle(skill.revision)} · ${skillRevisionMeta(skill.author, skill.updatedAt, now)}", style = AcornTheme.type.caption, color = colors.inkFaint)
    }
}
