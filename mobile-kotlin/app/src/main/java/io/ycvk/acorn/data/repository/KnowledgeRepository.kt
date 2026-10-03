package io.ycvk.acorn.data.repository

import io.ycvk.acorn.api.apis.KnowledgeApi
import io.ycvk.acorn.api.infrastructure.ApiClient
import io.ycvk.acorn.api.models.KnowledgeNote
import io.ycvk.acorn.api.models.KnowledgeNoteSummary
import io.ycvk.acorn.core.auth.ConnectionProfile
import javax.inject.Inject
import javax.inject.Singleton

/** Reads the knowledge base from the paired server. Blocking; call from an IO dispatcher. */
@Singleton
class KnowledgeRepository @Inject constructor() {
    fun notes(profile: ConnectionProfile, query: String): List<KnowledgeNoteSummary> {
        ApiClient.accessToken = profile.accessToken
        return KnowledgeApi(basePath = profile.serverUrl)
            .clientListKnowledgeNotes(q = query.trim().ifEmpty { null }, prefix = null, limit = 50)
            .notes
    }

    fun note(profile: ConnectionProfile, path: String): KnowledgeNote {
        ApiClient.accessToken = profile.accessToken
        return KnowledgeApi(basePath = profile.serverUrl).clientGetKnowledgeNote(path)
    }
}
