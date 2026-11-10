package io.ycvk.acorn.feature.shell

import android.Manifest
import android.content.Intent
import android.net.Uri
import android.provider.Settings
import androidx.activity.BackEventCompat
import androidx.activity.compose.PredictiveBackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.AnimatedVisibilityScope
import androidx.compose.animation.EnterExitState
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.SeekableTransitionState
import androidx.compose.animation.core.Transition
import androidx.compose.animation.core.animate
import androidx.compose.animation.core.animateDp
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.rememberTransition
import androidx.compose.animation.core.tween
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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveableStateHolder
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.TransformOrigin
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.kyant.backdrop.backdrops.layerBackdrop
import com.kyant.backdrop.backdrops.rememberLayerBackdrop
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.core.net.LocalNetworkAccess
import io.ycvk.acorn.core.net.LocalNetworkDenied
import io.ycvk.acorn.core.push.canPostNotifications
import io.ycvk.acorn.core.theme.AcornMotion
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.theme.CardRadius
import io.ycvk.acorn.core.ui.AcornDialog
import io.ycvk.acorn.core.ui.OnReturnToForeground
import io.ycvk.acorn.core.ui.animateOnceDrawn
import io.ycvk.acorn.core.ui.rememberReducedMotion
import io.ycvk.acorn.feature.browser.BrowserScreen
import io.ycvk.acorn.feature.chat.ChatScreen
import io.ycvk.acorn.feature.knowledge.KnowledgeScreen
import io.ycvk.acorn.feature.knowledge.KnowledgeViewModel
import io.ycvk.acorn.feature.knowledge.NoteScreen
import io.ycvk.acorn.feature.memory.MemoryScreen
import io.ycvk.acorn.feature.memory.MemoryViewModel
import io.ycvk.acorn.feature.now.NowScreen
import io.ycvk.acorn.feature.now.NowViewModel
import io.ycvk.acorn.feature.now.StoryScreen
import io.ycvk.acorn.feature.pairing.PairingScreen
import io.ycvk.acorn.feature.persona.PersonaRevisionScreen
import io.ycvk.acorn.feature.persona.PersonaScreen
import io.ycvk.acorn.feature.persona.PersonaViewModel
import io.ycvk.acorn.feature.settings.OwnerSettingsScreen
import io.ycvk.acorn.feature.settings.SettingsScreen
import io.ycvk.acorn.feature.skills.SkillRevisionScreen
import io.ycvk.acorn.feature.skills.SkillScreen
import io.ycvk.acorn.feature.skills.SkillsScreen
import io.ycvk.acorn.feature.skills.SkillsViewModel
import io.ycvk.acorn.feature.threads.ThreadsScreen
import kotlinx.coroutines.launch
import kotlin.coroutines.cancellation.CancellationException

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
        if (!canPostNotifications(context)) {
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
        shellViewModel.refreshPending()
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
    val nowViewModel: NowViewModel = hiltViewModel()
    val memoryViewModel: MemoryViewModel = hiltViewModel()
    val personaViewModel: PersonaViewModel = hiltViewModel()
    val skillsViewModel: SkillsViewModel = hiltViewModel()

    // Pages and tabs keep their scroll positions while covered.
    val saveable = rememberSaveableStateHolder()
    val top = pages.lastOrNull()
    val newChat = { shellViewModel.open(Page.Chat(null)) }
    val canvas = AcornTheme.colors.canvas
    val origins = remember { StoryOrigins() }
    val storyBodyTop = remember { mutableFloatStateOf(0f) }

    // A back gesture holds the stack part way into the pop before it is let go, so the stack's transition can be sought.
    val stack = remember { SeekableTransitionState(top) }
    val transition = rememberTransition(stack, label = "pages")
    val reducedMotion = rememberReducedMotion()
    var held by remember { mutableStateOf<Float?>(null) }
    var edge by remember { mutableStateOf(BackEdge.Left) }
    PredictiveBackHandler(enabled = pages.isNotEmpty()) { events ->
        try {
            events.collect { event ->
                if (!reducedMotion) {
                    edge = if (event.swipeEdge == BackEventCompat.EDGE_RIGHT) BackEdge.Right else BackEdge.Left
                    held = event.progress
                }
            }
            held = null
            edge = BackEdge.Left
            shellViewModel.back()
        } catch (e: CancellationException) {
            held = null
            edge = BackEdge.Left
            throw e
        }
    }
    val progress = held
    if (progress != null) {
        LaunchedEffect(progress) { stack.seekTo(backFraction(progress), pages.dropLast(1).lastOrNull()) }
    } else {
        LaunchedEffect(top) {
            if (stack.currentState != top) {
                // After a back gesture the pop is already under way and plays on from where it was held.
                stack.animateOnceDrawn(top)
            } else if (stack.targetState != top) {
                // The gesture was let go without going back: the pop plays backwards, then the page rests.
                animate(stack.fraction, 0f, animationSpec = AcornMotion.standard(AcornMotion.MEDIUM)) { value, _ ->
                    launch { if (value > 0f) stack.seekTo(value) else stack.snapTo(top) }
                }
            }
        }
    }

    transition.AnimatedContent(
        transitionSpec = { pageTransform(pageMove(initialState, targetState, pages), edge, depthOf(targetState, pages).toFloat()) },
        contentKey = { it?.key ?: "tabs" },
    ) { page ->
        saveable.SaveableStateProvider(page?.key ?: "tabs") {
            val frame = if (page is Page.Story) storyContainer(page, transition, origins) { storyBodyTop.floatValue } else Modifier
            Box(modifier = Modifier.fillMaxSize().then(frame).then(steppedBack()).background(canvas)) {
                when (page) {
                    null -> Tabs(
                        tab = tab,
                        pendingCount = pendingCount,
                        onSelect = shellViewModel::selectTab,
                    ) { current, barInset ->
                        when (current) {
                            // The story cards tell where they are, for the reading pages that grow out of them.
                            Tab.Today -> CompositionLocalProvider(LocalStoryOrigins provides origins) {
                                NowScreen(
                                    viewModel = nowViewModel,
                                    onThreadClick = { shellViewModel.open(Page.Chat(it)) },
                                    onStoryClick = { shellViewModel.open(Page.Story(it)) },
                                    onNewChat = newChat,
                                    onSettings = { shellViewModel.open(Page.Settings) },
                                    onBrowser = { shellViewModel.open(Page.Browser()) },
                                    bottomInset = barInset,
                                )
                            }
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
                                onOpenThread = { shellViewModel.open(Page.Chat(it)) },
                                bottomInset = barInset,
                            )
                        }
                    }
                    is Page.Chat -> ChatScreen(threadId = page.threadId, openActionId = page.actionId, onBack = shellViewModel::back)
                    is Page.Story -> StoryScreen(
                        storyId = page.storyId,
                        viewModel = nowViewModel,
                        onBack = shellViewModel::back,
                        onOpenThread = { shellViewModel.open(Page.Chat(it)) },
                        onBodyTop = { storyBodyTop.floatValue = it },
                    )
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
                        onOpenOwnerSettings = { shellViewModel.open(Page.OwnerSettings) },
                    )
                    Page.OwnerSettings -> OwnerSettingsScreen(onBack = shellViewModel::back)
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
}

/** Rounds a page's corners while it is stepped back from the screen: leaving, or held by a back gesture. */
@Composable
private fun AnimatedVisibilityScope.steppedBack(): Modifier {
    val corner by transition.animateDp(transitionSpec = { tween(POP_MS / 3) }, label = "page_corner") {
        if (it == EnterExitState.PostExit) CardRadius else 0.dp
    }
    return if (corner > 0.dp) Modifier.clip(RoundedCornerShape(corner)) else Modifier
}

/**
 * Draws a story's reading page inside a frame that grows out of the story's
 * card when the page opens from Today and shrinks back into it on return.
 * [bodyTop] is where the page's own content starts under its bar.
 */
@Composable
private fun AnimatedVisibilityScope.storyContainer(
    page: Page.Story,
    stack: Transition<Page?>,
    origins: StoryOrigins,
    bodyTop: () -> Float,
): Modifier {
    val from = stack.currentState
    val to = stack.targetState
    val withCard = (from == null && to == page) || (from == page && to == null)
    val grown by transition.animateFloat(
        transitionSpec = { tween(GROW_MS, easing = FastOutSlowInEasing) },
        label = "story_container",
    ) { if (it == EnterExitState.Visible || !withCard) 1f else 0f }
    val cardCorner = with(LocalDensity.current) { CardRadius.toPx() }
    return Modifier.graphicsLayer {
        val origin = origins[page.storyId]
        if (grown >= 1f || origin == null) return@graphicsLayer
        val frame = containerFrame(origin, size.width, size.height, bodyTop(), cardCorner, grown)
        transformOrigin = TransformOrigin(0f, 0f)
        scaleX = frame.scale
        scaleY = frame.scale
        translationX = frame.x
        translationY = frame.y
        shape = ContainerWindow(frame)
        clip = true
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
            val shift = with(density) { TabShift.roundToPx() }
            val shown = remember { SeekableTransitionState(tab) }
            LaunchedEffect(tab) { if (shown.currentState != tab) shown.animateOnceDrawn(tab) }
            rememberTransition(shown, label = "tabs").AnimatedContent(
                transitionSpec = {
                    // The page comes from the side its tab is on, once the one before it has cleared.
                    val side = if (targetState.ordinal > initialState.ordinal) 1 else -1
                    (slideInHorizontally(AcornMotion.travel(TAB_MS)) { side * shift } + fadeIn(tween(TAB_MS / 2, delayMillis = TAB_CLEAR_MS, easing = LinearEasing))) togetherWith
                        (slideOutHorizontally(tween(TAB_CLEAR_MS, easing = LinearEasing)) { -side * shift / 3 } + fadeOut(tween(TAB_CLEAR_MS, easing = LinearEasing)))
                },
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

/** How far a tab's page travels as it comes in. */
private val TabShift = 36.dp
private const val TAB_MS = 340
private const val TAB_CLEAR_MS = 80
