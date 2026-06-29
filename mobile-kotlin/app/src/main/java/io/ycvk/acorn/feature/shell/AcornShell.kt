package io.ycvk.acorn.feature.shell

import androidx.activity.compose.BackHandler
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Inbox
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalDrawerSheet
import androidx.compose.material3.ModalNavigationDrawer
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationBarItemDefaults
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.rememberDrawerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.api.models.Thread
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.core.theme.AetherBackground
import io.ycvk.acorn.core.theme.AetherOnPrimary
import io.ycvk.acorn.core.theme.AetherOnSurface
import io.ycvk.acorn.core.theme.AetherOnSurfaceVariant
import io.ycvk.acorn.core.theme.AetherPrimary
import io.ycvk.acorn.core.theme.AetherPrimaryContainer
import io.ycvk.acorn.core.theme.AetherSecondary
import io.ycvk.acorn.core.theme.AetherSurface
import io.ycvk.acorn.core.theme.AetherSurfaceHigh
import io.ycvk.acorn.core.theme.AetherTertiary
import io.ycvk.acorn.core.theme.accentGlow
import io.ycvk.acorn.core.theme.gradientBackground
import io.ycvk.acorn.feature.approvals.ApprovalsScreen
import io.ycvk.acorn.feature.chat.ChatScreen
import io.ycvk.acorn.feature.pairing.PairingScreen
import io.ycvk.acorn.feature.settings.SettingsScreen
import io.ycvk.acorn.feature.threads.ThreadsScreen
import io.ycvk.acorn.feature.threads.ThreadsViewModel
import kotlinx.coroutines.launch

@Composable
fun AcornShell(
    shellViewModel: ShellViewModel = hiltViewModel(),
) {
    val authController = shellViewModel.authController
    val authState by authController.authState.collectAsStateWithLifecycle()

    LaunchedEffect(Unit) { authController.loadStoredConnection() }

    when (val state = authState) {
        is AuthState.Loading -> {
            Box(
                modifier = Modifier
                    .fillMaxSize()
                    .gradientBackground()
                    .accentGlow(),
                contentAlignment = Alignment.Center,
            ) {
                CircularProgressIndicator(
                    color = AetherPrimary,
                    strokeWidth = 2.dp,
                    modifier = Modifier.size(32.dp),
                )
            }
        }
        is AuthState.Disconnected -> {
            PairingScreen(onPaired = {}, authController = authController)
        }
        is AuthState.Connected -> ConnectedShell(shellViewModel = shellViewModel)
    }
}

@OptIn(androidx.compose.foundation.ExperimentalFoundationApi::class)
@Composable
private fun ConnectedShell(
    shellViewModel: ShellViewModel,
) {
    val selectedTab by shellViewModel.selectedTab.collectAsStateWithLifecycle()
    val openThreadId by shellViewModel.openThreadId.collectAsStateWithLifecycle()
    val pendingCount by shellViewModel.pendingCount.collectAsStateWithLifecycle()
    val showApprovals by shellViewModel.showApprovals.collectAsStateWithLifecycle()

    val currentThreadId = openThreadId
    val drawerState = rememberDrawerState(initialValue = androidx.compose.material3.DrawerValue.Closed)
    val scope = rememberCoroutineScope()
    val threadsViewModel: ThreadsViewModel = hiltViewModel()
    val openDrawer: () -> Unit = { scope.launch { drawerState.open() } }

    // Page key drives AnimatedContent transitions — includes threadId so A→B triggers animation
    val pageKey: String = when {
        currentThreadId != null -> "chat:$currentThreadId"
        showApprovals -> "approvals"
        else -> "tabs"
    }

    // Single BackHandler for all non-root pages
    BackHandler(enabled = true) {
        when {
            drawerState.isOpen -> scope.launch { drawerState.close() }
            currentThreadId != null -> shellViewModel.closeThread()
            showApprovals -> shellViewModel.hideApprovalsList()
        }
    }

    val easing = CubicBezierEasing(0.22f, 0.84f, 0.18f, 1f)
    val duration = 320
    ModalNavigationDrawer(
        drawerState = drawerState,
        drawerContent = {
            ThreadDrawer(
                threadsViewModel = threadsViewModel,
                onThreadClick = { id ->
                    scope.launch { drawerState.close() }
                    shellViewModel.openThread(id)
                },
                onSettings = {
                    scope.launch { drawerState.close() }
                    shellViewModel.selectTab(ShellViewModel.TAB_SETTINGS)
                },
            )
        },
    ) {
        AnimatedContent(
            targetState = pageKey,
            transitionSpec = {
                val isThreadSwitch = initialState.startsWith("chat:") && targetState.startsWith("chat:") && initialState != targetState
                val forward = initialState == "tabs" && targetState.startsWith("chat:") || isThreadSwitch
                val backward = initialState.startsWith("chat:") && targetState == "tabs"
                when {
                    isThreadSwitch -> (fadeIn(tween(duration, easing = easing)) togetherWith
                        fadeOut(tween(duration, easing = easing)))
                    forward -> (slideInHorizontally(tween(duration, easing = easing)) { it / 3 } +
                        fadeIn(tween(duration))) togetherWith
                        (slideOutHorizontally(tween(duration, easing = easing)) { -it / 3 } +
                            fadeOut(tween(duration)))
                    backward -> (slideInHorizontally(tween(duration, easing = easing)) { -it / 3 } +
                        fadeIn(tween(duration))) togetherWith
                        (slideOutHorizontally(tween(duration, easing = easing)) { it / 3 } +
                            fadeOut(tween(duration)))
                    else -> fadeIn(tween(duration)) togetherWith fadeOut(tween(duration))
                }
            },
            contentKey = { it },
        ) { page ->
            when {
                page.startsWith("chat:") -> {
                    val tid = page.removePrefix("chat:")
                    ChatScreen(
                        threadId = tid,
                        onBack = { shellViewModel.closeThread() },
                        onOpenDrawer = openDrawer,
                    )
                }
                page == "approvals" -> {
                    ApprovalsScreen(
                        modifier = Modifier.fillMaxSize(),
                        onThreadClick = { threadId ->
                            shellViewModel.hideApprovalsList()
                            shellViewModel.openThread(threadId)
                        },
                    )
                }
                else -> {
                    Scaffold(
                        containerColor = AetherBackground,
                        bottomBar = {
                            NavigationBar(
                                containerColor = AetherSurface,
                                tonalElevation = 0.dp,
                            ) {
                                NavigationBarItem(
                                    selected = selectedTab == ShellViewModel.TAB_THREADS,
                                    onClick = { shellViewModel.selectTab(ShellViewModel.TAB_THREADS) },
                                    icon = { InboxIconWithBadge(pendingCount) },
                                    label = { Text("inbox", style = MaterialTheme.typography.labelSmall) },
                                    colors = navItemColors(),
                                )
                                NavigationBarItem(
                                    selected = selectedTab == ShellViewModel.TAB_SETTINGS,
                                    onClick = { shellViewModel.selectTab(ShellViewModel.TAB_SETTINGS) },
                                    icon = {
                                        Icon(
                                            Icons.Filled.Settings,
                                            contentDescription = "settings",
                                            modifier = Modifier.size(22.dp),
                                        )
                                    },
                                    label = { Text("settings", style = MaterialTheme.typography.labelSmall) },
                                    colors = navItemColors(),
                                )
                            }
                        },
                    ) { innerPadding ->
                        Box(
                            modifier = Modifier
                                .fillMaxSize()
                                .gradientBackground(),
                        ) {
                            when (selectedTab) {
                                ShellViewModel.TAB_THREADS -> ThreadsScreen(
                                    onThreadClick = { id -> shellViewModel.openThread(id) },
                                    onApprovalsClick = { shellViewModel.showApprovalsList() },
                                    pendingCount = pendingCount,
                                    onOpenDrawer = openDrawer,
                                    modifier = Modifier.padding(innerPadding),
                                )
                                ShellViewModel.TAB_SETTINGS -> SettingsScreen(Modifier.padding(innerPadding))
                            }
                        }
                    }
                }
            }
        }
    }
}

// ─── Thread Drawer ────────────────────────────────────────────────────────────

@Composable
private fun ThreadDrawer(
    threadsViewModel: ThreadsViewModel,
    onThreadClick: (String) -> Unit,
    onSettings: () -> Unit,
) {
    val threads by threadsViewModel.threads.collectAsStateWithLifecycle()

    LaunchedEffect(Unit) { threadsViewModel.loadThreads() }

    ModalDrawerSheet(
        modifier = Modifier
            .fillMaxHeight()
            .widthIn(min = 304.dp, max = 328.dp),
        drawerContainerColor = AetherSurface,
        drawerShape = RoundedCornerShape(topEnd = 30.dp, bottomEnd = 30.dp),
    ) {
        Box(modifier = Modifier.fillMaxSize()) {
            LazyColumn(
                modifier = Modifier.fillMaxSize(),
                contentPadding = PaddingValues(
                    start = 16.dp,
                    end = 16.dp,
                    top = 100.dp,
                    bottom = 96.dp,
                ),
                verticalArrangement = Arrangement.spacedBy(2.dp),
            ) {
                items(threads, key = { it.id }) { thread ->
                    DrawerThreadRow(
                        thread = thread,
                        onClick = { onThreadClick(thread.id) },
                        onDelete = { threadsViewModel.deleteThread(thread.id) },
                    )
                }
            }

            // Header
            Column(
                modifier = Modifier
                    .align(Alignment.TopStart)
                    .fillMaxWidth()
                    .statusBarsPadding()
                    .padding(horizontal = 16.dp, vertical = 18.dp),
            ) {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        text = "acorn",
                        style = MaterialTheme.typography.headlineSmall.copy(
                            fontWeight = FontWeight.SemiBold,
                        ),
                        color = AetherOnSurface,
                    )
                    DrawerCircleButton(
                        icon = Icons.Filled.Settings,
                        contentDescription = "settings",
                        onClick = onSettings,
                    )
                }
            }

            // FAB — new chat
            Box(
                modifier = Modifier
                    .align(Alignment.BottomEnd)
                    .navigationBarsPadding()
                    .padding(end = 18.dp, bottom = 18.dp)
                    .size(54.dp)
                    .shadow(8.dp, CircleShape)
                    .clip(CircleShape)
                    .background(AetherPrimary)
                    .clickable {
                        threadsViewModel.createNewThread(onThreadClick)
                    },
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    Icons.Filled.Add,
                    contentDescription = "new chat",
                    tint = AetherOnPrimary,
                    modifier = Modifier.size(24.dp),
                )
            }
        }
    }
}

@Composable
private fun DrawerCircleButton(
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    contentDescription: String,
    onClick: () -> Unit,
) {
    Box(
        modifier = Modifier
            .size(46.dp)
            .clip(CircleShape)
            .background(AetherSurface.copy(alpha = 0.9f))
            .clickable(onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            icon,
            contentDescription = contentDescription,
            tint = AetherOnSurface,
            modifier = Modifier.size(20.dp),
        )
    }
}

@OptIn(androidx.compose.foundation.ExperimentalFoundationApi::class)
@Composable
private fun DrawerThreadRow(
    thread: Thread,
    onClick: () -> Unit,
    onDelete: () -> Unit,
) {
    var menuExpanded by remember { mutableStateOf(false) }
    var showDeleteDialog by remember { mutableStateOf(false) }

    Box {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(18.dp))
                .combinedClickable(
                    onClick = onClick,
                    onLongClick = { menuExpanded = true },
                )
                .padding(horizontal = 12.dp, vertical = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = thread.title.ifBlank { "untitled" },
                style = MaterialTheme.typography.bodyLarge,
                color = AetherOnSurface,
                maxLines = 1,
                modifier = Modifier.weight(1f),
            )
            val indicatorColor = when (thread.state) {
                Thread.State.running -> AetherSecondary
                Thread.State.failed -> AetherTertiary
                else -> Color.Transparent
            }
            if (indicatorColor != Color.Transparent) {
                Spacer(Modifier.width(10.dp))
                Box(
                    modifier = Modifier
                        .size(8.dp)
                        .clip(CircleShape)
                        .background(indicatorColor),
                )
            }
        }

        DropdownMenu(
            expanded = menuExpanded,
            onDismissRequest = { menuExpanded = false },
        ) {
            DropdownMenuItem(
                text = { Text("Delete", color = AetherTertiary) },
                onClick = {
                    menuExpanded = false
                    showDeleteDialog = true
                },
            )
        }
    }

    if (showDeleteDialog) {
        androidx.compose.material3.AlertDialog(
            onDismissRequest = { showDeleteDialog = false },
            title = { Text("Delete thread?") },
            text = {
                Text("Delete \"${thread.title.ifBlank { "untitled" }}\"? This cannot be undone.")
            },
            confirmButton = {
                androidx.compose.material3.TextButton(onClick = {
                    showDeleteDialog = false
                    onDelete()
                }) { Text("Delete", color = AetherTertiary) }
            },
            dismissButton = {
                androidx.compose.material3.TextButton(onClick = { showDeleteDialog = false }) {
                    Text("Cancel")
                }
            },
        )
    }
}

// ─── Nav bar ─────────────────────────────────────────────────────────────────

@Composable
private fun InboxIconWithBadge(pendingCount: Int) {
    Box(contentAlignment = Alignment.TopEnd) {
        Icon(
            Icons.Filled.Inbox,
            contentDescription = "inbox",
            modifier = Modifier.size(22.dp),
        )
        if (pendingCount > 0) {
            Box(
                modifier = Modifier
                    .size(16.dp)
                    .clip(CircleShape)
                    .background(AetherTertiary),
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    if (pendingCount > 9) "9+" else pendingCount.toString(),
                    style = MaterialTheme.typography.labelSmall,
                    color = AetherOnSurface,
                )
            }
        }
    }
}

@Composable
private fun navItemColors() = NavigationBarItemDefaults.colors(
    selectedIconColor = AetherPrimary,
    selectedTextColor = AetherOnSurfaceVariant,
    unselectedIconColor = AetherOnSurfaceVariant,
    unselectedTextColor = AetherOnSurfaceVariant,
    indicatorColor = AetherPrimaryContainer,
)
