package io.ycvk.acorn.core.ui

/** A page's save to the paired server. */
sealed interface Save {
    data object Idle : Save
    data object Saving : Save
    data class Failed(val message: String) : Save
}
