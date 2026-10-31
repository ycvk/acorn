package io.ycvk.acorn.core.ui

/** A page's load from the paired server. */
sealed interface Load<out T> {
    data object Loading : Load<Nothing>
    data class Loaded<out T>(val value: T) : Load<T>
    data class Failed(val message: String) : Load<Nothing>
}
