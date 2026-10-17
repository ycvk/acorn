package io.ycvk.acorn.core.ui

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.addPathNodes
import androidx.compose.ui.unit.dp

/** Acorn's own line icons: one 24-unit grid, round caps and joins, a light stroke that matches the type. */
object AcornIcons {
    val Today by lazy {
        icon(
            "today",
            "M8 12a4 4 0 1 0 8 0a4 4 0 1 0 -8 0 M12 3.5v2 M12 18.5v2 M3.5 12h2 M18.5 12h2 " +
                "M6 6l1.4 1.4 M16.6 16.6l1.4 1.4 M6 18l1.4 -1.4 M16.6 7.4l1.4 -1.4",
        )
    }
    val Threads by lazy {
        icon("threads", "M7.5 4.5h9a4 4 0 0 1 4 4v4.5a4 4 0 0 1 -4 4h-5l-4 3v-3a4 4 0 0 1 -4 -4v-4.5a4 4 0 0 1 4 -4z")
    }
    val Knowledge by lazy {
        icon("knowledge", "M12 6.5C10.4 5.2 8.2 4.5 4 4.5v13.5c4.2 0 6.4 0.7 8 2c1.6 -1.3 3.8 -2 8 -2V4.5c-4.2 0 -6.4 0.7 -8 2z M12 6.5v13.5")
    }
    val Compose by lazy {
        icon(
            "compose",
            "M12 4.5H7.5a3 3 0 0 0 -3 3v9a3 3 0 0 0 3 3h9a3 3 0 0 0 3 -3V12 " +
                "M17.6 3.9a1.6 1.6 0 0 1 2.3 2.3L12.8 13.3l-3.1 0.8l0.8 -3.1z",
        )
    }
    val Search by lazy { icon("search", "M10.5 4a6.5 6.5 0 1 0 0 13a6.5 6.5 0 1 0 0 -13z M15.5 15.5l4.5 4.5") }
    val Settings by lazy {
        icon(
            "settings",
            "M4 7.5h9 M17 7.5h3 M4 16.5h3 M11 16.5h9 " +
                "M15 5.5a2 2 0 1 1 0 4a2 2 0 1 1 0 -4z M9 14.5a2 2 0 1 1 0 4a2 2 0 1 1 0 -4z",
        )
    }
    val Globe by lazy {
        icon(
            "globe",
            "M12 3.5a8.5 8.5 0 1 0 0 17a8.5 8.5 0 1 0 0 -17z M3.5 12h17 " +
                "M12 3.5c-2.4 2.4 -3.5 5.3 -3.5 8.5s1.1 6.1 3.5 8.5 M12 3.5c2.4 2.4 3.5 5.3 3.5 8.5s-1.1 6.1 -3.5 8.5",
        )
    }
    val More by lazy {
        icon(
            "more",
            "M5.5 10.4a1.6 1.6 0 1 1 0 3.2a1.6 1.6 0 1 1 0 -3.2z M12 10.4a1.6 1.6 0 1 1 0 3.2a1.6 1.6 0 1 1 0 -3.2z " +
                "M18.5 10.4a1.6 1.6 0 1 1 0 3.2a1.6 1.6 0 1 1 0 -3.2z",
            filled = true,
        )
    }
    val Back by lazy { icon("back", "M14.5 5.5L8 12l6.5 6.5", stroke = 2f) }
    val ChevronRight by lazy { icon("chevron_right", "M9.5 6l6 6l-6 6") }
    val ChevronDown by lazy { icon("chevron_down", "M6 9.5l6 6l6 -6") }
    val Plus by lazy { icon("plus", "M12 5.5v13 M5.5 12h13", stroke = 2f) }
    val ArrowUp by lazy { icon("arrow_up", "M12 18.5V6 M6.5 11.5L12 6l5.5 5.5", stroke = 2.2f) }
    val ArrowDown by lazy { icon("arrow_down", "M12 5.5V18 M6.5 12.5L12 18l5.5 -5.5", stroke = 2f) }
    val Stop by lazy {
        icon(
            "stop",
            "M9 7.5h6a1.5 1.5 0 0 1 1.5 1.5v6a1.5 1.5 0 0 1 -1.5 1.5h-6a1.5 1.5 0 0 1 -1.5 -1.5v-6a1.5 1.5 0 0 1 1.5 -1.5z",
            filled = true,
        )
    }
    val Copy by lazy {
        icon(
            "copy",
            "M10.5 8.5h7a2 2 0 0 1 2 2v7a2 2 0 0 1 -2 2h-7a2 2 0 0 1 -2 -2v-7a2 2 0 0 1 2 -2z " +
                "M15.5 8.5V6.5a2 2 0 0 0 -2 -2h-7a2 2 0 0 0 -2 2v7a2 2 0 0 0 2 2h2",
        )
    }
    val Close by lazy { icon("close", "M6.5 6.5l11 11 M17.5 6.5l-11 11") }
    val Trash by lazy {
        icon(
            "trash",
            "M5 7h14 M9.5 7V5.5a1 1 0 0 1 1 -1h3a1 1 0 0 1 1 1V7 M6.5 7l0.8 11.2a2 2 0 0 0 2 1.8h5.4a2 2 0 0 0 2 -1.8L17.5 7",
        )
    }

    private fun icon(name: String, data: String, stroke: Float = 1.8f, filled: Boolean = false): ImageVector =
        ImageVector.Builder(name = name, defaultWidth = 24.dp, defaultHeight = 24.dp, viewportWidth = 24f, viewportHeight = 24f)
            .addPath(
                pathData = addPathNodes(data),
                fill = if (filled) SolidColor(Color.Black) else null,
                stroke = if (filled) null else SolidColor(Color.Black),
                strokeLineWidth = stroke,
                strokeLineCap = StrokeCap.Round,
                strokeLineJoin = StrokeJoin.Round,
            )
            .build()
}
