package io.ycvk.acorn.core.net

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.content.ContextCompat
import dagger.hilt.android.qualifiers.ApplicationContext
import java.net.Inet6Address
import java.net.InetAddress
import java.net.URI
import java.net.UnknownHostException
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * Android 17 keeps apps that target it off the local network until the owner
 * allows nearby devices (ACCESS_LOCAL_NETWORK). A server on the LAN is then out
 * of reach and its connections only time out, so the app asks before it
 * connects and says why when the owner has said no.
 */
@Singleton
class LocalNetworkAccess @Inject constructor(@ApplicationContext private val context: Context) {
    /** Whether reaching the server at [serverUrl] needs the permission and the owner has not given it. */
    suspend fun needsPermission(serverUrl: String): Boolean {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.CINNAMON_BUN) return false
        if (ContextCompat.checkSelfPermission(context, PERMISSION) == PackageManager.PERMISSION_GRANTED) return false
        val host = URI(serverUrl).host ?: return false
        return withContext(Dispatchers.IO) { isLocalNetworkHost(host, resolve(host)) }
    }

    companion object {
        const val PERMISSION = Manifest.permission.ACCESS_LOCAL_NETWORK
    }
}

/** Whether traffic to [host], which resolves to [addresses], stays on the local network. */
fun isLocalNetworkHost(host: String, addresses: List<InetAddress>): Boolean =
    host.endsWith(".local", ignoreCase = true) || addresses.any(::isLocalNetworkAddress)

/** Private and link-local IPv4, link-local and unique-local IPv6; loopback is the phone itself. */
fun isLocalNetworkAddress(address: InetAddress): Boolean = when {
    address.isLoopbackAddress -> false
    address.isSiteLocalAddress || address.isLinkLocalAddress -> true
    address is Inet6Address -> (address.address[0].toInt() and 0xfe) == 0xfc
    else -> false
}

/**
 * The host's addresses. A `.local` name resolves over multicast DNS, which is
 * itself local traffic, so it is not looked up. A name that does not resolve
 * is not judged local; the connection then reports the unknown host itself.
 */
private fun resolve(host: String): List<InetAddress> {
    if (host.endsWith(".local", ignoreCase = true)) return emptyList()
    return try {
        InetAddress.getAllByName(host).toList()
    } catch (_: UnknownHostException) {
        emptyList()
    }
}

/** What the app says when the owner has not allowed nearby devices for a server on the LAN. */
const val LocalNetworkDenied = "这个服务器在局域网里。Android 17 起，要在系统设置里允许 Acorn 使用「附近的设备」，才能连到它。"
