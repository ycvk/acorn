package io.ycvk.acorn.feature.chat

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
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
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.PathMeasure
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.ycvk.acorn.api.models.PlanStep
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornIcons
import io.ycvk.acorn.core.ui.rememberReducedMotion
import io.ycvk.acorn.core.ui.shimmer

/** The header count, derived from the rows so it never disagrees with them. */
fun planCount(plan: List<PlanStep>): String = "${plan.count { it.status == PlanStep.Status.done }}/${plan.size}"

/** What the header adds when steps failed, or null. */
fun planFailures(plan: List<PlanStep>): String? =
    plan.count { it.status == PlanStep.Status.failed }.takeIf { it > 0 }?.let { "$it 步没做成" }

/** Where a step stands, in words; a step still running when the run ended reads as not done. */
fun planStepState(step: PlanStep, live: Boolean): String = when (step.status) {
    PlanStep.Status.done -> "已完成"
    PlanStep.Status.failed -> "没做成"
    PlanStep.Status.running -> if (live) "正在做" else "没做完"
    PlanStep.Status.pending -> "待做"
}

/**
 * The plan a reply works through, above its steps. While the run goes on the
 * whole checklist shows and only the steps under way move; once the reply is
 * in, the plan folds to its header and opens on a tap.
 */
@Composable
fun RunPlan(plan: List<PlanStep>, streaming: Boolean, modifier: Modifier = Modifier) {
    if (plan.isEmpty()) return
    val colors = AcornTheme.colors
    var expanded by rememberSaveable { mutableStateOf(streaming) }
    val rotation by animateFloatAsState(if (expanded) 180f else 0f, label = "plan_chevron")
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(AcornShapes.medium)
            .border(1.dp, colors.line, AcornShapes.medium),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clickable { expanded = !expanded }
                .padding(start = 12.dp, end = 8.dp, top = 8.dp, bottom = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text("计划", style = AcornTheme.type.footnote.copy(fontWeight = FontWeight.Medium), color = colors.ink)
            planFailures(plan)?.let {
                Text("  ·  $it", style = AcornTheme.type.caption, color = colors.warningInk)
            }
            Spacer(Modifier.weight(1f))
            Text(planCount(plan), style = AcornTheme.type.caption, color = colors.inkFaint)
            Spacer(Modifier.width(4.dp))
            Icon(AcornIcons.ChevronDown, contentDescription = if (expanded) "收起计划" else "展开计划", tint = colors.inkFaint, modifier = Modifier.size(16.dp).rotate(rotation))
        }
        AnimatedVisibility(visible = expanded, enter = expandVertically() + fadeIn(), exit = shrinkVertically() + fadeOut()) {
            Column(
                modifier = Modifier.padding(start = 12.dp, end = 12.dp, bottom = 10.dp),
                verticalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                plan.forEach { PlanRow(it, streaming) }
            }
        }
    }
}

@Composable
private fun PlanRow(step: PlanStep, live: Boolean) {
    val colors = AcornTheme.colors
    val done = step.status == PlanStep.Status.done
    val active = live && step.status == PlanStep.Status.running
    // Finished rows go quiet so the step under way is the one that asks for attention.
    val quiet by animateFloatAsState(if (done) 0.6f else 1f, tween(200), label = "plan_row_alpha")
    Row(
        modifier = Modifier
            .alpha(quiet)
            .semantics(mergeDescendants = true) { stateDescription = planStepState(step, live) },
        verticalAlignment = Alignment.Top,
    ) {
        PlanBox(step.status, active, Modifier.padding(top = 2.dp))
        Spacer(Modifier.width(8.dp))
        Text(
            step.label,
            style = AcornTheme.type.footnote,
            color = colors.ink,
            modifier = Modifier.weight(1f).shimmer(active),
        )
        step.note?.takeIf { it.isNotBlank() }?.let {
            Spacer(Modifier.width(8.dp))
            Text(it, style = AcornTheme.type.caption, color = colors.inkFaint, maxLines = 1, overflow = TextOverflow.Clip, modifier = Modifier.padding(top = 1.dp))
        }
    }
}

private val drawEasing = CubicBezierEasing(0.23f, 1f, 0.32f, 1f)

/**
 * The step's box. A check or cross draws itself as the step ends, which reads
 * as the act of ticking it; with reduced motion it is simply there.
 */
@Composable
private fun PlanBox(status: PlanStep.Status, active: Boolean, modifier: Modifier = Modifier) {
    val colors = AcornTheme.colors
    val reduced = rememberReducedMotion()
    val marked = status == PlanStep.Status.done || status == PlanStep.Status.failed
    val tone = when (status) {
        PlanStep.Status.done -> colors.success
        PlanStep.Status.failed -> colors.danger
        else -> if (active) colors.inkSoft else colors.lineStrong
    }
    val stroke by animateColorAsState(tone, tween(if (reduced) 0 else 200), label = "plan_box_stroke")
    val mark = remember { Animatable(0f) }
    LaunchedEffect(marked, reduced) {
        when {
            !marked -> mark.snapTo(0f)
            reduced -> mark.snapTo(1f)
            else -> mark.animateTo(1f, tween(200, easing = drawEasing))
        }
    }
    Canvas(modifier.size(14.dp)) {
        val unit = size.minDimension / 14f
        val box = Size(12f * unit, 12f * unit)
        if (marked) {
            drawRoundRect(tone.copy(alpha = 0.12f), topLeft = Offset(unit, unit), size = box, cornerRadius = CornerRadius(3.5f * unit))
        }
        drawRoundRect(stroke, topLeft = Offset(unit, unit), size = box, cornerRadius = CornerRadius(3.5f * unit), style = Stroke(width = 1.4f * unit))
        when (status) {
            PlanStep.Status.done -> drawMark(listOf(Offset(3.5f, 7.5f), Offset(6f, 10f), Offset(10.5f, 4.5f)), mark.value, tone, unit)
            PlanStep.Status.failed -> {
                drawMark(listOf(Offset(5f, 5f), Offset(9f, 9f)), mark.value, tone, unit)
                drawMark(listOf(Offset(9f, 5f), Offset(5f, 9f)), mark.value, tone, unit)
            }
            else -> Unit
        }
    }
}

/** Strokes the polyline through [points] (in 14-unit box coordinates) up to [progress] of its length. */
private fun DrawScope.drawMark(points: List<Offset>, progress: Float, color: Color, unit: Float) {
    if (progress <= 0f) return
    val path = Path().apply {
        moveTo(points[0].x * unit, points[0].y * unit)
        points.drop(1).forEach { lineTo(it.x * unit, it.y * unit) }
    }
    val measure = PathMeasure().apply { setPath(path, false) }
    val drawn = Path()
    measure.getSegment(0f, measure.length * progress, drawn, true)
    drawPath(drawn, color, style = Stroke(width = 1.8f * unit, cap = StrokeCap.Round, join = StrokeJoin.Round))
}
