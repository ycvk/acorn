import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/providers.dart';
import '../../ui/theme/acorn_theme.dart';
import '../../ui/widgets/acorn_formatters.dart';
import '../../ui/widgets/acorn_status.dart';
import '../../ui/widgets/acorn_surfaces.dart';
import '../../ui/widgets/list_rows.dart';
import '../../ui/widgets/section_header.dart';

class SettingsScreen extends ConsumerWidget {
  const SettingsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final controller = ref.watch(acornControllerProvider);
    final profile = controller.profile;
    final system = controller.inbox?.system;
    final readiness = system?.runtimeReadiness.status ?? 'unknown';
    final readinessTone = readiness == 'ready'
        ? AcornStatusTone.success
        : AcornStatusTone.error;

    return Scaffold(
      appBar: AppBar(
        title: const Text('Settings'),
        actions: [
          IconButton.filledTonal(
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh),
            onPressed: controller.busy ? null : controller.refreshAll,
          ),
        ],
      ),
      body: ListView(
        children: [
          AcornPageIntro(
            icon: Icons.health_and_safety_outlined,
            title: 'Runtime status',
            body:
                system?.runtimeReadiness.reason ??
                'Remote runtime status from the authenticated server.',
            tone: readinessTone,
            trailing: StatusPill(label: readiness, tone: readinessTone),
          ),
          const SectionHeader(title: 'Connection'),
          AcornListRow(
            icon: Icons.dns_outlined,
            title: profile?.serverUrl ?? 'No server',
            subtitle: 'Authenticated /v1 remote client',
          ),
          AcornListRow(
            icon: Icons.phone_android_outlined,
            title: profile == null ? 'No device' : shortId(profile.deviceId),
            subtitle: 'Device bearer token stored in secure storage',
          ),
          const SectionHeader(title: 'Backend'),
          AcornListRow(
            icon: Icons.model_training_outlined,
            title: system?.model.name ?? 'Unknown model',
            subtitle: system?.runtimeReadiness.reason,
          ),
          AcornListRow(
            icon: Icons.folder_outlined,
            title: system?.workspaceRoot ?? 'Unknown workspace',
            subtitle: system == null
                ? null
                : '${system.summary.enabledToolCount}/${system.summary.toolCount} tools · ${system.summary.skillCount} skills',
          ),
          const SizedBox(height: 18),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: OutlinedButton.icon(
              onPressed: controller.disconnect,
              icon: const Icon(Icons.logout),
              label: const Text('Disconnect device'),
            ),
          ),
        ],
      ),
    );
  }
}
