import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../theme/acorn_theme.dart';
import 'acorn_status.dart';
import 'acorn_surfaces.dart';

class AcornMessageBubble extends StatelessWidget {
  const AcornMessageBubble({
    super.key,
    required this.outbound,
    required this.child,
    this.footer,
  });

  final bool outbound;
  final Widget child;
  final Widget? footer;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final maxWidth = math.min(MediaQuery.sizeOf(context).width * 0.78, 640.0);
    final radius = BorderRadius.only(
      topLeft: const Radius.circular(AcornRadius.xl),
      topRight: const Radius.circular(AcornRadius.xl),
      bottomLeft: Radius.circular(outbound ? AcornRadius.xl : AcornRadius.lg),
      bottomRight: Radius.circular(outbound ? AcornRadius.lg : AcornRadius.xl),
    );
    final background = outbound
        ? colors.primaryContainer
        : colors.surfaceContainerHigh;
    final foreground = outbound ? colors.onPrimaryContainer : colors.onSurface;

    return Align(
      alignment: outbound ? Alignment.centerRight : Alignment.centerLeft,
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 5),
        child: ConstrainedBox(
          constraints: BoxConstraints(maxWidth: maxWidth),
          child: Material(
            color: background,
            surfaceTintColor: Colors.transparent,
            shape: RoundedRectangleBorder(
              borderRadius: radius,
              side: BorderSide(
                color: outbound
                    ? colors.primary.withValues(alpha: 0.24)
                    : colors.outlineVariant.withValues(alpha: 0.82),
              ),
            ),
            clipBehavior: Clip.antiAlias,
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 15, vertical: 12),
              child: IconTheme.merge(
                data: IconThemeData(color: foreground),
                child: DefaultTextStyle.merge(
                  style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                    color: foreground,
                    fontSize: 15,
                    height: 1.42,
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      child,
                      if (footer != null) ...[
                        const SizedBox(height: 8),
                        footer!,
                      ],
                    ],
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class AcornActivityRow extends StatelessWidget {
  const AcornActivityRow({
    super.key,
    required this.title,
    required this.timestamp,
    this.detail,
    this.icon = Icons.bolt_outlined,
  });

  final String title;
  final String timestamp;
  final String? detail;
  final IconData icon;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    return AcornSurface(
      tone: AcornSurfaceTone.low,
      border: true,
      radius: AcornRadius.lg,
      margin: const EdgeInsets.symmetric(vertical: 4),
      padding: const EdgeInsets.fromLTRB(12, 10, 12, 10),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          AcornTonalIcon(
            icon: icon,
            tone: AcornStatusTone.info,
            size: 34,
            iconSize: 18,
            radius: AcornRadius.md,
          ),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  title,
                  style: Theme.of(
                    context,
                  ).textTheme.labelLarge?.copyWith(fontWeight: FontWeight.w800),
                ),
                if (detail != null) ...[
                  const SizedBox(height: 2),
                  Text(
                    detail!,
                    maxLines: 3,
                    overflow: TextOverflow.ellipsis,
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: colors.onSurfaceVariant,
                    ),
                  ),
                ],
              ],
            ),
          ),
          const SizedBox(width: 8),
          Text(
            timestamp,
            style: Theme.of(
              context,
            ).textTheme.labelSmall?.copyWith(color: colors.onSurfaceVariant),
          ),
        ],
      ),
    );
  }
}

class AcornMessageStatusFooter extends StatelessWidget {
  const AcornMessageStatusFooter({
    super.key,
    required this.label,
    required this.tone,
    required this.foregroundColor,
  });

  final String label;
  final AcornStatusTone tone;
  final Color foregroundColor;

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        StatusDot(tone: tone, size: 7),
        const SizedBox(width: 6),
        Text(
          label,
          style: Theme.of(context).textTheme.labelSmall?.copyWith(
            color: foregroundColor.withValues(alpha: 0.84),
            fontWeight: FontWeight.w700,
          ),
        ),
      ],
    );
  }
}

class AcornThinkingSection extends StatefulWidget {
  const AcornThinkingSection({super.key, required this.reasoning});

  final String reasoning;

  @override
  State<AcornThinkingSection> createState() => _AcornThinkingSectionState();
}

class _AcornThinkingSectionState extends State<AcornThinkingSection> {
  bool _expanded = false;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final textTheme = Theme.of(context).textTheme;
    final trimmed = widget.reasoning.trim();

    return AcornSurface(
      tone: AcornSurfaceTone.base,
      border: true,
      radius: AcornRadius.md,
      clipBehavior: Clip.antiAlias,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          InkWell(
            onTap: () => setState(() => _expanded = !_expanded),
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
              child: Row(
                children: [
                  Icon(
                    Icons.lightbulb_outline,
                    size: 18,
                    color: colors.onSurfaceVariant,
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      'Thinking',
                      style: textTheme.labelLarge?.copyWith(
                        color: colors.onSurfaceVariant,
                        fontWeight: FontWeight.w800,
                      ),
                    ),
                  ),
                  Icon(
                    _expanded ? Icons.expand_less : Icons.expand_more,
                    size: 20,
                    color: colors.onSurfaceVariant,
                  ),
                ],
              ),
            ),
          ),
          AnimatedSize(
            duration: const Duration(milliseconds: 180),
            alignment: Alignment.topCenter,
            child: _expanded
                ? Padding(
                    padding: const EdgeInsets.fromLTRB(10, 0, 10, 10),
                    child: SelectableText(
                      trimmed,
                      style: textTheme.bodySmall?.copyWith(
                        color: colors.onSurfaceVariant,
                        height: 1.38,
                      ),
                    ),
                  )
                : const SizedBox.shrink(),
          ),
        ],
      ),
    );
  }
}

class AcornTypingIndicator extends StatefulWidget {
  const AcornTypingIndicator({super.key});

  @override
  State<AcornTypingIndicator> createState() => _AcornTypingIndicatorState();
}

class _AcornTypingIndicatorState extends State<AcornTypingIndicator>
    with SingleTickerProviderStateMixin {
  late final AnimationController _controller;

  @override
  void initState() {
    super.initState();
    _controller = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 900),
    )..repeat();
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final color = Theme.of(context).colorScheme.onSurfaceVariant;
    return AnimatedBuilder(
      animation: _controller,
      builder: (context, _) {
        return Row(
          mainAxisSize: MainAxisSize.min,
          children: List.generate(3, (index) {
            final phase = (_controller.value + index / 3) % 1;
            return Padding(
              padding: const EdgeInsets.only(right: 4),
              child: SizedBox.square(
                dimension: 6,
                child: Material(
                  color: color.withValues(alpha: 0.35 + phase * 0.55),
                  shape: const CircleBorder(),
                ),
              ),
            );
          }),
        );
      },
    );
  }
}
