package io.ycvk.acorn.feature.pairing

import android.os.Build
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.animateColorAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusDirection
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.R
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.auth.PairingState
import io.ycvk.acorn.core.theme.AcornMotion
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornButton
import io.ycvk.acorn.core.ui.InlineError
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

@Composable
fun PairingScreen(authController: AuthController) {
    val pairingState by authController.pairingState.collectAsStateWithLifecycle()

    LaunchedEffect(pairingState) {
        if (pairingState is PairingState.Success) authController.resetPairingState()
    }

    var serverUrl by rememberSaveable { mutableStateOf("") }
    var pairingCode by rememberSaveable { mutableStateOf("") }
    var deviceName by rememberSaveable { mutableStateOf(Build.MODEL ?: "Android") }
    val focus = LocalFocusManager.current

    var urlError by rememberSaveable { mutableStateOf<String?>(null) }
    val pairing = pairingState is PairingState.Pairing
    val errorMessage = urlError ?: (pairingState as? PairingState.Error)?.message
    val canPair = serverUrl.isNotBlank() && pairingCode.isNotBlank() && deviceName.isNotBlank()
    val connect = {
        focus.clearFocus()
        urlError = serverUrlProblem(serverUrl.trim())
        if (urlError == null) authController.pair(serverUrl.trim(), pairingCode.trim(), deviceName.trim())
    }
    val colors = AcornTheme.colors

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(colors.canvas)
            .statusBarsPadding()
            .navigationBarsPadding()
            .imePadding()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 28.dp),
    ) {
        Spacer(Modifier.height(72.dp))
        Box(
            modifier = Modifier
                .size(64.dp)
                .background(colors.accentSoft, RoundedCornerShape(20.dp)),
            contentAlignment = Alignment.Center,
        ) {
            Icon(painterResource(R.drawable.ic_acorn), contentDescription = null, tint = colors.accent, modifier = Modifier.size(32.dp))
        }
        Spacer(Modifier.height(24.dp))
        Text("连接你的 Acorn", style = AcornTheme.type.largeTitle, color = colors.ink)
        Spacer(Modifier.height(8.dp))
        Text(
            "在服务器上运行 acorn pair，把显示的地址和配对码填在下面。",
            style = AcornTheme.type.callout,
            color = colors.inkSoft,
        )
        Spacer(Modifier.height(36.dp))

        Field(
            label = "服务器地址",
            value = serverUrl,
            onValueChange = {
                serverUrl = it
                urlError = null
            },
            placeholder = "https://acorn.example.com",
            enabled = !pairing,
            keyboard = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Next, autoCorrectEnabled = false),
            onImeAction = { focus.moveFocus(FocusDirection.Down) },
        )
        Spacer(Modifier.height(18.dp))
        Field(
            label = "配对码",
            value = pairingCode,
            onValueChange = { pairingCode = it.uppercase() },
            placeholder = "XXXX-XXXX-XXXX-XXXX",
            enabled = !pairing,
            textStyle = AcornTheme.type.mono.copy(fontSize = 17.sp, letterSpacing = 1.sp),
            keyboard = KeyboardOptions(
                capitalization = KeyboardCapitalization.Characters,
                keyboardType = KeyboardType.Ascii,
                imeAction = ImeAction.Next,
                autoCorrectEnabled = false,
            ),
            onImeAction = { focus.moveFocus(FocusDirection.Down) },
        )
        Spacer(Modifier.height(18.dp))
        Field(
            label = "设备名称",
            value = deviceName,
            onValueChange = { deviceName = it },
            placeholder = "我的手机",
            enabled = !pairing,
            keyboard = KeyboardOptions(imeAction = ImeAction.Done),
            onImeAction = { if (canPair && !pairing) connect() },
        )

        AnimatedVisibility(visible = errorMessage != null) {
            InlineError(errorMessage.orEmpty(), Modifier.padding(top = 18.dp))
        }

        Spacer(Modifier.height(28.dp))
        AcornButton(
            "连接",
            onClick = connect,
            loading = pairing,
            enabled = canPair,
            modifier = Modifier.fillMaxWidth(),
        )
        Spacer(Modifier.height(16.dp))
        Text(
            "配对码只能用一次，默认 10 分钟内有效。",
            style = AcornTheme.type.caption,
            color = colors.inkFaint,
            textAlign = TextAlign.Center,
            modifier = Modifier.fillMaxWidth(),
        )
        Spacer(Modifier.height(32.dp))
    }
}

/** Why [url] can't be a server address, or null when it can. */
fun serverUrlProblem(url: String): String? {
    val parsed = url.toHttpUrlOrNull()
    return if (parsed == null || !(url.startsWith("http://") || url.startsWith("https://"))) {
        "服务器地址格式不对，应该像 https://acorn.example.com"
    } else {
        null
    }
}

@Composable
private fun Field(
    label: String,
    value: String,
    onValueChange: (String) -> Unit,
    placeholder: String,
    enabled: Boolean,
    keyboard: KeyboardOptions,
    onImeAction: () -> Unit,
    textStyle: TextStyle = AcornTheme.type.body,
) {
    val colors = AcornTheme.colors
    var focused by rememberSaveable { mutableStateOf(false) }
    val edge by animateColorAsState(if (focused) colors.accent else colors.line, AcornMotion.standard(AcornMotion.SHORT), label = "field_edge")
    Column {
        Text(label, style = AcornTheme.type.eyebrow, color = colors.inkSoft)
        Spacer(Modifier.height(8.dp))
        Box(
            modifier = Modifier
                .fillMaxWidth()
                .height(54.dp)
                .clip(AcornShapes.medium)
                .background(colors.surface)
                .border(if (focused) 1.5.dp else 1.dp, edge, AcornShapes.medium)
                .padding(horizontal = 16.dp),
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
