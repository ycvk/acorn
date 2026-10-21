package io.ycvk.acorn.feature.views

import io.ycvk.acorn.api.models.View
import io.ycvk.acorn.api.models.ViewActionSent

/** A component of a view: its id, its type from the catalog and the rest of what the server sent. */
data class ViewComponent(val id: String, val type: String, val props: Map<String, Any?>)

/**
 * A view ready to draw: its components by id and the data their paths point
 * into. The server checked the view against the catalog before storing it, so
 * a value that does not resolve here draws as nothing.
 */
class ViewTree(val title: String, private val components: Map<String, ViewComponent>, private val data: Any?) {
    val root: ViewComponent? get() = components["root"]

    fun component(id: String?): ViewComponent? = id?.let(components::get)

    fun children(component: ViewComponent): List<ViewComponent> =
        (component.props["children"] as? List<*>).orEmpty().mapNotNull { component(it as? String) }

    /**
     * What a property holds: the value written out, or what its
     * `{"path": ...}` finds. A path without a leading slash is relative to the
     * innermost List item in [scopes].
     */
    fun resolve(value: Any?, scopes: List<Any?>): Any? {
        val path = bindingPath(value) ?: return value
        return when {
            path.startsWith("/") -> lookup(data, path)
            scopes.isEmpty() -> null
            path.isEmpty() -> scopes.last()
            else -> lookup(scopes.last(), "/$path")
        }
    }

    fun text(component: ViewComponent, name: String, scopes: List<Any?>): String? = displayText(resolve(component.props[name], scopes))

    fun list(component: ViewComponent, name: String, scopes: List<Any?>): List<Any?> = resolve(component.props[name], scopes) as? List<*> ?: emptyList()
}

fun viewTree(view: View): ViewTree {
    val components = view.components.mapNotNull { raw ->
        val id = raw["id"] as? String ?: return@mapNotNull null
        val type = raw["component"] as? String ?: return@mapNotNull null
        ViewComponent(id, type, raw)
    }.associateBy { it.id }
    return ViewTree(view.title, components, view.`data`)
}

/** The path of a `{"path": "..."}` value. */
fun bindingPath(value: Any?): String? {
    val obj = value as? Map<*, *> ?: return null
    return if (obj.size == 1) obj["path"] as? String else null
}

/** Follows an RFC 6901 JSON Pointer through decoded JSON; null when it finds nothing. */
fun lookup(root: Any?, pointer: String): Any? {
    if (pointer.isEmpty()) return root
    if (!pointer.startsWith("/")) return null
    var current = root
    for (raw in pointer.substring(1).split("/")) {
        val token = raw.replace("~1", "/").replace("~0", "~")
        current = when (current) {
            is Map<*, *> -> if (current.containsKey(token)) current[token] else return null
            is List<*> -> {
                val index = token.toIntOrNull() ?: return null
                if (index !in current.indices || (token.length > 1 && token.startsWith("0"))) return null
                current[index]
            }
            else -> return null
        }
    }
    return current
}

/** A string or number as text; JSON numbers arrive as doubles, and whole ones read without ".0". */
fun displayText(value: Any?): String? = when (value) {
    is String -> value
    is Double -> if (value % 1.0 == 0.0 && kotlin.math.abs(value) < 1e15) value.toLong().toString() else value.toString()
    is Number -> value.toString()
    else -> null
}

/** A chart point: a label and its value. */
data class ChartPoint(val label: String, val value: Double)

fun chartPoints(values: List<Any?>): List<ChartPoint> = values.mapNotNull { point ->
    val map = point as? Map<*, *> ?: return@mapNotNull null
    val label = map["label"] as? String ?: return@mapNotNull null
    val value = (map["value"] as? Number)?.toDouble() ?: return@mapNotNull null
    ChartPoint(label, value)
}

/** The rows of a table as text cells; a null cell is empty. */
fun tableRows(values: List<Any?>, columns: Int): List<List<String>> = values.mapNotNull { row ->
    val cells = row as? List<*> ?: return@mapNotNull null
    List(columns) { index -> displayText(cells.getOrNull(index)).orEmpty() }
}

/** What the owner pressed or chose in a view: the component, the index of the item in each List it is drawn for, and the chosen option ids. */
data class ViewActionPress(val viewId: String, val componentId: String, val scope: List<Int>, val selected: List<String> = emptyList()) {
    /** Tells this action apart from every other in the thread. */
    val id: String get() = "$viewId:${actionKey(componentId, scope)}"
}

/** The server's key for an action: the component id, then the List item indices. */
fun actionKey(componentId: String, scope: List<Int>): String = (listOf(componentId) + scope).joinToString("/")

/** The actions the owner already sent from [view], by key. */
fun sentActions(view: View): Map<String, ViewActionSent> = view.actions.orEmpty().associateBy { actionKey(it.componentId, it.scope) }

/** A ChoicePicker option. */
data class ChoiceOption(val id: String, val label: String)

fun choiceOptions(values: List<Any?>): List<ChoiceOption> = values.mapNotNull { option ->
    val map = option as? Map<*, *> ?: return@mapNotNull null
    val id = map["id"] as? String ?: return@mapNotNull null
    val label = map["label"] as? String ?: return@mapNotNull null
    ChoiceOption(id, label)
}

/** The picked option ids after tapping [id]: a tap on a picked one drops it, and one at a time unless [multiple]. */
fun toggleChoice(picked: List<String>, id: String, multiple: Boolean): List<String> = when {
    id in picked -> picked - id
    multiple -> picked + id
    else -> listOf(id)
}
