import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';
import 'package:markdown/markdown.dart' as md;
import 'package:url_launcher/url_launcher.dart';

import '../../ui/theme/acorn_theme.dart';
import '../../ui/widgets/acorn_surfaces.dart';

const assistantMarkdownLongViewportKey = ValueKey(
  'assistant-markdown-long-viewport',
);

class AssistantMarkdown extends StatefulWidget {
  const AssistantMarkdown({
    super.key,
    required this.text,
    required this.textColor,
  });

  final String text;
  final Color textColor;

  @override
  State<AssistantMarkdown> createState() => _AssistantMarkdownState();
}

class _AssistantMarkdownState extends State<AssistantMarkdown> {
  static const int _longMessageThreshold = 2800;
  static const double _longMessageMinHeight = 280;
  static const double _longMessageMaxHeight = 520;

  final ScrollController _longMessageScrollController = ScrollController();

  @override
  void dispose() {
    _longMessageScrollController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colors = theme.colorScheme;
    final baseText = theme.textTheme.bodyMedium?.copyWith(
      color: widget.textColor,
      fontSize: 15,
      height: 1.42,
    );
    final headingText = theme.textTheme.titleMedium?.copyWith(
      color: widget.textColor,
      fontSize: 16,
      height: 1.25,
      fontWeight: FontWeight.w800,
    );
    final codeSurface = colors.surfaceContainerHighest.withValues(alpha: 0.76);
    final codeBorder = colors.outlineVariant.withValues(alpha: 0.72);
    final styleSheet = MarkdownStyleSheet.fromTheme(theme).copyWith(
      p: baseText,
      pPadding: EdgeInsets.zero,
      a: baseText?.copyWith(
        color: colors.primary,
        fontWeight: FontWeight.w700,
        decoration: TextDecoration.underline,
        decorationColor: colors.primary.withValues(alpha: 0.52),
      ),
      strong: const TextStyle(fontWeight: FontWeight.w800),
      em: const TextStyle(fontStyle: FontStyle.italic),
      h1: headingText,
      h1Padding: const EdgeInsets.only(bottom: 6),
      h2: headingText,
      h2Padding: const EdgeInsets.only(bottom: 6),
      h3: headingText?.copyWith(fontSize: 15),
      h3Padding: const EdgeInsets.only(bottom: 4),
      blockSpacing: 8,
      listIndent: 20,
      listBullet: baseText,
      listBulletPadding: const EdgeInsets.only(right: 5),
      code: baseText?.copyWith(
        fontFamily: 'monospace',
        fontSize: 13,
        color: widget.textColor,
        backgroundColor: codeSurface,
      ),
      codeblockPadding: EdgeInsets.zero,
      codeblockDecoration: const BoxDecoration(),
      blockquote: baseText?.copyWith(
        color: widget.textColor.withValues(alpha: 0.86),
        fontStyle: FontStyle.italic,
      ),
      blockquotePadding: const EdgeInsets.fromLTRB(10, 8, 10, 8),
      blockquoteDecoration: BoxDecoration(
        color: colors.surfaceContainerHighest.withValues(alpha: 0.48),
        borderRadius: BorderRadius.circular(AcornRadius.sm),
        border: Border.all(color: codeBorder),
      ),
      tableHead: baseText?.copyWith(fontWeight: FontWeight.w800),
      tableBody: baseText?.copyWith(fontSize: 13),
      tableBorder: TableBorder.all(color: codeBorder),
      tableCellsPadding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
      horizontalRuleDecoration: BoxDecoration(
        border: Border(top: BorderSide(color: codeBorder)),
      ),
    );
    final builders = <String, MarkdownElementBuilder>{
      'pre': _CodeBlockBuilder(
        textColor: widget.textColor,
        borderColor: codeBorder,
        surfaceColor: codeSurface,
      ),
    };

    if (widget.text.length >= _longMessageThreshold) {
      final height = _longMessageHeight(context);
      return SizedBox(
        key: assistantMarkdownLongViewportKey,
        height: height,
        child: Scrollbar(
          controller: _longMessageScrollController,
          child: Markdown(
            data: widget.text,
            controller: _longMessageScrollController,
            padding: EdgeInsets.zero,
            softLineBreak: true,
            onTapLink: (_, href, _) {
              unawaited(_openLink(context, href));
            },
            styleSheet: styleSheet,
            builders: builders,
          ),
        ),
      );
    }

    return MarkdownBody(
      data: widget.text,
      softLineBreak: true,
      onTapLink: (_, href, _) {
        unawaited(_openLink(context, href));
      },
      styleSheet: styleSheet,
      builders: builders,
    );
  }

  static double _longMessageHeight(BuildContext context) {
    final viewportHeight = MediaQuery.sizeOf(context).height;
    return (viewportHeight * 0.54)
        .clamp(_longMessageMinHeight, _longMessageMaxHeight)
        .toDouble();
  }

  static Future<void> _openLink(BuildContext context, String? href) async {
    final uri = Uri.tryParse(href ?? '');
    if (uri == null || (uri.scheme != 'http' && uri.scheme != 'https')) {
      _showLinkError(context, 'Unsupported link');
      return;
    }

    final launched = await launchUrl(uri, mode: LaunchMode.externalApplication);
    if (!launched && context.mounted) {
      _showLinkError(context, 'Could not open link');
    }
  }

  static void _showLinkError(BuildContext context, String message) {
    if (!context.mounted) {
      return;
    }
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(SnackBar(content: Text(message)));
  }
}

class _CodeBlockBuilder extends MarkdownElementBuilder {
  _CodeBlockBuilder({
    required this.textColor,
    required this.borderColor,
    required this.surfaceColor,
  });

  final Color textColor;
  final Color borderColor;
  final Color surfaceColor;

  @override
  bool isBlockElement() => true;

  @override
  Widget? visitElementAfterWithContext(
    BuildContext context,
    md.Element element,
    TextStyle? preferredStyle,
    TextStyle? parentStyle,
  ) {
    final code = _stripSingleTrailingNewline(element.textContent);
    return _CodeBlock(
      code: code,
      language: _languageFromCodeElement(element),
      textColor: textColor,
      borderColor: borderColor,
      surfaceColor: surfaceColor,
    );
  }
}

class _CodeBlock extends StatelessWidget {
  const _CodeBlock({
    required this.code,
    required this.language,
    required this.textColor,
    required this.borderColor,
    required this.surfaceColor,
  });

  final String code;
  final String? language;
  final Color textColor;
  final Color borderColor;
  final Color surfaceColor;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final label = language == null ? 'Code' : language!.toUpperCase();
    return AcornSurface(
      margin: const EdgeInsets.symmetric(vertical: 4),
      tone: AcornSurfaceTone.highest,
      radius: AcornRadius.sm,
      border: true,
      padding: EdgeInsets.zero,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        mainAxisSize: MainAxisSize.min,
        children: [
          SizedBox(
            height: 34,
            child: Material(
              color: surfaceColor,
              child: DecoratedBox(
                decoration: BoxDecoration(
                  border: Border(bottom: BorderSide(color: borderColor)),
                ),
                child: Padding(
                  padding: const EdgeInsets.only(left: 10, right: 2),
                  child: Row(
                    children: [
                      Expanded(
                        child: Text(
                          label,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: Theme.of(context).textTheme.labelSmall
                              ?.copyWith(
                                color: textColor.withValues(alpha: 0.68),
                                fontWeight: FontWeight.w800,
                              ),
                        ),
                      ),
                      IconButton(
                        tooltip: 'Copy code',
                        icon: const Icon(Icons.content_copy_rounded, size: 16),
                        color: colors.onSurfaceVariant,
                        padding: EdgeInsets.zero,
                        constraints: const BoxConstraints.tightFor(
                          width: 32,
                          height: 32,
                        ),
                        onPressed: () {
                          unawaited(_copyCode(context, code));
                        },
                      ),
                    ],
                  ),
                ),
              ),
            ),
          ),
          Material(
            color: colors.surfaceContainerHigh.withValues(alpha: 0.38),
            child: SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              padding: const EdgeInsets.fromLTRB(10, 10, 10, 12),
              child: Text(
                code.isEmpty ? ' ' : code,
                style: TextStyle(
                  color: textColor,
                  fontFamily: 'monospace',
                  fontSize: 13,
                  height: 1.42,
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }

  static Future<void> _copyCode(BuildContext context, String code) async {
    await Clipboard.setData(ClipboardData(text: code));
    if (!context.mounted) {
      return;
    }
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(const SnackBar(content: Text('Copied code')));
  }
}

String _stripSingleTrailingNewline(String value) {
  return value.endsWith('\n') ? value.substring(0, value.length - 1) : value;
}

String? _languageFromCodeElement(md.Element preElement) {
  final children = preElement.children;
  if (children == null) {
    return null;
  }
  for (final child in children) {
    if (child is! md.Element || child.tag != 'code') {
      continue;
    }
    final className = child.attributes['class'];
    if (className == null || className.isEmpty) {
      return null;
    }
    for (final token in className.split(' ')) {
      if (token.startsWith('language-') && token.length > 'language-'.length) {
        return token.substring('language-'.length);
      }
    }
  }
  return null;
}
