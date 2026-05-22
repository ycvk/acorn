import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../api/acorn_api.dart';
import '../../core/providers.dart';
import '../../ui/theme/acorn_theme.dart';
import '../../ui/widgets/acorn_formatters.dart';
import '../../ui/widgets/acorn_status.dart';
import '../../ui/widgets/acorn_surfaces.dart';
import '../../ui/widgets/section_header.dart';

class SettingsScreen extends ConsumerWidget {
  const SettingsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final profile = ref.watch(
      connectionControllerProvider.select((controller) => controller.profile),
    );
    final system = ref.watch(
      inboxControllerProvider.select((controller) => controller.system),
    );
    final busy = ref.watch(
      inboxControllerProvider.select((controller) => controller.loading),
    );
    final inbox = ref.read(inboxControllerProvider);
    final readiness = system?.runtimeReadiness.status ?? 'unknown';
    final readinessTone = readiness == 'ready'
        ? AcornStatusTone.success
        : AcornStatusTone.error;

    return Scaffold(
      appBar: AppBar(
        title: const Text('Settings'),
        actions: [
          IconButton(
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh),
            onPressed: busy ? null : inbox.refresh,
          ),
        ],
      ),
      body: ListView(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 24),
        children: [
          _RuntimeStatusCard(
            system: system,
            readiness: readiness,
            tone: readinessTone,
          ),
          const SizedBox(height: 20),
          const SectionHeader(
            title: 'Connection',
            padding: EdgeInsets.fromLTRB(4, 4, 4, 8),
          ),
          _SettingsGroup(
            children: [
              _SettingsRow(
                icon: Icons.dns_outlined,
                title: profile?.serverUrl ?? 'No server',
                subtitle: 'Authenticated /v1 remote client',
              ),
              const Divider(height: 1),
              _SettingsRow(
                icon: Icons.phone_android_outlined,
                title: profile == null
                    ? 'No device'
                    : shortId(profile.deviceId),
                subtitle: 'Device bearer token stored in secure storage',
              ),
            ],
          ),
          const SizedBox(height: 20),
          const SectionHeader(
            title: 'Backend',
            padding: EdgeInsets.fromLTRB(4, 4, 4, 8),
          ),
          _SettingsGroup(
            children: [
              _SettingsRow(
                icon: Icons.model_training_outlined,
                title: system?.model.name ?? 'Unknown model',
                subtitle: system?.runtimeReadiness.reason,
              ),
              const Divider(height: 1),
              _SettingsRow(
                icon: Icons.folder_outlined,
                title: system?.workspaceRoot ?? 'Unknown workspace',
                subtitle: system == null
                    ? null
                    : '${system.summary.enabledToolCount}/${system.summary.toolCount} tools · ${system.summary.skillCount} skills',
              ),
            ],
          ),
          const SizedBox(height: 20),
          FilledButton.tonalIcon(
            onPressed: () => _disconnect(ref),
            icon: const Icon(Icons.logout),
            label: const Text('Disconnect device'),
          ),
        ],
      ),
    );
  }

  Future<void> _disconnect(WidgetRef ref) async {
    ref.read(inboxControllerProvider).clear();
    ref.read(threadsControllerProvider).clear();
    ref.read(approvalsControllerProvider).clear();
    ref.read(chatControllerProvider).clear();
    ref.read(runDetailControllerProvider).clear();
    ref.read(shellControllerProvider).reset();
    await ref.read(connectionControllerProvider).disconnect();
  }
}

class _RuntimeStatusCard extends StatelessWidget {
  const _RuntimeStatusCard({
    required this.system,
    required this.readiness,
    required this.tone,
  });

  final SystemStatus? system;
  final String readiness;
  final AcornStatusTone tone;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final reason =
        system?.runtimeReadiness.reason ??
        'Remote runtime status from the authenticated server.';

    return Card(
      margin: EdgeInsets.zero,
      elevation: 1,
      color: colors.surfaceContainerLow,
      surfaceTintColor: colors.surfaceTint.withValues(alpha: 0.12),
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(AcornRadius.lg),
        side: BorderSide(color: colors.outlineVariant),
      ),
      clipBehavior: Clip.antiAlias,
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            AcornTonalIcon(
              icon: Icons.health_and_safety_outlined,
              tone: tone,
              size: 44,
              iconSize: 24,
              radius: AcornRadius.sm,
            ),
            const SizedBox(width: 14),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    readiness == 'ready' ? 'Runtime ready' : 'Runtime status',
                    style: Theme.of(context).textTheme.titleMedium?.copyWith(
                      fontWeight: FontWeight.w800,
                    ),
                  ),
                  const SizedBox(height: 4),
                  Text(
                    reason,
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: colors.onSurfaceVariant,
                      height: 1.4,
                    ),
                  ),
                ],
              ),
            ),
            const SizedBox(width: 12),
            StatusPill(label: readiness, tone: tone),
          ],
        ),
      ),
    );
  }
}

class _SettingsGroup extends StatelessWidget {
  const _SettingsGroup({required this.children});

  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    return Card(
      margin: EdgeInsets.zero,
      elevation: 1,
      color: colors.surfaceContainerLow,
      surfaceTintColor: colors.surfaceTint.withValues(alpha: 0.10),
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(AcornRadius.lg),
        side: BorderSide(color: colors.outlineVariant),
      ),
      clipBehavior: Clip.antiAlias,
      child: Column(children: children),
    );
  }
}

class _SettingsRow extends StatelessWidget {
  const _SettingsRow({required this.icon, required this.title, this.subtitle});

  final IconData icon;
  final String title;
  final String? subtitle;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    return ListTile(
      contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
      leading: AcornTonalIcon(
        icon: icon,
        tone: AcornStatusTone.neutral,
        size: 40,
        iconSize: 20,
        radius: AcornRadius.sm,
      ),
      title: Text(title, maxLines: 1, overflow: TextOverflow.ellipsis),
      subtitle: subtitle == null
          ? null
          : Text(
              subtitle!,
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                color: colors.onSurfaceVariant,
                height: 1.35,
              ),
            ),
    );
  }
}
