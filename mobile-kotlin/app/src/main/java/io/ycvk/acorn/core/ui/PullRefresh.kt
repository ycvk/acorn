package io.ycvk.acorn.core.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.material3.pulltorefresh.PullToRefreshState
import androidx.compose.material3.pulltorefresh.rememberPullToRefreshState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.unit.dp
import io.ycvk.acorn.core.theme.AcornTheme
import kotlinx.coroutines.flow.drop

/**
 * A page that loads again when pulled down. Acorn's orb comes down with the
 * finger and grows until the pull is far enough to let go, which a tick tells
 * the hand; it turns while the page loads.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PullRefresh(
    isRefreshing: Boolean,
    onRefresh: () -> Unit,
    modifier: Modifier = Modifier,
    content: @Composable BoxScope.() -> Unit,
) {
    val state = rememberPullToRefreshState()
    val haptics = rememberHaptics()
    val refreshing by rememberUpdatedState(isRefreshing)
    LaunchedEffect(state) {
        // The orb also comes down to the threshold when a load starts without a pull; only a pull ticks.
        snapshotFlow { state.distanceFraction >= 1f }.drop(1).collect { far -> if (far && !refreshing) haptics.tick() }
    }
    PullToRefreshBox(
        isRefreshing = isRefreshing,
        onRefresh = onRefresh,
        modifier = modifier,
        state = state,
        indicator = { RefreshOrb(state, isRefreshing, Modifier.align(Alignment.TopCenter)) },
        content = content,
    )
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun RefreshOrb(state: PullToRefreshState, refreshing: Boolean, modifier: Modifier = Modifier) {
    Box(
        modifier = modifier
            .graphicsLayer {
                val pulled = state.distanceFraction.coerceIn(0f, 1.2f)
                val grown = pulled.coerceAtMost(1f)
                translationY = pulled * OrbDrop.toPx() - size.height
                scaleX = 0.6f + 0.4f * grown
                scaleY = 0.6f + 0.4f * grown
                alpha = (pulled * 2f).coerceAtMost(1f)
            }
            .raised(CircleShape, elevation = 6.dp)
            .size(40.dp)
            .clip(CircleShape)
            .background(AcornTheme.colors.surface),
        contentAlignment = Alignment.Center,
    ) {
        ThinkingOrb(if (refreshing) OrbState.Thinking else OrbState.Listening, size = 24.dp, animated = refreshing)
    }
}

/** How far below the top of the page the orb rests at the threshold. */
private val OrbDrop = 76.dp
