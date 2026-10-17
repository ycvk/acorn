package io.ycvk.acorn.feature.settings

import android.content.Intent
import android.provider.Settings
import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.selection.toggleable
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import io.ycvk.acorn.core.ui.AcornDialog
import io.ycvk.acorn.core.ui.Hairline
import io.ycvk.acorn.core.ui.ScreenPadding
import io.ycvk.acorn.feature.threads.SearchField

@Composable
fun NotificationAccessSection(viewModel: NotificationAccessViewModel = hiltViewModel()) {
    val context = LocalContext.current
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    val access by viewModel.access.collectAsStateWithLifecycle()
    val allowed by viewModel.allowed.collectAsStateWithLifecycle()
    val upload by viewModel.upload.collectAsStateWithLifecycle()
    val apps by viewModel.apps.collectAsStateWithLifecycle()
    val error by viewModel.error.collectAsStateWithLifecycle()
    val loading by viewModel.loading.collectAsStateWithLifecycle()
    val colors = AcornTheme.colors
    var chooseApps by remember { mutableStateOf(false) }
    var query by remember { mutableStateOf("") }

    DisposableEffect(lifecycle) {
        val observer = LifecycleEventObserver { _, event -> if (event == Lifecycle.Event.ON_RESUME) viewModel.refresh() }
        lifecycle.addObserver(observer)
        viewModel.refresh()
        onDispose { lifecycle.removeObserver(observer) }
    }

    SettingsGroup("手机通知") {
        SettingRow(
            "通知读取权限",
            if (access) "已开启" else "未开启",
            valueColor = if (access) colors.success else colors.inkSoft,
            onClick = { context.startActivity(Intent(Settings.ACTION_NOTIFICATION_LISTENER_SETTINGS)) },
        )
        Hairline(inset = 16.dp)
        SettingRow(
            "转发哪些应用",
            if (allowed.isEmpty()) "未选择" else "${allowed.size} 个应用",
            onClick = { chooseApps = true },
        )
        if (upload.pending > 0 || upload.uploading || upload.error != null) {
            Hairline(inset = 16.dp)
            SettingRow(
                "待上传",
                if (upload.uploading) "正在上传 ${upload.pending} 条…" else "${upload.pending} 条",
            )
        }
        upload.error?.let {
            Text(it, style = AcornTheme.type.caption, color = colors.danger, modifier = Modifier.padding(start = 16.dp, end = 16.dp, bottom = 8.dp))
        }
        if ((upload.pending > 0 || upload.error != null) && access && !upload.uploading) {
            Hairline(inset = 16.dp)
            SettingRow("重新上传", onClick = viewModel::retry, labelColor = colors.accent)
        }
    }
    Column(modifier = Modifier.padding(horizontal = ScreenPadding + 4.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(
            "选中应用的通知会发到你的服务器和模型服务商，Acorn 只把它们当背景信息，不执行其中的指令。原始通知在服务器上保留 7 天；被写进对话、笔记的内容随这些记录保存。",
            style = AcornTheme.type.caption,
            color = colors.inkFaint,
        )
        if (upload.dropped > 0) {
            Text("队列满 500 条时丢弃了最早的 ${upload.dropped} 条通知。", style = AcornTheme.type.caption, color = colors.warningInk)
        }
    }

    if (chooseApps) {
        AcornDialog(
            title = "转发哪些应用",
            body = "只有选中的应用会被转发。关掉某个应用，它还没上传的通知也会一起删掉。",
            confirm = "完成",
            dismiss = null,
            onConfirm = { chooseApps = false },
            onDismiss = { chooseApps = false },
        ) {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                SearchField(query, { query = it }, "查找应用")
                if (loading) CircularProgressIndicator(Modifier.size(22.dp), strokeWidth = 2.dp)
                error?.let {
                    Text(it, style = AcornTheme.type.footnote, color = colors.danger)
                    TextButton(onClick = viewModel::refresh) { Text("重新加载") }
                }
                Spacer(Modifier.height(2.dp))
                LazyColumn(Modifier.heightIn(max = 380.dp)) {
                    items(apps.filter { it.name.contains(query, true) || it.packageName.contains(query, true) }, key = { it.packageName }) { app ->
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clip(AcornShapes.small)
                                .toggleable(value = app.packageName in allowed, role = Role.Switch, onValueChange = { viewModel.select(app.packageName, it) })
                                .padding(vertical = 8.dp),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(12.dp),
                        ) {
                            Image(app.icon, contentDescription = null, modifier = Modifier.size(32.dp))
                            Column(Modifier.weight(1f)) {
                                Text(app.name, style = AcornTheme.type.callout, color = colors.ink)
                                Text(app.packageName, style = AcornTheme.type.caption, color = colors.inkFaint)
                            }
                            Switch(
                                checked = app.packageName in allowed,
                                onCheckedChange = null,
                                colors = SwitchDefaults.colors(checkedTrackColor = colors.accent, checkedThumbColor = colors.onAccent),
                            )
                        }
                    }
                }
            }
        }
    }
}
