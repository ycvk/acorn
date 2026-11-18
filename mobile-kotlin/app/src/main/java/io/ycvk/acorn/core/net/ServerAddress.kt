package io.ycvk.acorn.core.net

import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

private val ipv4 = Regex("""^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$""")

/**
 * Why [url] can't be the server address, or null when it can. Every request
 * the app makes goes to this address, so http is taken only for a host inside
 * the owner's own networks; a stored address is held to the same rule.
 */
fun serverUrlProblem(url: String): String? {
    val parsed = url.toHttpUrlOrNull()
    return when {
        parsed == null || !(url.startsWith("http://") || url.startsWith("https://")) -> "服务器地址格式不对，应该像 https://acorn.example.com"
        parsed.scheme == "http" && !isInternalHost(parsed.host) -> "公网上的服务器要用 https://，http:// 只用于局域网、Tailscale 和本机的地址"
        else -> null
    }
}

/**
 * Whether [host] is inside the owner's own networks, where the app may talk
 * plain http: private IPv4 (10/8, 172.16/12, 192.168/16), Tailscale's
 * 100.64/10, link-local, IPv6 unique-local and link-local, loopback and
 * localhost, and names under .local or .ts.net. Any other host is public and
 * needs https. The host is judged as written, never looked up.
 */
fun isInternalHost(host: String): Boolean {
    val name = host.lowercase().removeSuffix(".")
    if (name == "localhost" || name.endsWith(".local") || name.endsWith(".ts.net")) return true
    // OkHttp writes an IPv6 literal in its shortest form, and one that maps an IPv4 address as that address.
    val literal = if (':' in name) "http://[${name.removePrefix("[").removeSuffix("]")}]/".toHttpUrlOrNull()?.host ?: return false else name
    ipv4Octets(literal)?.let { return internalIpv4(it) }
    if (':' !in literal) return false
    if (literal == "::1") return true
    if (literal.startsWith("::")) return false
    val first = literal.substringBefore(':').toInt(16)
    return (first and 0xfe00) == 0xfc00 || (first and 0xffc0) == 0xfe80
}

private fun ipv4Octets(name: String): List<Int>? =
    ipv4.matchEntire(name)?.groupValues?.drop(1)?.map { it.toInt() }?.takeIf { octets -> octets.all { it <= 255 } }

private fun internalIpv4(octets: List<Int>): Boolean {
    val (a, b) = octets
    return a == 10 || a == 127 ||
        a == 172 && b in 16..31 ||
        a == 192 && b == 168 ||
        a == 100 && b in 64..127 ||
        a == 169 && b == 254
}
