package io.ycvk.acorn.feature.shell

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Inbox
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.Badge
import androidx.compose.material3.BadgedBox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationBarItemDefaults
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.core.theme.Accent
import io.ycvk.acorn.core.theme.Bg
import io.ycvk.acorn.core.theme.Surface
import io.ycvk.acorn.core.theme.TextSecondary
import io.ycvk.acorn.core.theme.Warning
import io.ycvk.acorn.feature.approvals.ApprovalsScreen
import io.ycvk.acorn.feature.chat.ChatScreen
import io.ycvk.acorn.feature.pairing.PairingScreen
import io.ycvk.acorn.feature.settings.SettingsScreen
import io.ycvk.acorn.feature.threads.ThreadsScreen

@Composable
fun AcornShell(
    shellViewModel: ShellViewModel = hiltViewModel(),
) {
    val authController = shellViewModel.authController
    val authState by authController.authState.collectAsStateWithLifecycle()

    LaunchedEffect(Unit) {
        authController.loadStoredConnection()
    }

    when (val state = authState) {
        is AuthState.Loading -> {
            Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                CircularProgressIndicator(color = Accent)
            }
        }

        is AuthState.Disconnected -> {
            PairingScreen(
                onPaired = {},
                authController = authController,
            )
        }

        is AuthState.Connected -> ConnectedShell(
            shellViewModel = shellViewModel,
        )
    }
}

@Composable
private fun ConnectedShell(
    shellViewModel: ShellViewModel,
) {
    val selectedTab by shellViewModel.selectedTab.collectAsStateWithLifecycle()
    val openThreadId by shellViewModel.openThreadId.collectAsStateWithLifecycle()
    val pendingCount by shellViewModel.pendingCount.collectAsStateWithLifecycle()

    val currentThreadId = openThreadId
    if (currentThreadId != null) {
        ChatScreen(
            threadId = currentThreadId,
            onBack = { shellViewModel.closeThread() },
        )
        return
    }

    Scaffold(
        containerColor = Bg,
        bottomBar = {
            NavigationBar(
                containerColor = Surface,
                tonalElevation = 0.dp,
            ) {
                NavigationBarItem(
                    selected = selectedTab == ShellViewModel.TAB_THREADS,
                    onClick = { shellViewModel.selectTab(ShellViewModel.TAB_THREADS) },
                    icon = { Icon(Icons.Filled.Inbox, contentDescription = "inbox") },
                    label = { Text("inbox", style = MaterialTheme.typography.labelSmall) },
                    colors = navItemColors(),
                )
                NavigationBarItem(
                    selected = selectedTab == ShellViewModel.TAB_APPROVALS,
                    onClick = { shellViewModel.selectTab(ShellViewModel.TAB_APPROVALS) },
                    icon = {
                        if (pendingCount > 0) {
                            BadgedBox(badge = {
                                Badge(containerColor = Warning, contentColor = Bg) {
                                    Text(pendingCount.toString())
                                }
                            }) {
                                Icon(Icons.Filled.CheckCircle, contentDescription = "approvals")
                            }
                        } else {
                            Icon(Icons.Filled.CheckCircle, contentDescription = "approvals")
                        }
                    },
                    label = { Text("approvals", style = MaterialTheme.typography.labelSmall) },
                    colors = navItemColors(),
                )
                NavigationBarItem(
                    selected = selectedTab == ShellViewModel.TAB_SETTINGS,
                    onClick = { shellViewModel.selectTab(ShellViewModel.TAB_SETTINGS) },
                    icon = { Icon(Icons.Filled.Settings, contentDescription = "settings") },
                    label = { Text("settings", style = MaterialTheme.typography.labelSmall) },
                    colors = navItemColors(),
                )
            }
        },
    ) { innerPadding ->
        when (selectedTab) {
            ShellViewModel.TAB_THREADS -> ThreadsScreen(
                onThreadClick = { id -> shellViewModel.openThread(id) },
                modifier = Modifier.padding(innerPadding),
            )
            ShellViewModel.TAB_APPROVALS -> ApprovalsScreen(Modifier.padding(innerPadding))
            ShellViewModel.TAB_SETTINGS -> SettingsScreen(Modifier.padding(innerPadding))
        }
    }
}

@Composable
private fun navItemColors() = NavigationBarItemDefaults.colors(
    selectedIconColor = Accent,
    selectedTextColor = Accent,
    unselectedIconColor = TextSecondary,
    unselectedTextColor = TextSecondary,
    indicatorColor = Surface,
)
