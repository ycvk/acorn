package io.ycvk.acorn.feature.knowledge

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
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
import io.ycvk.acorn.core.theme.AetherError
import io.ycvk.acorn.core.theme.AetherOnSurfaceVariant
import io.ycvk.acorn.core.theme.AetherSurfaceHigh
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
        modifier = modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)),
        loading = { AttachmentPlaceholder(description, isError = false) },
        error = { AttachmentPlaceholder("Couldn't load ${path.substringAfterLast('/')}", isError = true) },
    )
}

@Composable
private fun AttachmentPlaceholder(text: String, isError: Boolean) {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .height(120.dp)
            .background(AetherSurfaceHigh)
            .padding(12.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text,
            style = MaterialTheme.typography.labelSmall,
            color = if (isError) AetherError else AetherOnSurfaceVariant,
        )
    }
}
