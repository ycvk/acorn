package io.ycvk.acorn.feature.shell

import android.Manifest
import android.content.pm.PackageManager
import android.os.Build
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.Crossfade
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
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
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Chat
import androidx.compose.material.icons.automirrored.filled.Chat
import androidx.compose.material.icons.filled.AutoStories
import androidx.compose.material.icons.filled.WbSunny
import androidx.compose.material.icons.outlined.AutoStories
import androidx.compose.material.icons.outlined.WbSunny
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
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.core.theme.AcornMotion
import io.ycvk.acorn.core.theme.AcornTheme
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
            val enter = slideInHorizontally(AcornMotion.standard(280)) { if (forward) it / 4 else -it / 4 } +
                fadeIn(AcornMotion.standard(220))
            val exit = slideOutHorizontally(AcornMotion.standard(280)) { if (forward) -it / 8 else it / 4 } +
                fadeOut(AcornMotion.standard(180))
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
                            onNoteClick = { shellViewModel.open(Page.Note(it)) },
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
            Crossfade(
                targetState = tab,
                animationSpec = AcornMotion.standard(AcornMotion.SHORT),
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
    Column(modifier = Modifier.fillMaxWidth().background(AcornTheme.colors.canvas)) {
        Hairline()
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .navigationBarsPadding()
                .height(58.dp),
            horizontalArrangement = Arrangement.SpaceEvenly,
        ) {
            Tab.entries.forEach { item ->
                val (label, icons) = when (item) {
                    Tab.Today -> "今天" to (Icons.Filled.WbSunny to Icons.Outlined.WbSunny)
                    Tab.Threads -> "对话" to (Icons.AutoMirrored.Filled.Chat to Icons.AutoMirrored.Outlined.Chat)
                    Tab.Knowledge -> "知识" to (Icons.Filled.AutoStories to Icons.Outlined.AutoStories)
                }
                TabItem(
                    label = label,
                    icon = if (item == tab) icons.first else icons.second,
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
    val tint = if (selected) colors.ink else colors.inkFaint
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
        verticalArrangement = Arrangement.Center,
    ) {
        Box {
            Icon(icon, contentDescription = null, tint = tint, modifier = Modifier.size(23.dp))
            if (badge > 0) {
                Box(
                    modifier = Modifier
                        .align(Alignment.TopEnd)
                        .offset(x = 9.dp, y = (-4).dp)
                        .size(17.dp)
                        .background(colors.warning, CircleShape),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        if (badge > 9) "9+" else badge.toString(),
                        style = AcornTheme.type.caption.copy(fontWeight = FontWeight.SemiBold),
                        color = colors.onAccent,
                    )
                }
            }
        }
        Spacer(Modifier.height(3.dp))
        Text(
            label,
            style = AcornTheme.type.caption.copy(fontWeight = if (selected) FontWeight.Medium else FontWeight.Normal),
            color = tint,
        )
    }
}
