import 'package:flutter/material.dart';

import '../theme/acorn_theme.dart';
import 'acorn_surfaces.dart';

class AcornEmptyState extends StatelessWidget {
  const AcornEmptyState({
    super.key,
    required this.icon,
    required this.title,
    required this.body,
    this.action,
    this.tone = AcornStatusTone.neutral,
  });

  final IconData icon;
  final String title;
  final String body;
  final Widget? action;
  final AcornStatusTone tone;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 420),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              AcornTonalIcon(
                icon: icon,
                tone: tone,
                size: 72,
                iconSize: 34,
                radius: AcornRadius.xl,
              ),
              const SizedBox(height: 20),
              Text(
                title,
                textAlign: TextAlign.center,
                style: Theme.of(
                  context,
                ).textTheme.titleLarge?.copyWith(fontWeight: FontWeight.w800),
              ),
              const SizedBox(height: 8),
              Text(
                body,
                textAlign: TextAlign.center,
                style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                  color: colors.onSurfaceVariant,
                  height: 1.45,
                ),
              ),
              if (action != null) ...[const SizedBox(height: 22), action!],
            ],
          ),
        ),
      ),
    );
  }
}
