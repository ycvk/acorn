package io.ycvk.acorn.feature.chat

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.provider.OpenableColumns
import androidx.activity.result.contract.ActivityResultContract
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.FileDownload
import androidx.compose.material.icons.outlined.Image
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.core.content.FileProvider
import coil.compose.SubcomposeAsyncImage
import coil.request.ImageRequest
import io.ycvk.acorn.api.models.ThreadFile
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import java.io.File
import java.util.Locale
import java.util.UUID

/** A file in a conversation as the chat shows it; [id] is null until it is uploaded. */
data class FileView(
    val id: String?,
    val name: String,
    val mimeType: String,
    val sizeBytes: Long,
) {
    val isImage: Boolean get() = mimeType in inlineImageTypes
}

private val inlineImageTypes = setOf("image/jpeg", "image/png", "image/webp", "image/gif")

fun fileView(file: ThreadFile) = FileView(file.id, file.name, file.mimeType, file.sizeBytes)

/**
 * A file picked in the composer, not sent yet. A [photo] is resized for
 * viewing before upload; other files go unchanged.
 */
data class PendingAttachment(
    val uri: Uri,
    val name: String,
    val mimeType: String,
    val sizeBytes: Long,
    val photo: Boolean,
    val key: String = UUID.randomUUID().toString(),
) {
    fun view() = FileView(null, name, mimeType, sizeBytes)
}

/** Reads a picked file's name, size and type from its provider. */
fun pendingAttachment(context: Context, uri: Uri, photo: Boolean): PendingAttachment {
    val resolver = context.contentResolver
    var name: String? = null
    var size = -1L
    resolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE), null, null, null)?.use { cursor ->
        if (cursor.moveToFirst()) {
            name = cursor.getString(0)
            if (!cursor.isNull(1)) size = cursor.getLong(1)
        }
    }
    val mime = resolver.getType(uri) ?: "application/octet-stream"
    return PendingAttachment(uri, name ?: uri.lastPathSegment ?: "file", mime, size, photo)
}

/** A size such as 2.3 MB, or blank when unknown. */
fun formatBytes(bytes: Long): String = when {
    bytes < 0 -> ""
    bytes >= 1L shl 20 -> String.format(Locale.ROOT, "%.1f MB", bytes / 1048576.0)
    bytes >= 1L shl 10 -> String.format(Locale.ROOT, "%.0f KB", bytes / 1024.0)
    else -> "$bytes B"
}

/** Opens a downloaded file in another app; false when no app takes its type. */
fun openFile(context: Context, file: File, mimeType: String): Boolean {
    val uri = FileProvider.getUriForFile(context, context.packageName + ".files", file)
    val intent = Intent(Intent.ACTION_VIEW)
        .setDataAndType(uri, mimeType)
        .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
    return try {
        context.startActivity(Intent.createChooser(intent, file.name).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        true
    } catch (_: ActivityNotFoundException) {
        false
    }
}

/** Asks where to save a file, offering its name and type. */
class SaveFileContract : ActivityResultContract<FileView, Uri?>() {
    override fun createIntent(context: Context, input: FileView): Intent =
        Intent(Intent.ACTION_CREATE_DOCUMENT)
            .addCategory(Intent.CATEGORY_OPENABLE)
            .setType(input.mimeType)
            .putExtra(Intent.EXTRA_TITLE, input.name)

    override fun parseResult(resultCode: Int, intent: Intent?): Uri? = intent?.data
}

/** The files a message carries: pictures shown inline, other files as cards. */
@Composable
fun MessageFiles(
    files: List<FileView>,
    threadId: String?,
    profile: ConnectionProfile?,
    busy: Set<String>,
    onOpen: (FileView) -> Unit,
    onSave: (FileView) -> Unit,
    modifier: Modifier = Modifier,
    alignment: Alignment.Horizontal = Alignment.Start,
) {
    if (files.isEmpty()) return
    Column(modifier = modifier, horizontalAlignment = alignment, verticalArrangement = Arrangement.spacedBy(6.dp)) {
        files.forEach { file ->
            if (file.isImage && file.id != null && threadId != null && profile != null) {
                ThreadImage(threadId, file, profile, onOpen = { onOpen(file) })
            } else {
                FileCard(file, loading = file.id in busy, onOpen = { onOpen(file) }, onSave = { onSave(file) })
            }
        }
    }
}

@Composable
private fun ThreadImage(threadId: String, file: FileView, profile: ConnectionProfile, onOpen: () -> Unit) {
    val context = LocalContext.current
    val request = remember(threadId, file.id, profile) {
        ImageRequest.Builder(context)
            .data("${profile.serverUrl.trimEnd('/')}/v1/threads/$threadId/files/${file.id}")
            .addHeader("Authorization", "Bearer ${profile.accessToken}")
            .crossfade(true)
            .build()
    }
    SubcomposeAsyncImage(
        model = request,
        contentDescription = file.name,
        contentScale = ContentScale.FillWidth,
        modifier = Modifier
            .widthIn(max = 260.dp)
            .heightIn(max = 320.dp)
            .clip(AcornShapes.medium)
            .clickable(onClick = onOpen),
        loading = { Placeholder(file.name) },
        error = { Placeholder("图片没能加载：${file.name}") },
    )
}

@Composable
private fun Placeholder(text: String) {
    Box(
        modifier = Modifier
            .width(200.dp)
            .height(120.dp)
            .background(AcornTheme.colors.surfaceMuted)
            .padding(12.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(text, style = AcornTheme.type.caption, color = AcornTheme.colors.inkFaint)
    }
}

@Composable
private fun FileCard(file: FileView, loading: Boolean, onOpen: () -> Unit, onSave: () -> Unit) {
    val colors = AcornTheme.colors
    val ready = file.id != null
    Row(
        modifier = Modifier
            .widthIn(max = 300.dp)
            .clip(AcornShapes.medium)
            .background(colors.surface)
            .border(0.5.dp, colors.line, AcornShapes.medium)
            .clickable(enabled = ready && !loading, onClickLabel = "打开", onClick = onOpen)
            .padding(start = 12.dp, end = 4.dp, top = 8.dp, bottom = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(
            if (file.mimeType.startsWith("image/")) Icons.Outlined.Image else Icons.Outlined.Description,
            contentDescription = null,
            tint = colors.inkSoft,
            modifier = Modifier.size(22.dp),
        )
        Spacer(Modifier.width(10.dp))
        Column(modifier = Modifier.weight(1f, fill = false)) {
            Text(file.name, style = AcornTheme.type.callout, color = colors.ink, maxLines = 2, overflow = TextOverflow.Ellipsis)
            val size = formatBytes(file.sizeBytes)
            if (size.isNotEmpty()) Text(size, style = AcornTheme.type.caption, color = colors.inkFaint)
        }
        if (ready) {
            Box(
                modifier = Modifier
                    .size(40.dp)
                    .clip(CircleShape)
                    .clickable(enabled = !loading, onClickLabel = "保存到手机", onClick = onSave),
                contentAlignment = Alignment.Center,
            ) {
                if (loading) {
                    CircularProgressIndicator(strokeWidth = 2.dp, color = colors.inkSoft, modifier = Modifier.size(18.dp))
                } else {
                    Icon(Icons.Outlined.FileDownload, contentDescription = "保存到手机", tint = colors.inkSoft, modifier = Modifier.size(20.dp))
                }
            }
        } else {
            Spacer(Modifier.width(8.dp))
        }
    }
}

/** Files picked in the composer, each with a button that drops it. */
@Composable
fun PendingAttachments(attachments: List<PendingAttachment>, enabled: Boolean, onRemove: (PendingAttachment) -> Unit) {
    val colors = AcornTheme.colors
    Column(modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        attachments.forEach { attachment ->
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(AcornShapes.medium)
                    .background(colors.surfaceMuted)
                    .padding(start = 12.dp, end = 2.dp, top = 2.dp, bottom = 2.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(
                    if (attachment.photo) Icons.Outlined.Image else Icons.Outlined.Description,
                    contentDescription = null,
                    tint = colors.inkSoft,
                    modifier = Modifier.size(18.dp),
                )
                Spacer(Modifier.width(8.dp))
                Text(
                    attachment.name,
                    style = AcornTheme.type.footnote,
                    color = colors.ink,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                val size = if (attachment.photo) "" else formatBytes(attachment.sizeBytes)
                if (size.isNotEmpty()) {
                    Text(size, style = AcornTheme.type.caption, color = colors.inkFaint, modifier = Modifier.padding(start = 8.dp))
                }
                Box(
                    modifier = Modifier
                        .size(36.dp)
                        .clip(CircleShape)
                        .clickable(enabled = enabled, onClickLabel = "移除") { onRemove(attachment) },
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(Icons.Outlined.Close, contentDescription = "移除", tint = colors.inkFaint, modifier = Modifier.size(16.dp))
                }
            }
        }
    }
}
