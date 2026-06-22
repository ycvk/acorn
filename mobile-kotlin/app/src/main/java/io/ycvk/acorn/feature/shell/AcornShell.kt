package io.ycvk.acorn.feature.shell

import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Email
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.feature.approvals.ApprovalsScreen
import io.ycvk.acorn.feature.settings.SettingsScreen
import io.ycvk.acorn.feature.threads.ThreadsScreen

@Composable
fun AcornShell(
    shellViewModel: ShellViewModel = hiltViewModel(),
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
