package io.ycvk.acorn.core.ui

import io.ycvk.acorn.api.infrastructure.ClientError
import io.ycvk.acorn.api.infrastructure.ClientException
import io.ycvk.acorn.api.infrastructure.Serializer
import io.ycvk.acorn.api.infrastructure.ServerException
import java.io.IOException
import java.net.ConnectException
import java.net.SocketTimeoutException
import java.net.UnknownHostException

/** What went wrong, in words the owner can act on; unrecognised failures keep their own message. */
fun userMessage(e: Throwable): String = when (e) {
    is ClientException -> when (e.statusCode) {
        401, 403 -> "这台设备的授权已失效，需要重新配对"
        404 -> "内容不存在或已被删除"
        409 -> "内容刚被改过，刷新后再试"
        429 -> "请求太频繁，稍后再试"
        else -> serverMessage(e)?.let { "服务器拒绝了这个请求：$it" } ?: "服务器拒绝了这个请求（${e.statusCode}）"
    }
    is ServerException -> "服务器出错了（${e.statusCode}），稍后再试"
    is UnknownHostException, is ConnectException -> "连不上服务器，检查网络或服务器地址"
    is SocketTimeoutException -> "服务器响应超时"
    is IOException -> "网络连接中断了"
    else -> e.message ?: e.javaClass.simpleName
}

/** The `error.message` from an API error body, when the server sent one. */
private fun serverMessage(e: ClientException): String? {
    val body = (e.response as? ClientError<*>)?.body as? String ?: return null
    return runCatching {
        @Suppress("UNCHECKED_CAST")
        val error = Serializer.moshi.adapter(Map::class.java).fromJson(body)?.get("error") as? Map<String, Any?>
        error?.get("message") as? String
    }.getOrNull()
}
