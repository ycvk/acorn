package io.ycvk.acorn

import android.app.Application
import dagger.hilt.android.HiltAndroidApp
import io.ycvk.acorn.core.push.PushManager
import javax.inject.Inject
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.notifications.NotificationUploader

@HiltAndroidApp
class AcornApp : Application() {
    @Inject lateinit var pushManager: PushManager
    @Inject lateinit var authController: AuthController
    @Inject lateinit var notificationUploader: NotificationUploader

    override fun onCreate() {
        super.onCreate()
        pushManager.initialize()
        notificationUploader.initialize()
        authController.loadStoredConnection()
    }
}
