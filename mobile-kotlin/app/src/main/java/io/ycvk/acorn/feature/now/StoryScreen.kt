package io.ycvk.acorn.feature.now

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Link
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.NowBriefingStory
import io.ycvk.acorn.api.models.NowResponse
import io.ycvk.acorn.api.models.NowStoryPiece
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornIcons
import io.ycvk.acorn.core.ui.EmptyState
import io.ycvk.acorn.core.ui.ErrorState
import io.ycvk.acorn.core.ui.Hairline
import io.ycvk.acorn.core.ui.InlineError
import io.ycvk.acorn.core.ui.Load
import io.ycvk.acorn.core.ui.Markdown
import io.ycvk.acorn.core.ui.ScreenPadding
import io.ycvk.acorn.core.ui.SkeletonRows
import io.ycvk.acorn.core.ui.Tag
import io.ycvk.acorn.core.ui.TopBar
import io.ycvk.acorn.core.ui.clockTime
import kotlinx.coroutines.delay
import java.net.URI
import java.time.ZoneId

/**
 * The reading page of a story on the briefing card: the piece Acorn wrote
 * about it or, without one, the sentence the briefing wrote; its source; what
 * the owner can ask of it; and the way into its conversation. The story is
 * read from the Today tab's load, so what the owner does here shows there.
 */
@Composable
fun StoryScreen(
    storyId: Long,
    viewModel: NowViewModel,
    onBack: () -> Unit,
    onOpenThread: (String) -> Unit,
) {
    val load by viewModel.load.collectAsStateWithLifecycle()
    val asking by viewModel.askingStory.collectAsStateWithLifecycle()
    val actionError by viewModel.actionError.collectAsStateWithLifecycle()
    val openThread by rememberUpdatedState(onOpenThread)
    LaunchedEffect(viewModel) { viewModel.openThread.collect { openThread(it) } }
    // Coming back from the story's conversation, the page shows what changed there.
    LaunchedEffect(Unit) { viewModel.refresh() }

    val story = (load as? Load.Loaded<NowResponse>)?.value?.briefing?.stories?.firstOrNull { it.storyId == storyId }
    RefreshWhilePiecesAreWritten(load, viewModel::refresh)

    val scroll = rememberScrollState()
    val colors = AcornTheme.colors
    Column(modifier = Modifier.fillMaxSize().background(colors.canvas)) {
        // The title moves into the bar once the heading scrolls away.
        TopBar(onBack = onBack, title = story?.title?.takeIf { scroll.value > 160 })
        actionError?.let { message ->
            InlineError(
                message,
                modifier = Modifier.padding(horizontal = ScreenPadding, vertical = 4.dp),
                action = "知道了",
                onAction = viewModel::dismissActionError,
            )
        }
        val current = load
        when {
            story != null -> Box(modifier = Modifier.weight(1f)) {
                StoryBody(
                    story = story,
                    busy = asking != null,
                    onFollow = { viewModel.followStory(story) },
                    onReact = { viewModel.react(story, it) },
                    modifier = Modifier.verticalScroll(scroll),
                )
                AskBar(
                    storyAskLabel(story),
                    onClick = { viewModel.openStory(story) },
                    modifier = Modifier.align(Alignment.BottomCenter).navigationBarsPadding(),
                    busy = asking == story.storyId,
                )
            }
            current is Load.Failed -> ErrorState(current.message, onRetry = viewModel::refresh)
            current is Load.Loading -> SkeletonRows(rows = 5)
            else -> EmptyState(
                icon = AcornIcons.Today,
                title = "这件事不在最新的简报里",
                body = "简报每天更新，之前的事件不再列在这里。",
            )
        }
    }
}

/** How often the page is loaded again while a piece is being written. */
private const val PIECE_POLL_MS = 4_000L

/** Loads the Today page again every few seconds for as long as its briefing has a piece that is being written. */
@Composable
internal fun RefreshWhilePiecesAreWritten(load: Load<NowResponse>, refresh: () -> Unit) {
    val writing = writingPiece((load as? Load.Loaded<NowResponse>)?.value)
    val current by rememberUpdatedState(refresh)
    LaunchedEffect(writing) {
        while (writing) {
            delay(PIECE_POLL_MS)
            current()
        }
    }
}

@Composable
private fun StoryBody(
    story: NowBriefingStory,
    busy: Boolean,
    onFollow: () -> Unit,
    onReact: (NowBriefingStory.Reaction) -> Unit,
    modifier: Modifier = Modifier,
) {
    val colors = AcornTheme.colors
    val piece = story.piece
    Column(modifier = modifier.padding(start = ScreenPadding, end = ScreenPadding, top = 4.dp, bottom = 132.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(story.section, style = AcornTheme.type.footnote, color = colors.accent)
            if (story.focus) {
                Spacer(Modifier.width(8.dp))
                Tag("今日关注", color = colors.warningInk, container = colors.warningSoft)
            }
        }
        Spacer(Modifier.height(6.dp))
        Text(story.title, style = AcornTheme.type.largeTitle.copy(fontSize = AcornTheme.type.title.fontSize * 1.2f), color = colors.ink)
        val meta = listOfNotNull(
            sourceLine(story.sources),
            piece?.finishedAt?.let { "${clockTime(it, ZoneId.systemDefault())} 写好" },
        ).joinToString("  ·  ")
        if (meta.isNotEmpty()) {
            Spacer(Modifier.height(8.dp))
            Text(meta, style = AcornTheme.type.footnote, color = colors.inkFaint)
        }
        Spacer(Modifier.height(14.dp))
        SourceLink(story.url)
        Spacer(Modifier.height(22.dp))
        val body = piece?.body?.takeIf { piece.status == NowStoryPiece.Status.ready }
        if (body != null) {
            SelectionContainer { Markdown(body) }
        } else {
            story.summary?.takeIf { it.isNotBlank() }?.let {
                SelectionContainer { Text(it, style = AcornTheme.type.reading, color = colors.ink) }
            }
            if (piece?.status == NowStoryPiece.Status.writing) {
                Spacer(Modifier.height(16.dp))
                Row(verticalAlignment = Alignment.CenterVertically) {
                    CircularProgressIndicator(color = colors.inkSoft, strokeWidth = 2.dp, modifier = Modifier.size(14.dp))
                    Spacer(Modifier.width(10.dp))
                    Text("Acorn 正在写这篇短文", style = AcornTheme.type.footnote, color = colors.inkSoft)
                }
            }
        }
        Spacer(Modifier.height(24.dp))
        Hairline()
        Spacer(Modifier.height(16.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            StoryAction(if (story.following) "跟进中" else "持续跟进", selected = story.following, enabled = !story.following && !busy, onClick = onFollow)
            StoryAction("多报这类", selected = story.reaction == NowBriefingStory.Reaction.more, onClick = { onReact(NowBriefingStory.Reaction.more) })
            StoryAction("少报这类", selected = story.reaction == NowBriefingStory.Reaction.less, onClick = { onReact(NowBriefingStory.Reaction.less) })
        }
    }
}

/** The source the briefing linked, named by its host; a tap opens it. */
@Composable
private fun SourceLink(url: String) {
    val colors = AcornTheme.colors
    val uriHandler = LocalUriHandler.current
    Row(
        modifier = Modifier
            .clip(AcornShapes.small)
            .background(colors.surfaceMuted)
            .clickable(onClickLabel = "打开原文") { uriHandler.openUri(url) }
            .heightIn(min = 40.dp)
            .padding(horizontal = 12.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(Icons.Outlined.Link, contentDescription = null, tint = colors.inkSoft, modifier = Modifier.size(16.dp))
        Spacer(Modifier.width(8.dp))
        Text(
            "原文 · ${sourceHost(url)}",
            style = AcornTheme.type.footnote,
            color = colors.inkSoft,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

/** A link's host without a leading "www.", or the link itself when it has none. */
fun sourceHost(url: String): String =
    runCatching { URI(url).host }.getOrNull()?.removePrefix("www.")?.takeIf { it.isNotEmpty() } ?: url

/** Something the owner asks of a story, on or off: a tinted pill when on. Following is on until the owner ends it in the conversation. */
@Composable
private fun StoryAction(text: String, selected: Boolean, onClick: () -> Unit, enabled: Boolean = true) {
    val colors = AcornTheme.colors
    val shape = AcornShapes.pill
    Box(
        modifier = Modifier
            .alpha(if (enabled || selected) 1f else 0.4f)
            .clip(shape)
            .then(if (selected) Modifier.background(colors.accentSoft) else Modifier.border(1.dp, colors.line, shape))
            .toggleable(value = selected, enabled = enabled, role = Role.Checkbox, onValueChange = { onClick() })
            .heightIn(min = 40.dp)
            .padding(horizontal = 14.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text,
            style = AcornTheme.type.footnote.copy(fontWeight = FontWeight.Medium),
            color = if (selected) colors.accentInk else colors.inkSoft,
            maxLines = 1,
        )
    }
}
