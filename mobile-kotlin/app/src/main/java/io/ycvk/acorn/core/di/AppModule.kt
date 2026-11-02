package io.ycvk.acorn.core.di

import android.content.Context
import androidx.core.app.NotificationManagerCompat
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import io.ycvk.acorn.core.auth.AuthController
import io.ycvk.acorn.core.notifications.NotificationDelivery
import io.ycvk.acorn.core.notifications.NotificationPreferences
import io.ycvk.acorn.core.notifications.NotificationQueue
import io.ycvk.acorn.data.repository.PhoneNotificationRepository
import java.io.File
import javax.inject.Singleton

/** Application-scoped Hilt bindings for phone notification delivery. */
@Module
@InstallIn(SingletonComponent::class)
object AppModule {

    @Provides
    @Singleton
    fun provideNotificationQueue(@ApplicationContext context: Context): NotificationQueue =
        NotificationQueue(File(context.filesDir, "phone-notifications.json"))

    @Provides
    @Singleton
    fun provideNotificationDelivery(
        @ApplicationContext context: Context,
        queue: NotificationQueue,
        auth: AuthController,
        preferences: NotificationPreferences,
        repository: PhoneNotificationRepository,
    ): NotificationDelivery = NotificationDelivery(
        queue = queue,
        profile = { auth.profile },
        allowed = { preferences.allowed.value },
        hasAccess = { NotificationManagerCompat.getEnabledListenerPackages(context).contains(context.packageName) },
        send = repository::send,
    )
}
