package io.ycvk.acorn.data.repository

import io.ycvk.acorn.api.apis.MemoriesApi
import io.ycvk.acorn.api.infrastructure.ApiClient
import io.ycvk.acorn.api.models.CorrectMemoryRequest
import io.ycvk.acorn.api.models.MemoryDetail
import io.ycvk.acorn.api.models.MemoryListResponse
import io.ycvk.acorn.api.models.MemorySummary
import io.ycvk.acorn.core.auth.ConnectionProfile
import javax.inject.Inject
import javax.inject.Singleton

/** Reads and changes what Acorn remembers on the paired server. Blocking; call from an IO dispatcher. */
@Singleton
class MemoryRepository @Inject constructor() {
    fun list(profile: ConnectionProfile, kind: MemorySummary.Kind?, query: String, limit: Int): MemoryListResponse =
        api(profile).clientListMemories(
            kind = kind?.let { k -> MemoriesApi.KindClientListMemories.entries.first { it.value == k.value } },
            q = query.trim().ifEmpty { null },
            limit = limit,
        )

    fun get(profile: ConnectionProfile, id: String): MemoryDetail = api(profile).clientGetMemory(id)

    fun forget(profile: ConnectionProfile, id: String) {
        api(profile).clientForgetMemory(id)
    }

    fun correct(profile: ConnectionProfile, id: String, revision: Long, content: String): MemorySummary =
        api(profile).clientCorrectMemory(id, CorrectMemoryRequest(revision = revision, content = content))

    private fun api(profile: ConnectionProfile): MemoriesApi {
        ApiClient.accessToken = profile.accessToken
        return MemoriesApi(basePath = profile.serverUrl)
    }
}
