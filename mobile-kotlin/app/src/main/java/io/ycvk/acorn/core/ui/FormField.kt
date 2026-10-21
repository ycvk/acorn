package io.ycvk.acorn.core.ui

import androidx.compose.animation.animateColorAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.unit.dp
import io.ycvk.acorn.core.theme.AcornMotion
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme

/** A labelled single-line field for the full-screen forms (pairing, the owner's name); its edge takes the accent while focused. */
@Composable
fun FormField(
    label: String,
    value: String,
    onValueChange: (String) -> Unit,
    placeholder: String,
    enabled: Boolean,
    keyboard: KeyboardOptions,
    onImeAction: () -> Unit,
    modifier: Modifier = Modifier,
    textStyle: TextStyle = AcornTheme.type.body,
) {
    val colors = AcornTheme.colors
    var focused by rememberSaveable { mutableStateOf(false) }
    val edge by animateColorAsState(if (focused) colors.accent else colors.lineStrong, AcornMotion.standard(AcornMotion.SHORT), label = "field_edge")
    Column(modifier = modifier) {
        Text(label, style = AcornTheme.type.eyebrow, color = colors.inkSoft)
        Spacer(Modifier.height(8.dp))
        Box(
            modifier = Modifier
                .fillMaxWidth()
                .height(44.dp)
                .clip(AcornShapes.medium)
                .background(colors.surface)
                .border(if (focused) 1.5.dp else 1.dp, edge, AcornShapes.medium)
                .padding(horizontal = 12.dp),
            contentAlignment = Alignment.CenterStart,
        ) {
            if (value.isEmpty()) Text(placeholder, style = textStyle, color = colors.inkFaint)
            BasicTextField(
                value = value,
                onValueChange = onValueChange,
                enabled = enabled,
                singleLine = true,
                textStyle = textStyle.copy(color = colors.ink),
                cursorBrush = SolidColor(colors.accent),
                keyboardOptions = keyboard,
                keyboardActions = KeyboardActions(onAny = { onImeAction() }),
                modifier = Modifier
                    .fillMaxWidth()
                    .onFocusChanged { focused = it.isFocused },
            )
        }
    }
}
