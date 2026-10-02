package io.ycvk.acorn

import android.app.Application
import dagger.hilt.android.HiltAndroidApp
import io.ycvk.acorn.core.push.PushManager
import javax.inject.Inject

@HiltAndroidApp
class AcornApp : Application() {
    @Inject lateinit var pushManager: PushManager

    override fun onCreate() {
        super.onCreate()
        pushManager.initialize()
    }
}
