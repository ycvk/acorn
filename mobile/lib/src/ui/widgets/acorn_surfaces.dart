import 'package:flutter/material.dart';

import '../theme/acorn_theme.dart';

enum AcornSurfaceTone { lowest, low, base, high, highest, inverse }

class AcornSurface extends StatelessWidget {
  const AcornSurface({
    super.key,
    required this.child,
    this.tone = AcornSurfaceTone.base,
    this.padding = EdgeInsets.zero,
    this.margin = EdgeInsets.zero,
    this.radius = AcornRadius.md,
    this.border = false,
    this.elevation = 0,
    this.clipBehavior = Clip.antiAlias,
  });

  final Widget child;
  final AcornSurfaceTone tone;
  final EdgeInsetsGeometry padding;
  final EdgeInsetsGeometry margin;
  final double radius;
  final bool border;
  final double elevation;
  final Clip clipBehavior;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final side = border
        ? BorderSide(color: colors.outlineVariant)
        : BorderSide.none;
    return Padding(
      padding: margin,
      child: Material(
        color: _surfaceColor(colors, tone),
        elevation: elevation,
        shadowColor: colors.shadow.withValues(alpha: 0.18),
        surfaceTintColor: Colors.transparent,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(radius),
          side: side,
        ),
        clipBehavior: clipBehavior,
        child: Padding(padding: padding, child: child),
      ),
    );
  }
}

class AcornBottomSurface extends StatelessWidget {
  const AcornBottomSurface({super.key, required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    return SafeArea(
      top: false,
      child: Material(
        color: colors.surfaceContainer,
        elevation: 1,
        shadowColor: colors.shadow.withValues(alpha: 0.12),
        surfaceTintColor: colors.surfaceTint.withValues(alpha: 0.12),
        child: DecoratedBox(
          decoration: BoxDecoration(
            border: Border(
              top: BorderSide(
                color: colors.outlineVariant.withValues(alpha: 0.72),
                width: 1,
              ),
            ),
          ),
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 12, 16, 12),
            child: child,
          ),
        ),
      ),
    );
  }
}

class AcornTonalIcon extends StatelessWidget {
  const AcornTonalIcon({
    super.key,
    required this.icon,
    this.tone = AcornStatusTone.neutral,
    this.size = 40,
    this.iconSize = 22,
    this.radius = AcornRadius.md,
  });

  final IconData icon;
  final AcornStatusTone tone;
  final double size;
  final double iconSize;
  final double radius;

  @override
  Widget build(BuildContext context) {
    final status = AcornStatusColors.of(context).tone(tone);
    return SizedBox.square(
      dimension: size,
      child: Material(
        color: status.container,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(radius),
          side: BorderSide(color: status.color.withValues(alpha: 0.22)),
        ),
        clipBehavior: Clip.antiAlias,
        child: Icon(icon, color: status.onContainer, size: iconSize),
      ),
    );
  }
}

class AcornPageIntro extends StatelessWidget {
  const AcornPageIntro({
    super.key,
    required this.icon,
    required this.title,
    required this.body,
    this.trailing,
    this.tone = AcornStatusTone.neutral,
  });

  final IconData icon;
  final String title;
  final String body;
  final Widget? trailing;
  final AcornStatusTone tone;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    return AcornSurface(
      tone: AcornSurfaceTone.low,
      border: true,
      radius: AcornRadius.xl,
      margin: const EdgeInsets.fromLTRB(16, 8, 16, 14),
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 16),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          AcornTonalIcon(
            icon: icon,
            tone: tone,
            size: 44,
            iconSize: 24,
            radius: AcornRadius.lg,
          ),
          const SizedBox(width: 14),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(
                  title,
                  style: Theme.of(context).textTheme.titleMedium?.copyWith(
                    fontWeight: FontWeight.w800,
                  ),
                ),
                const SizedBox(height: 4),
                Text(
                  body,
                  style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: colors.onSurfaceVariant,
                  ),
                ),
              ],
            ),
          ),
          if (trailing != null) ...[const SizedBox(width: 12), trailing!],
        ],
      ),
    );
  }
}

class AcornCameraInstructionSurface extends StatelessWidget {
  const AcornCameraInstructionSurface({
    super.key,
    required this.child,
    this.error,
  });

  final Widget child;
  final String? error;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final statuses = AcornStatusColors.of(context);
    return SafeArea(
      child: AcornSurface(
        tone: AcornSurfaceTone.high,
        margin: const EdgeInsets.all(16),
        padding: const EdgeInsets.all(16),
        radius: AcornRadius.lg,
        border: true,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            DefaultTextStyle.merge(
              style: Theme.of(
                context,
              ).textTheme.bodyMedium?.copyWith(color: colors.onSurface),
              child: child,
            ),
            if (error != null) ...[
              const SizedBox(height: 8),
              Text(
                error!,
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                  color: statuses.error.color,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

Color _surfaceColor(ColorScheme colors, AcornSurfaceTone tone) {
  return switch (tone) {
    AcornSurfaceTone.lowest => colors.surfaceContainerLowest,
    AcornSurfaceTone.low => colors.surfaceContainerLow,
    AcornSurfaceTone.base => colors.surfaceContainer,
    AcornSurfaceTone.high => colors.surfaceContainerHigh,
    AcornSurfaceTone.highest => colors.surfaceContainerHighest,
    AcornSurfaceTone.inverse => colors.inverseSurface,
  };
}
