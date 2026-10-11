package io.ycvk.acorn.feature.knowledge

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import coil.compose.SubcomposeAsyncImage
import coil.request.ImageRequest
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import java.net.URLEncoder

/** URL of an image the backend stores under attachments/. */
fun attachmentUrl(serverUrl: String, path: String): String =
    serverUrl.trimEnd('/') + "/v1/knowledge/attachment?path=" + URLEncoder.encode(path, "UTF-8")

/** A stored image, fetched with the device token and shown at full width. */
@Composable
fun AttachmentImage(
    path: String,
    description: String,
    profile: ConnectionProfile,
    modifier: Modifier = Modifier,
) {
    val context = LocalContext.current
    val request = remember(path, profile) {
        ImageRequest.Builder(context)
            .data(attachmentUrl(profile.serverUrl, path))
            .addHeader("Authorization", "Bearer ${profile.accessToken}")
            .crossfade(true)
            .build()
    }
    SubcomposeAsyncImage(
        model = request,
        contentDescription = description,
        contentScale = ContentScale.FillWidth,
        modifier = modifier.fillMaxWidth().clip(AcornShapes.medium),
        loading = { AttachmentPlaceholder(description, isError = false) },
        error = { AttachmentPlaceholder("图片没能加载：${path.substringAfterLast('/')}", isError = true) },
    )
}

@Composable
private fun AttachmentPlaceholder(text: String, isError: Boolean) {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .height(120.dp)
            .background(AcornTheme.colors.surfaceMuted)
            .padding(12.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text,
            style = AcornTheme.type.caption,
            color = if (isError) AcornTheme.colors.danger else AcornTheme.colors.inkFaint,
        )
    }
}
