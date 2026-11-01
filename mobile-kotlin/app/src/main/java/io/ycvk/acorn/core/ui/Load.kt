package io.ycvk.acorn.core.ui

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/** A page's load from the paired server. */
sealed interface Load<out T> {
    data object Loading : Load<Nothing>
    data class Loaded<out T>(val value: T) : Load<T>
    data class Failed(val message: String) : Load<Nothing>
}

/** Runs [block] on the IO dispatcher as a load: what it returns is loaded, an error fails it, cancellation propagates. */
suspend fun <T> loadOf(block: suspend CoroutineScope.() -> T): Load<T> = try {
    Load.Loaded(withContext(Dispatchers.IO, block))
} catch (e: CancellationException) {
    throw e
} catch (e: Exception) {
    Load.Failed(userMessage(e))
}
