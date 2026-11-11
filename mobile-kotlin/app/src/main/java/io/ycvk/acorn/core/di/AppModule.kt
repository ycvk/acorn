package io.ycvk.acorn.core.di

import android.content.Context
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import io.ycvk.acorn.data.repository.ThreadSnapshots
import java.io.File
import javax.inject.Singleton

/** Application-scoped Hilt bindings for what the app keeps in files: copies of conversations. */
@Module
@InstallIn(SingletonComponent::class)
object AppModule {

    @Provides
    @Singleton
    fun provideThreadSnapshots(@ApplicationContext context: Context): ThreadSnapshots =
        ThreadSnapshots(File(context.cacheDir, "threads"))
}
