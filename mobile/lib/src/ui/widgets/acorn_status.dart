import 'package:flutter/material.dart';

import '../theme/acorn_theme.dart';
import 'acorn_surfaces.dart';

class StatusDot extends StatelessWidget {
  const StatusDot({super.key, required this.tone, this.size = 9});

  final AcornStatusTone tone;
  final double size;

  @override
  Widget build(BuildContext context) {
    final status = AcornStatusColors.of(context).tone(tone);
    return SizedBox.square(
      dimension: size,
      child: Material(
        color: status.color,
        shape: const CircleBorder(),
        clipBehavior: Clip.antiAlias,
      ),
    );
  }
}

class StatusPill extends StatelessWidget {
  const StatusPill({
    super.key,
    required this.label,
    required this.tone,
    this.icon,
  });

  final String label;
  final AcornStatusTone tone;
  final IconData? icon;

  @override
  Widget build(BuildContext context) {
    final status = AcornStatusColors.of(context).tone(tone);
    return Material(
      color: status.container,
      shape: StadiumBorder(
        side: BorderSide(color: status.color.withValues(alpha: 0.42)),
      ),
      clipBehavior: Clip.antiAlias,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (icon == null)
              StatusDot(tone: tone, size: 7)
            else
              Icon(icon, size: 16, color: status.color),
            const SizedBox(width: 7),
            Text(
              label,
              style: Theme.of(context).textTheme.labelMedium?.copyWith(
                color: status.onContainer,
                fontWeight: FontWeight.w800,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class InlineStatusLabel extends StatelessWidget {
  const InlineStatusLabel({super.key, required this.label, required this.tone});

  final String label;
  final AcornStatusTone tone;

  @override
  Widget build(BuildContext context) {
    final status = AcornStatusColors.of(context).tone(tone);
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        StatusDot(tone: tone, size: 7),
        const SizedBox(width: 6),
        Text(
          label,
          style: Theme.of(context).textTheme.labelSmall?.copyWith(
            color: status.onContainer,
            fontWeight: FontWeight.w700,
          ),
        ),
      ],
    );
  }
}

class ErrorBanner extends StatelessWidget {
  const ErrorBanner({super.key, required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    final status = AcornStatusColors.of(context).error;
    return AcornSurface(
      tone: AcornSurfaceTone.low,
      margin: const EdgeInsets.fromLTRB(16, 8, 16, 8),
      padding: const EdgeInsets.all(14),
      radius: AcornRadius.lg,
      border: true,
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(Icons.error_outline, color: status.color, size: 20),
          const SizedBox(width: 10),
          Expanded(
            child: Text(
              message,
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                color: status.color,
                fontWeight: FontWeight.w500,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class InfoBanner extends StatelessWidget {
  const InfoBanner({super.key, required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    final status = AcornStatusColors.of(context).info;
    return AcornSurface(
      tone: AcornSurfaceTone.low,
      margin: const EdgeInsets.fromLTRB(16, 8, 16, 8),
      padding: const EdgeInsets.all(14),
      radius: AcornRadius.lg,
      border: true,
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(Icons.sync, color: status.color, size: 20),
          const SizedBox(width: 10),
          Expanded(
            child: Text(
              message,
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                color: status.color,
                fontWeight: FontWeight.w500,
              ),
            ),
          ),
        ],
      ),
    );
  }
}
