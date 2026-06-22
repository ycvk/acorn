package io.ycvk.acorn.feature.shell

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Email
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.core.auth.AuthState
import io.ycvk.acorn.feature.approvals.ApprovalsScreen
import io.ycvk.acorn.feature.pairing.PairingScreen
import io.ycvk.acorn.feature.settings.SettingsScreen
import io.ycvk.acorn.feature.threads.ThreadsScreen

@Composable
fun AcornShell(
    shellViewModel: ShellViewModel = hiltViewModel(),
) {
    val authController = shellViewModel.authController
    val authState by authController.authState.collectAsStateWithLifecycle()

    // Load any persisted connection on first composition.
    LaunchedEffect(Unit) {
        authController.loadStoredConnection()
    }

    when (val state = authState) {
        is AuthState.Loading -> {
            Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                CircularProgressIndicator()
            }
        }

        is AuthState.Disconnected -> {
            PairingScreen(
                onPaired = {
                    // authState will flip to Connected via AuthController.pair(),
                    // which recomposes this composable to the bottom-nav shell.
                },
                authController = authController,
            )
        }

        is AuthState.Connected -> ConnectedShell(
            shellViewModel = shellViewModel,
            profile = state.profile,
        )
    }
}

@Composable
private fun ConnectedShell(
    shellViewModel: ShellViewModel,
    @Suppress("UNUSED_PARAMETER") profile: io.ycvk.acorn.core.auth.ConnectionProfile,
) {
    val selectedTab by shellViewModel.selectedTab.collectAsStateWithLifecycle()
    val screens = listOf("Threads", "Approvals", "Settings")
    val icons = listOf(Icons.Filled.Email, Icons.Filled.CheckCircle, Icons.Filled.Settings)

    Scaffold(
        bottomBar = {
            NavigationBar {
                screens.forEachIndexed { index, label ->
                    NavigationBarItem(
                        selected = selectedTab == index,
                        onClick = { shellViewModel.selectTab(index) },
                        icon = { Icon(icons[index], contentDescription = label) },
                        label = { Text(label) },
                    )
                }
            }
        },
    ) { innerPadding ->
        when (selectedTab) {
            ShellViewModel.TAB_THREADS -> ThreadsScreen(Modifier.padding(innerPadding))
            ShellViewModel.TAB_APPROVALS -> ApprovalsScreen(Modifier.padding(innerPadding))
            ShellViewModel.TAB_SETTINGS -> SettingsScreen(Modifier.padding(innerPadding))
        }
    }
}
