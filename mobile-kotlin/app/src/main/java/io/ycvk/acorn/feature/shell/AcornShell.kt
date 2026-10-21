package io.ycvk.acorn.feature.shell

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.provider.Settings
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.layout.windowInsetsTopHeight
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveableStateHolder
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.kyant.backdrop.backdrops.layerBackdrop
import com.kyant.backdrop.backdrops.rememberLayerBackdrop
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.core.net.LocalNetworkAccess
import io.ycvk.acorn.core.net.LocalNetworkDenied
import io.ycvk.acorn.core.theme.AcornMotion
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornDialog
import io.ycvk.acorn.core.ui.OnReturnToForeground
import io.ycvk.acorn.feature.browser.BrowserScreen
import io.ycvk.acorn.feature.chat.ChatScreen
import io.ycvk.acorn.feature.knowledge.KnowledgeScreen
import io.ycvk.acorn.feature.knowledge.KnowledgeViewModel
import io.ycvk.acorn.feature.knowledge.NoteScreen
import io.ycvk.acorn.feature.memory.MemoryScreen
import io.ycvk.acorn.feature.memory.MemoryViewModel
import io.ycvk.acorn.feature.now.NowScreen
import io.ycvk.acorn.feature.pairing.PairingScreen
import io.ycvk.acorn.feature.persona.PersonaRevisionScreen
import io.ycvk.acorn.feature.persona.PersonaScreen
import io.ycvk.acorn.feature.persona.PersonaViewModel
import io.ycvk.acorn.feature.settings.SettingsScreen
import io.ycvk.acorn.feature.skills.SkillRevisionScreen
import io.ycvk.acorn.feature.skills.SkillScreen
import io.ycvk.acorn.feature.skills.SkillsScreen
import io.ycvk.acorn.feature.skills.SkillsViewModel
import io.ycvk.acorn.feature.threads.ThreadsScreen
import kotlinx.coroutines.launch

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
            is AuthState.Disconnected -> PairingScreen(authController = shellViewModel.authController, localNetwork = shellViewModel.localNetwork)
            is AuthState.Connected -> ConnectedShell(shellViewModel)
        }
    }
}

@Composable
private fun ConnectedShell(shellViewModel: ShellViewModel) {
    val ownerGate by shellViewModel.ownerGate.collectAsStateWithLifecycle()
    val savingName by shellViewModel.savingName.collectAsStateWithLifecycle()
    val nameError by shellViewModel.nameError.collectAsStateWithLifecycle()

    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val notificationPermission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) {
        shellViewModel.registerPush()
    }
    val askNotifications = {
        val needsPrompt = Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        if (needsPrompt) {
            notificationPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
        } else {
            shellViewModel.registerPush()
        }
    }
    // A server on the LAN is asked for first; a refusal is explained until the owner allows it in settings.
    var localNetworkDenied by remember { mutableStateOf(false) }
    val localNetworkPermission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        localNetworkDenied = !granted
        askNotifications()
        shellViewModel.checkOwner()
    }
    LaunchedEffect(Unit) {
        if (shellViewModel.needsLocalNetwork()) {
            localNetworkPermission.launch(LocalNetworkAccess.PERMISSION)
        } else {
            askNotifications()
            shellViewModel.checkOwner()
        }
    }
    OnReturnToForeground {
        if (localNetworkDenied) scope.launch { localNetworkDenied = shellViewModel.needsLocalNetwork() }
        if (shellViewModel.ownerGate.value is OwnerGate.Failed) shellViewModel.checkOwner()
    }
    if (localNetworkDenied) {
        AcornDialog(
            title = "连不到局域网里的服务器",
            body = LocalNetworkDenied,
            confirm = "去设置",
            dismiss = "稍后",
            onConfirm = {
                context.startActivity(
                    Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.fromParts("package", context.packageName, null)),
                )
            },
            onDismiss = { localNetworkDenied = false },
        )
    }

    AnimatedContent(
        targetState = ownerGate,
        transitionSpec = { fadeIn(AcornMotion.enter(AcornMotion.MEDIUM)) togetherWith fadeOut(AcornMotion.exit(AcornMotion.SHORT)) },
        contentKey = { it::class },
        label = "owner_gate",
    ) { gate ->
        when (gate) {
            OwnerGate.Checking -> Box(modifier = Modifier.fillMaxSize()) {
                CircularProgressIndicator(
                    color = AcornTheme.colors.inkFaint,
                    strokeWidth = 2.dp,
                    modifier = Modifier.size(24.dp).align(Alignment.Center),
                )
            }
            OwnerGate.Unnamed -> OwnerNameScreen(saving = savingName, error = nameError, onSave = shellViewModel::saveOwnerName)
            is OwnerGate.Failed -> OwnerCheckFailedScreen(gate.message, onRetry = shellViewModel::checkOwner, onDisconnect = shellViewModel::disconnect)
            OwnerGate.Named -> Pages(shellViewModel)
        }
    }
}

@Composable
private fun Pages(shellViewModel: ShellViewModel) {
    val tab by shellViewModel.tab.collectAsStateWithLifecycle()
    val pages by shellViewModel.pages.collectAsStateWithLifecycle()
    val pendingCount by shellViewModel.pendingCount.collectAsStateWithLifecycle()
    val knowledgeViewModel: KnowledgeViewModel = hiltViewModel()
    val memoryViewModel: MemoryViewModel = hiltViewModel()
    val personaViewModel: PersonaViewModel = hiltViewModel()
    val skillsViewModel: SkillsViewModel = hiltViewModel()

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
                ) { current, barInset ->
                    when (current) {
                        Tab.Today -> NowScreen(
                            onThreadClick = { shellViewModel.open(Page.Chat(it)) },
                            onNewChat = newChat,
                            onSettings = { shellViewModel.open(Page.Settings) },
                            onBrowser = { shellViewModel.open(Page.Browser()) },
                            bottomInset = barInset,
                        )
                        Tab.Threads -> ThreadsScreen(
                            onThreadClick = { shellViewModel.open(Page.Chat(it)) },
                            onNewChat = newChat,
                            bottomInset = barInset,
                        )
                        Tab.Knowledge -> KnowledgeScreen(
                            viewModel = knowledgeViewModel,
                            memoryViewModel = memoryViewModel,
                            onNoteClick = { shellViewModel.open(Page.Note(it)) },
                            onMemoryClick = { shellViewModel.open(Page.Memory(it)) },
                            bottomInset = barInset,
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
                Page.Settings -> SettingsScreen(
                    onBack = shellViewModel::back,
                    onOpenPersona = { shellViewModel.open(Page.Persona) },
                    onOpenSkills = { shellViewModel.open(Page.Skills) },
                )
                Page.Persona -> PersonaScreen(
                    viewModel = personaViewModel,
                    onBack = shellViewModel::back,
                    onOpenRevision = { shellViewModel.open(Page.PersonaRevision(it)) },
                )
                is Page.PersonaRevision -> PersonaRevisionScreen(revision = page.revision, viewModel = personaViewModel, onBack = shellViewModel::back)
                Page.Skills -> SkillsScreen(
                    viewModel = skillsViewModel,
                    onBack = shellViewModel::back,
                    onOpenSkill = { shellViewModel.open(Page.Skill(it)) },
                )
                is Page.Skill -> SkillScreen(
                    name = page.name,
                    viewModel = skillsViewModel,
                    onBack = shellViewModel::back,
                    onOpenRevision = { shellViewModel.open(Page.SkillRevision(page.name, it)) },
                    onOpenThread = { shellViewModel.open(Page.Chat(it)) },
                )
                is Page.SkillRevision -> SkillRevisionScreen(
                    name = page.name,
                    revision = page.revision,
                    viewModel = skillsViewModel,
                    onBack = shellViewModel::back,
                    onOpenThread = { shellViewModel.open(Page.Chat(it)) },
                )
                is Page.Browser -> BrowserScreen(openUrl = page.openUrl, onBack = shellViewModel::back)
            }
        }
    }
}

/**
 * The tab pages under a floating bar of liquid glass. The pages are drawn into
 * the glass's backdrop over the canvas, and each page learns how much of its
 * bottom the bar covers so its last rows can scroll clear of it.
 */
@Composable
private fun Tabs(
    tab: Tab,
    pendingCount: Int,
    onSelect: (Tab) -> Unit,
    content: @Composable (Tab, Dp) -> Unit,
) {
    val saveable = rememberSaveableStateHolder()
    val canvas = AcornTheme.colors.canvas
    val backdrop = rememberLayerBackdrop {
        drawRect(canvas)
        drawContent()
    }
    val density = LocalDensity.current
    var barInset by remember { mutableStateOf(0.dp) }
    Box(modifier = Modifier.fillMaxSize()) {
        Box(modifier = Modifier.fillMaxSize().layerBackdrop(backdrop)) {
            AnimatedContent(
                targetState = tab,
                transitionSpec = { fadeIn(AcornMotion.enter(AcornMotion.SHORT)) togetherWith fadeOut(AcornMotion.exit(100)) },
                label = "tabs",
            ) { current ->
                saveable.SaveableStateProvider(current.name) { content(current, barInset) }
            }
        }
        // Lists scroll under the transparent status bar; keep its icons on the canvas.
        Spacer(
            Modifier
                .fillMaxWidth()
                .windowInsetsTopHeight(WindowInsets.statusBars)
                .background(canvas),
        )
        GlassTabBar(
            tab = tab,
            pendingCount = pendingCount,
            backdrop = backdrop,
            onSelect = onSelect,
            modifier = Modifier
                .align(Alignment.BottomCenter)
                .onSizeChanged { barInset = with(density) { it.height.toDp() } }
                .navigationBarsPadding()
                .padding(start = 20.dp, end = 20.dp, top = 8.dp, bottom = 10.dp),
        )
    }
}
