package io.ycvk.acorn.feature.shell

import android.Manifest
import android.content.pm.PackageManager
import android.os.Build
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.scaleIn
import androidx.compose.animation.scaleOut
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.layout.windowInsetsTopHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveableStateHolder
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.content.ContextCompat
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.core.theme.AcornMotion
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornIcons
import io.ycvk.acorn.core.ui.Hairline
import io.ycvk.acorn.core.ui.rememberHaptics
import io.ycvk.acorn.feature.browser.BrowserScreen
import io.ycvk.acorn.feature.chat.ChatScreen
import io.ycvk.acorn.feature.knowledge.KnowledgeScreen
import io.ycvk.acorn.feature.knowledge.KnowledgeViewModel
import io.ycvk.acorn.feature.memory.MemoryScreen
import io.ycvk.acorn.feature.memory.MemoryViewModel
import io.ycvk.acorn.feature.knowledge.NoteScreen
import io.ycvk.acorn.feature.now.NowScreen
import io.ycvk.acorn.feature.pairing.PairingScreen
import io.ycvk.acorn.feature.persona.PersonaRevisionScreen
import io.ycvk.acorn.feature.persona.PersonaScreen
import io.ycvk.acorn.feature.persona.PersonaViewModel
import io.ycvk.acorn.feature.settings.SettingsScreen
import io.ycvk.acorn.feature.threads.ThreadsScreen

@Composable
fun AcornShell(
    shellViewModel: ShellViewModel = hiltViewModel(),
) {
    val authState by shellViewModel.authController.authState.collectAsStateWithLifecycle()
    Box(modifier = Modifier.fillMaxSize().background(AcornTheme.colors.canvas)) {
        when (authState) {
            is AuthState.Loading -> CircularProgressIndicator(
                color = AcornTheme.colors.inkFaint,
                strokeWidth = 2.dp,
                modifier = Modifier.size(24.dp).align(Alignment.Center),
            )
            is AuthState.Disconnected -> PairingScreen(authController = shellViewModel.authController)
            is AuthState.Connected -> ConnectedShell(shellViewModel)
        }
    }
}

@Composable
private fun ConnectedShell(shellViewModel: ShellViewModel) {
    val tab by shellViewModel.tab.collectAsStateWithLifecycle()
    val pages by shellViewModel.pages.collectAsStateWithLifecycle()
    val pendingCount by shellViewModel.pendingCount.collectAsStateWithLifecycle()
    val knowledgeViewModel: KnowledgeViewModel = hiltViewModel()
    val memoryViewModel: MemoryViewModel = hiltViewModel()
    val personaViewModel: PersonaViewModel = hiltViewModel()

    val context = LocalContext.current
    val notificationPermission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) {
        shellViewModel.registerPush()
    }
    LaunchedEffect(Unit) {
        val needsPrompt = Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        if (needsPrompt) {
            notificationPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
        } else {
            shellViewModel.registerPush()
        }
    }

    BackHandler(enabled = pages.isNotEmpty()) { shellViewModel.back() }

    // Pages and tabs keep their scroll positions while covered.
    val saveable = rememberSaveableStateHolder()
    val top = pages.lastOrNull()
    val newChat = { shellViewModel.open(Page.Chat(null)) }
    AnimatedContent(
        targetState = top,
        transitionSpec = {
            val depth = { page: Page? -> if (page == null) 0 else pages.indexOf(page).let { if (it < 0) Int.MAX_VALUE else it + 1 } }
            val forward = depth(targetState) >= depth(initialState)
            val enter = slideInHorizontally(AcornMotion.enter(AcornMotion.LONG)) { if (forward) it / 5 else -it / 5 } +
                fadeIn(AcornMotion.enter(AcornMotion.MEDIUM))
            val exit = slideOutHorizontally(AcornMotion.enter(AcornMotion.LONG)) { if (forward) -it / 10 else it / 5 } +
                fadeOut(AcornMotion.exit(AcornMotion.SHORT))
            enter togetherWith exit
        },
        contentKey = { it?.key ?: "tabs" },
        label = "pages",
    ) { page ->
        saveable.SaveableStateProvider(page?.key ?: "tabs") {
            when (page) {
                null -> Tabs(
                    tab = tab,
                    pendingCount = pendingCount,
                    onSelect = shellViewModel::selectTab,
                ) { current ->
                    when (current) {
                        Tab.Today -> NowScreen(
                            onThreadClick = { shellViewModel.open(Page.Chat(it)) },
                            onNewChat = newChat,
                            onSettings = { shellViewModel.open(Page.Settings) },
                            onBrowser = { shellViewModel.open(Page.Browser()) },
                        )
                        Tab.Threads -> ThreadsScreen(
                            onThreadClick = { shellViewModel.open(Page.Chat(it)) },
                            onNewChat = newChat,
                        )
                        Tab.Knowledge -> KnowledgeScreen(
                            viewModel = knowledgeViewModel,
                            memoryViewModel = memoryViewModel,
                            onNoteClick = { shellViewModel.open(Page.Note(it)) },
                            onMemoryClick = { shellViewModel.open(Page.Memory(it)) },
                        )
                    }
                }
                is Page.Chat -> ChatScreen(threadId = page.threadId, openActionId = page.actionId, onBack = shellViewModel::back)
                is Page.Note -> NoteScreen(path = page.path, viewModel = knowledgeViewModel, onBack = shellViewModel::back)
                is Page.Memory -> MemoryScreen(
                    id = page.id,
                    viewModel = memoryViewModel,
                    onBack = shellViewModel::back,
                    onOpenMemory = { shellViewModel.open(Page.Memory(it)) },
                    onReplaceMemory = { shellViewModel.replace(Page.Memory(it)) },
                    onOpenThread = { shellViewModel.open(Page.Chat(it)) },
                    onOpenNote = { shellViewModel.open(Page.Note(it)) },
                )
                Page.Settings -> SettingsScreen(onBack = shellViewModel::back, onOpenPersona = { shellViewModel.open(Page.Persona) })
                Page.Persona -> PersonaScreen(
                    viewModel = personaViewModel,
                    onBack = shellViewModel::back,
                    onOpenRevision = { shellViewModel.open(Page.PersonaRevision(it)) },
                )
                is Page.PersonaRevision -> PersonaRevisionScreen(revision = page.revision, viewModel = personaViewModel, onBack = shellViewModel::back)
                is Page.Browser -> BrowserScreen(openUrl = page.openUrl, onBack = shellViewModel::back)
            }
        }
    }
}

@Composable
private fun Tabs(
    tab: Tab,
    pendingCount: Int,
    onSelect: (Tab) -> Unit,
    content: @Composable (Tab) -> Unit,
) {
    val saveable = rememberSaveableStateHolder()
    Column(modifier = Modifier.fillMaxSize()) {
        Box(modifier = Modifier.weight(1f)) {
            AnimatedContent(
                targetState = tab,
                transitionSpec = {
                    (fadeIn(AcornMotion.enter(AcornMotion.MEDIUM)) + scaleIn(AcornMotion.enter(AcornMotion.MEDIUM), initialScale = 0.985f)) togetherWith
                        fadeOut(AcornMotion.exit())
                },
                label = "tabs",
            ) { current ->
                saveable.SaveableStateProvider(current.name) { content(current) }
            }
            // Lists scroll under the transparent status bar; keep its icons on the canvas.
            Spacer(
                Modifier
                    .fillMaxWidth()
                    .windowInsetsTopHeight(WindowInsets.statusBars)
                    .background(AcornTheme.colors.canvas),
            )
        }
        TabBar(tab = tab, pendingCount = pendingCount, onSelect = onSelect)
    }
}

@Composable
private fun TabBar(tab: Tab, pendingCount: Int, onSelect: (Tab) -> Unit) {
    val haptics = rememberHaptics()
    val colors = AcornTheme.colors
    Column(modifier = Modifier.fillMaxWidth().background(colors.canvas)) {
        Hairline()
        BoxWithConstraints(
            modifier = Modifier
                .fillMaxWidth()
                .navigationBarsPadding()
                .height(62.dp),
        ) {
            // One indicator glides between tabs instead of each tab fading its own.
            val slot = maxWidth / Tab.entries.size
            val indicatorWidth = 58.dp
            val offset by animateDpAsState(slot * tab.ordinal + (slot - indicatorWidth) / 2, AcornMotion.glide(), label = "tab_indicator")
            Box(
                Modifier
                    .offset(x = offset, y = 8.dp)
                    .size(width = indicatorWidth, height = 30.dp)
                    .background(colors.accentSoft, AcornShapes.pill),
            )
            Row(modifier = Modifier.fillMaxSize()) {
                Tab.entries.forEach { item ->
                    val (label, icon) = when (item) {
                        Tab.Today -> "今天" to AcornIcons.Today
                        Tab.Threads -> "对话" to AcornIcons.Threads
                        Tab.Knowledge -> "知识" to AcornIcons.Knowledge
                    }
                    TabItem(
                        label = label,
                        icon = icon,
                        selected = item == tab,
                        badge = if (item == Tab.Today) pendingCount else 0,
                        onClick = {
                            if (item != tab) haptics.tick()
                            onSelect(item)
                        },
                        modifier = Modifier.weight(1f),
                    )
                }
            }
        }
    }
}

@Composable
private fun TabItem(
    label: String,
    icon: ImageVector,
    selected: Boolean,
    badge: Int,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val colors = AcornTheme.colors
    val tint by animateColorAsState(if (selected) colors.accentInk else colors.inkFaint, AcornMotion.standard(), label = "tab_tint")
    // The icon dips and springs back when its tab becomes selected.
    val bounce = remember { Animatable(1f) }
    LaunchedEffect(selected) {
        if (selected) {
            bounce.snapTo(0.82f)
            bounce.animateTo(1f, AcornMotion.pop())
        }
    }
    Column(
        modifier = modifier
            .fillMaxSize()
            .clickable(
                interactionSource = remember { MutableInteractionSource() },
                indication = null,
                role = Role.Tab,
                onClick = onClick,
            )
            .semantics { this.selected = selected },
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Box(modifier = Modifier.padding(top = 11.dp).height(24.dp)) {
            Icon(
                icon,
                contentDescription = null,
                tint = tint,
                modifier = Modifier
                    .size(24.dp)
                    .graphicsLayer {
                        scaleX = bounce.value
                        scaleY = bounce.value
                    },
            )
            androidx.compose.animation.AnimatedVisibility(
                visible = badge > 0,
                enter = scaleIn(AcornMotion.pop()) + fadeIn(),
                exit = scaleOut(AcornMotion.exit()) + fadeOut(),
                modifier = Modifier
                    .align(Alignment.TopEnd)
                    .offset(x = 11.dp, y = (-5).dp),
            ) {
                Box(
                    modifier = Modifier
                        .size(18.dp)
                        .background(colors.canvas, CircleShape)
                        .padding(1.5.dp)
                        .background(colors.warning, CircleShape),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        if (badge > 9) "9+" else badge.toString(),
                        style = AcornTheme.type.caption.copy(fontSize = 10.sp, fontWeight = FontWeight.SemiBold),
                        color = colors.onAccent,
                    )
                }
            }
        }
        Spacer(Modifier.height(9.dp))
        Text(
            label,
            style = AcornTheme.type.caption.copy(fontSize = 11.sp, fontWeight = FontWeight.Medium),
            color = tint,
        )
    }
}
