package io.ycvk.acorn.core.ui

import android.content.Context
import coil3.network.NetworkHeaders
import coil3.network.httpHeaders
import coil3.request.ImageRequest
import coil3.request.crossfade
import io.ycvk.acorn.core.auth.ConnectionProfile

/** An image request to the paired server, sent with the device token. */
fun authorizedImage(context: Context, url: String, profile: ConnectionProfile): ImageRequest =
    ImageRequest.Builder(context)
        .data(url)
        .httpHeaders(NetworkHeaders.Builder().set("Authorization", "Bearer ${profile.accessToken}").build())
        .crossfade(true)
        .build()
