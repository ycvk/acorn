package io.ycvk.acorn.data.repository

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Matrix
import android.net.Uri
import androidx.exifinterface.media.ExifInterface
import dagger.hilt.android.qualifiers.ApplicationContext
import io.ycvk.acorn.api.infrastructure.ClientError
import io.ycvk.acorn.api.infrastructure.ClientException
import io.ycvk.acorn.api.infrastructure.Serializer
import io.ycvk.acorn.api.infrastructure.ServerError
import io.ycvk.acorn.api.infrastructure.ServerException
import io.ycvk.acorn.api.models.ThreadFile
import io.ycvk.acorn.core.auth.ConnectionProfile
import io.ycvk.acorn.feature.chat.FileView
import io.ycvk.acorn.feature.chat.PendingAttachment
import okhttp3.MediaType
import okhttp3.MediaType.Companion.toMediaTypeOrNull
import okhttp3.MultipartBody
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import okio.BufferedSink
import okio.source
import java.io.ByteArrayOutputStream
import java.io.File
import java.io.IOException
import java.util.concurrent.TimeUnit
import javax.inject.Inject
import javax.inject.Singleton
import kotlin.math.max

/**
 * Moves the files of conversations between the phone and the paired server.
 * Blocking; call from an IO dispatcher. Uploads stream from the picked file,
 * and downloads land in the app's cache, where a second open reuses them.
 */
@Singleton
class ThreadFileRepository @Inject constructor(@ApplicationContext private val context: Context) {
    // No overall call timeout: a large file on a slow network takes as long as it takes.
    private val client = OkHttpClient.Builder()
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(60, TimeUnit.SECONDS)
        .writeTimeout(60, TimeUnit.SECONDS)
        .build()

    fun upload(profile: ConnectionProfile, threadId: String, attachment: PendingAttachment): ThreadFile {
        val (name, body) = if (attachment.photo) {
            photoName(attachment.name) to shrinkPhoto(attachment.uri).toRequestBody("image/jpeg".toMediaTypeOrNull())
        } else {
            attachment.name to UriBody(attachment.uri, attachment.mimeType.toMediaTypeOrNull(), attachment.sizeBytes)
        }
        val multipart = MultipartBody.Builder()
            .setType(MultipartBody.FORM)
            .addFormDataPart("file", name, body)
            .build()
        val request = Request.Builder()
            .url("${profile.serverUrl.trimEnd('/')}/v1/threads/$threadId/files")
            .header("Authorization", "Bearer ${profile.accessToken}")
            .post(multipart)
            .build()
        client.newCall(request).execute().use { response ->
            val text = response.body?.string().orEmpty()
            requireSuccess(response, text)
            return Serializer.moshi.adapter(ThreadFile::class.java).fromJson(text)
                ?: throw IOException("the server answered the upload without a file")
        }
    }

    /** The file in the app's cache, downloaded first unless an earlier download is complete. */
    fun download(profile: ConnectionProfile, threadId: String, file: FileView): File {
        val id = requireNotNull(file.id) { "file ${file.name} is not uploaded" }
        val dir = File(context.cacheDir, "thread-files/$id").apply { mkdirs() }
        val target = File(dir, file.name.replace('/', '_'))
        if (target.length() == file.sizeBytes) return target
        val request = Request.Builder()
            .url("${profile.serverUrl.trimEnd('/')}/v1/threads/$threadId/files/$id")
            .header("Authorization", "Bearer ${profile.accessToken}")
            .build()
        client.newCall(request).execute().use { response ->
            if (!response.isSuccessful) requireSuccess(response, response.body?.string().orEmpty())
            val partial = File(dir, ".partial")
            partial.outputStream().use { out -> response.body!!.byteStream().copyTo(out) }
            if (partial.length() != file.sizeBytes) {
                partial.delete()
                throw IOException("download of ${file.name} ended at ${partial.length()} of ${file.sizeBytes} bytes")
            }
            if (!partial.renameTo(target)) throw IOException("could not keep the download of ${file.name}")
        }
        return target
    }

    /** Copies a downloaded file to where the owner chose to save it. */
    fun saveTo(file: File, destination: Uri) {
        val out = context.contentResolver.openOutputStream(destination) ?: throw IOException("cannot write to $destination")
        out.use { sink -> file.inputStream().use { it.copyTo(sink) } }
    }

    /** Fails with the generated client's exceptions, so the app words the error the same way. */
    private fun requireSuccess(response: Response, body: String) {
        val headers = response.headers.toMultimap()
        when (response.code) {
            in 200..299 -> return
            in 400..499 -> throw ClientException("Client error : ${response.code}", response.code, ClientError<Any>(response.message, body, response.code, headers))
            else -> throw ServerException("Server error : ${response.code}", response.code, ServerError<Any>(response.message, body, response.code, headers))
        }
    }

    /**
     * A photo as JPEG at most [MAX_PHOTO_EDGE] pixels on its long edge,
     * turned upright and with transparency on white.
     */
    private fun shrinkPhoto(uri: Uri): ByteArray {
        val resolver = context.contentResolver
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        resolver.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, bounds) }
        if (bounds.outWidth <= 0 || bounds.outHeight <= 0) throw IOException("this picture's format cannot be read; send it with 文件 instead")
        var sample = 1
        while (max(bounds.outWidth, bounds.outHeight) / (sample * 2) >= MAX_PHOTO_EDGE) sample *= 2
        val decoded = resolver.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, BitmapFactory.Options().apply { inSampleSize = sample }) }
            ?: throw IOException("this picture cannot be read; send it with 文件 instead")
        val orientation = resolver.openInputStream(uri)?.use { ExifInterface(it).getAttributeInt(ExifInterface.TAG_ORIENTATION, ExifInterface.ORIENTATION_NORMAL) }
            ?: ExifInterface.ORIENTATION_NORMAL
        val scale = minOf(1f, MAX_PHOTO_EDGE.toFloat() / max(decoded.width, decoded.height))
        val matrix = Matrix().apply {
            postScale(scale, scale)
            when (orientation) {
                ExifInterface.ORIENTATION_ROTATE_90 -> postRotate(90f)
                ExifInterface.ORIENTATION_ROTATE_180 -> postRotate(180f)
                ExifInterface.ORIENTATION_ROTATE_270 -> postRotate(270f)
                ExifInterface.ORIENTATION_FLIP_HORIZONTAL -> postScale(-1f, 1f)
                ExifInterface.ORIENTATION_FLIP_VERTICAL -> postScale(1f, -1f)
            }
        }
        val upright = Bitmap.createBitmap(decoded, 0, 0, decoded.width, decoded.height, matrix, true)
        val opaque = if (upright.hasAlpha()) {
            Bitmap.createBitmap(upright.width, upright.height, Bitmap.Config.ARGB_8888).also {
                Canvas(it).apply { drawColor(Color.WHITE); drawBitmap(upright, 0f, 0f, null) }
            }
        } else {
            upright
        }
        return ByteArrayOutputStream().use { out ->
            opaque.compress(Bitmap.CompressFormat.JPEG, PHOTO_QUALITY, out)
            out.toByteArray()
        }
    }

    /** Streams a picked file into a request without copying it first. */
    private inner class UriBody(private val uri: Uri, private val type: MediaType?, private val size: Long) : RequestBody() {
        override fun contentType() = type
        override fun contentLength() = size
        override fun writeTo(sink: BufferedSink) {
            val input = context.contentResolver.openInputStream(uri) ?: throw IOException("cannot read $uri")
            input.source().use { sink.writeAll(it) }
        }
    }

    private companion object {
        const val MAX_PHOTO_EDGE = 2048
        const val PHOTO_QUALITY = 85
    }
}

/** A photo's name once it is sent as JPEG. */
fun photoName(name: String): String = name.substringBeforeLast('.', name).ifBlank { "photo" } + ".jpg"
