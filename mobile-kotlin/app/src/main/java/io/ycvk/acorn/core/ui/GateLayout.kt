package io.ycvk.acorn.core.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import io.ycvk.acorn.core.theme.AcornTheme

/** A full-screen step before the app opens: the mark, a title, a lead and the step's content. */
@Composable
fun GateLayout(title: String, lead: String, content: @Composable ColumnScope.() -> Unit) {
    val colors = AcornTheme.colors
    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(colors.canvas)
            .statusBarsPadding()
            .navigationBarsPadding()
            .imePadding()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 24.dp),
    ) {
        Spacer(Modifier.height(72.dp))
        AcornMark(size = 44.dp)
        Spacer(Modifier.height(24.dp))
        Text(title, style = AcornTheme.type.largeTitle, color = colors.ink)
        Spacer(Modifier.height(8.dp))
        Text(lead, style = AcornTheme.type.callout, color = colors.inkSoft)
        Spacer(Modifier.height(36.dp))
        content()
        Spacer(Modifier.height(32.dp))
    }
}
