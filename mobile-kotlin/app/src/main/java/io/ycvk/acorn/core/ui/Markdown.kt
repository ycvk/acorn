package io.ycvk.acorn.core.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.isSpecified
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.LinkAnnotation
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextLayoutResult
import androidx.compose.ui.text.TextLinkStyles
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.withLink
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.em
import androidx.compose.ui.unit.sp
import io.ycvk.acorn.core.theme.AcornColors
import io.ycvk.acorn.core.theme.AcornShapes
import io.ycvk.acorn.core.theme.AcornTheme
import org.commonmark.ext.autolink.AutolinkExtension
import org.commonmark.ext.gfm.strikethrough.Strikethrough
import org.commonmark.ext.gfm.strikethrough.StrikethroughExtension
import org.commonmark.ext.gfm.tables.TableBlock
import org.commonmark.ext.gfm.tables.TableCell
import org.commonmark.ext.gfm.tables.TableHead
import org.commonmark.ext.gfm.tables.TablesExtension
import org.commonmark.node.BlockQuote
import org.commonmark.node.BulletList
import org.commonmark.node.Code
import org.commonmark.node.Emphasis
import org.commonmark.node.FencedCodeBlock
import org.commonmark.node.HardLineBreak
import org.commonmark.node.Heading
import org.commonmark.node.HtmlBlock
import org.commonmark.node.HtmlInline
import org.commonmark.node.Image
import org.commonmark.node.IndentedCodeBlock
import org.commonmark.node.Link
import org.commonmark.node.ListItem
import org.commonmark.node.Node
import org.commonmark.node.OrderedList
import org.commonmark.node.Paragraph
import org.commonmark.node.SoftLineBreak
import org.commonmark.node.StrongEmphasis
import org.commonmark.node.ThematicBreak
import org.commonmark.parser.Parser

private val parser: Parser = Parser.builder()
    .extensions(listOf(TablesExtension.create(), StrikethroughExtension.create(), AutolinkExtension.create()))
    .build()

/** Renders markdown with Acorn's reading typography; links open in the browser. */
@Composable
fun Markdown(markdown: String, modifier: Modifier = Modifier, style: TextStyle = AcornTheme.type.reading) {
    val document = remember(markdown) { parser.parse(markdown) }
    val resolved = if (style.color.isSpecified) style else style.copy(color = AcornTheme.colors.ink)
    Column(modifier = modifier, verticalArrangement = Arrangement.spacedBy(blockGap)) {
        document.childNodes().forEach { Block(it, resolved) }
    }
}

private val blockGap = 12.dp

private fun Node.childNodes(): List<Node> = generateSequence(firstChild) { it.next }.toList()

@Composable
private fun Block(node: Node, style: TextStyle) {
    val colors = AcornTheme.colors
    when (node) {
        is Heading -> Inline(node, headingStyle(node.level, style), Modifier.padding(top = if (node.level <= 2) 6.dp else 2.dp))
        is Paragraph -> Inline(node, style)
        is BulletList -> ListBlock(node.childNodes(), style) { "•" }
        is OrderedList -> {
            val start = node.markerStartNumber ?: 1
            ListBlock(node.childNodes(), style) { index -> "${start + index}." }
        }
        is BlockQuote -> Row(modifier = Modifier.height(IntrinsicSize.Min)) {
            Box(
                Modifier
                    .width(3.dp)
                    .fillMaxHeight()
                    .background(colors.lineStrong, RoundedCornerShape(2.dp)),
            )
            Spacer(Modifier.width(12.dp))
            Column(verticalArrangement = Arrangement.spacedBy(blockGap)) {
                node.childNodes().forEach { Block(it, style.copy(color = colors.inkSoft)) }
            }
        }
        is FencedCodeBlock -> CodeBlock(node.literal.trimEnd('\n'))
        is IndentedCodeBlock -> CodeBlock(node.literal.trimEnd('\n'))
        is ThematicBreak -> Hairline(Modifier.padding(vertical = 4.dp))
        is TableBlock -> Table(node, style)
        is HtmlBlock -> Text(node.literal.trim(), style = style)
        else -> node.childNodes().forEach { Block(it, style) }
    }
}

private fun headingStyle(level: Int, base: TextStyle): TextStyle {
    val (size, line) = when (level) {
        1 -> 21 to 30
        2 -> 19 to 28
        else -> 17 to 26
    }
    return base.copy(
        fontFamily = FontFamily.SansSerif,
        fontWeight = FontWeight.SemiBold,
        fontSize = size.sp,
        lineHeight = line.sp,
    )
}

@Composable
private fun ListBlock(items: List<Node>, style: TextStyle, marker: (Int) -> String) {
    val colors = AcornTheme.colors
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        items.forEachIndexed { index, item ->
            Row {
                Text(
                    marker(index),
                    style = style.copy(color = colors.inkFaint, fontFamily = FontFamily.SansSerif),
                    modifier = Modifier.widthIn(min = 20.dp),
                )
                Spacer(Modifier.width(4.dp))
                Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    (item as? ListItem)?.childNodes()?.forEach { Block(it, style) }
                }
            }
        }
    }
}

@Composable
private fun CodeBlock(code: String) {
    val colors = AcornTheme.colors
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .clip(AcornShapes.small)
            .background(colors.surfaceMuted)
            .horizontalScroll(rememberScrollState())
            .padding(horizontal = 14.dp, vertical = 12.dp),
    ) {
        Text(code, style = AcornTheme.type.mono, color = colors.ink, softWrap = false)
    }
}

@Composable
private fun Table(table: TableBlock, style: TextStyle) {
    val colors = AcornTheme.colors
    val cellStyle = style.copy(fontFamily = FontFamily.SansSerif, fontSize = 14.sp, lineHeight = 21.sp)
    val rows = table.childNodes().flatMap { section -> section.childNodes().map { row -> (section is TableHead) to row } }
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(AcornShapes.small)
            .border(0.5.dp, colors.lineStrong, AcornShapes.small),
    ) {
        rows.forEachIndexed { index, (header, row) ->
            if (index > 0) Hairline()
            Row(modifier = Modifier.background(if (header) colors.surfaceMuted else Color.Transparent)) {
                row.childNodes().forEach { cell ->
                    Inline(
                        cell as TableCell,
                        if (header) cellStyle.copy(fontWeight = FontWeight.Medium) else cellStyle,
                        Modifier
                            .weight(1f)
                            .padding(horizontal = 10.dp, vertical = 8.dp),
                    )
                }
            }
        }
    }
}

private class InlineText(val text: AnnotatedString, val code: List<IntRange>)

@Composable
private fun Inline(node: Node, style: TextStyle, modifier: Modifier = Modifier) {
    val colors = AcornTheme.colors
    val built = remember(node, colors) { buildInline(node, colors) }
    var layout by remember { mutableStateOf<TextLayoutResult?>(null) }
    val codeFill = colors.surfaceMuted
    Text(
        built.text,
        style = style,
        onTextLayout = { layout = it },
        modifier = modifier.drawBehind {
            val result = layout ?: return@drawBehind
            drawCodeFills(result, built.code, codeFill, style.fontSize.toPx() * CODE_SCALE)
        },
    )
}

private const val CODE_SCALE = 0.8f
private const val THIN_SPACE = '\u2009'

private fun buildInline(node: Node, colors: AcornColors): InlineText {
    val code = mutableListOf<IntRange>()
    val builder = AnnotatedString.Builder()
    var last = ' '
    fun append(text: String) {
        if (text.isEmpty()) return
        builder.append(text)
        last = text.last()
    }
    fun walk(parent: Node) {
        var child = parent.firstChild
        while (child != null) {
            when (child) {
                is org.commonmark.node.Text -> append(child.literal)
                // A soft break joins words: a space between Latin words, nothing between CJK characters.
                is SoftLineBreak -> if (!last.isCjk()) append(" ")
                is HardLineBreak -> append("\n")
                is Emphasis -> builder.withStyle(SpanStyle(fontStyle = FontStyle.Italic)) { walk(child) }
                is StrongEmphasis -> builder.withStyle(SpanStyle(fontWeight = FontWeight.SemiBold)) { walk(child) }
                is Strikethrough -> builder.withStyle(SpanStyle(textDecoration = TextDecoration.LineThrough)) { walk(child) }
                is Code -> {
                    append(THIN_SPACE.toString())
                    val start = builder.length
                    builder.withStyle(SpanStyle(fontFamily = FontFamily.Monospace, fontSize = CODE_SCALE.em, color = colors.accentInk)) {
                        append(child.literal)
                    }
                    code += start until builder.length
                    append(THIN_SPACE.toString())
                }
                is Link -> builder.withLink(
                    LinkAnnotation.Url(
                        child.destination,
                        TextLinkStyles(SpanStyle(color = colors.accent, textDecoration = TextDecoration.Underline)),
                    ),
                ) { walk(child) }
                is Image -> builder.withLink(
                    LinkAnnotation.Url(child.destination, TextLinkStyles(SpanStyle(color = colors.accent))),
                ) { append("[图片] "); walk(child) }
                is HtmlInline -> append(child.literal)
                else -> walk(child)
            }
            child = child.next
        }
    }
    walk(node)
    return InlineText(builder.toAnnotatedString(), code)
}

private fun Char.isCjk(): Boolean {
    val block = Character.UnicodeBlock.of(this)
    return block == Character.UnicodeBlock.CJK_UNIFIED_IDEOGRAPHS ||
        block == Character.UnicodeBlock.CJK_SYMBOLS_AND_PUNCTUATION ||
        block == Character.UnicodeBlock.HALFWIDTH_AND_FULLWIDTH_FORMS ||
        block == Character.UnicodeBlock.HIRAGANA ||
        block == Character.UnicodeBlock.KATAKANA
}

/** Rounded fills behind inline code, sized to the glyphs instead of the tall reading line. */
private fun DrawScope.drawCodeFills(layout: TextLayoutResult, ranges: List<IntRange>, color: Color, codePx: Float) {
    val pad = 3.dp.toPx()
    val radius = CornerRadius(4.dp.toPx())
    for (range in ranges) {
        if (range.isEmpty()) continue
        for (line in layout.getLineForOffset(range.first)..layout.getLineForOffset(range.last)) {
            val start = maxOf(range.first, layout.getLineStart(line))
            val end = minOf(range.last, layout.getLineEnd(line, visibleEnd = true) - 1)
            if (start > end) continue
            val left = layout.getBoundingBox(start).left
            val right = layout.getBoundingBox(end).right
            val baseline = layout.getLineBaseline(line)
            drawRoundRect(
                color = color,
                topLeft = Offset(left - pad, baseline - codePx * 0.95f),
                size = Size(right - left + pad * 2, codePx * 1.3f),
                cornerRadius = radius,
            )
        }
    }
}
