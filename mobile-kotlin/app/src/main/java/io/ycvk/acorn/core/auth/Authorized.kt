package io.ycvk.acorn.core.auth

import io.ycvk.acorn.api.infrastructure.ApiClient

/** Sends this client's calls with the device token of [profile]. */
fun <T : ApiClient> T.authorized(profile: ConnectionProfile): T = apply { accessTokenProvider = { profile.accessToken } }
