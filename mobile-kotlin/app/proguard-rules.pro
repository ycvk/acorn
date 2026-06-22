# Keep Kotlin metadata so reflection-based Moshi adapters work.
-keep class kotlin.Metadata { *; }

# Moshi
-keepclassmembers class * {
    @com.squareup.moshi.* <methods>;
}
-keep @com.squareup.moshi.JsonClass class * { *; }
-keep class io.ycvk.acorn.api.models.** { *; }

# OkHttp / Okio
-dontwarn okhttp3.**
-dontwarn okio.**
