import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../api/acorn_api.dart';
import '../../core/providers.dart';
import '../../ui/theme/acorn_theme.dart';
import '../../ui/widgets/acorn_formatters.dart';
import '../../ui/widgets/acorn_status.dart';
import '../../ui/widgets/acorn_surfaces.dart';
import '../../ui/widgets/empty_state.dart';
import '../../ui/widgets/list_rows.dart';
import '../../ui/widgets/section_header.dart';
import '../chat/chat_screen.dart';
import '../runs/run_detail_screen.dart';

class HomeScreen extends ConsumerWidget {
  const HomeScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final inboxLoading = ref.watch(
      inboxControllerProvider.select((controller) => controller.loading),
    );
    final threadsLoading = ref.watch(
      threadsControllerProvider.select((controller) => controller.loading),
    );
    final inboxError = ref.watch(
      inboxControllerProvider.select((controller) => controller.errorMessage),
    );
    final threadsError = ref.watch(
      threadsControllerProvider.select((controller) => controller.errorMessage),
    );
    final pending = ref.watch(
      inboxControllerProvider.select((controller) => controller.pendingActions),
    );
    final activeRuns = ref.watch(
      inboxControllerProvider.select((controller) => controller.activeRuns),
    );
    final recentRuns = ref.watch(
      inboxControllerProvider.select(
        (controller) => controller.recentTerminalRuns,
      ),
    );
    final system = ref.watch(
      inboxControllerProvider.select((controller) => controller.system),
    );
    final activeThread = ref.watch(
      threadsControllerProvider.select((controller) => controller.activeThread),
    );
    final busy = inboxLoading || threadsLoading;
    final readiness = system?.runtimeReadiness.status ?? 'unknown';
    final readinessTone = _readinessTone(readiness);

    return Scaffold(
      appBar: AppBar(
        title: const Text('Home'),
        actions: [
          IconButton.filledTonal(
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh),
            onPressed: busy ? null : () => _refreshHome(ref),
          ),
          IconButton.filledTonal(
            tooltip: 'New thread',
            icon: const Icon(Icons.add_comment_outlined),
            onPressed: busy
                ? null
                : () => _createThreadAndOpenChat(context, ref),
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: () => _refreshHome(ref),
        child: ListView(
          padding: const EdgeInsets.only(bottom: 16),
          children: [
            if (inboxError != null) ErrorBanner(message: inboxError),
            if (threadsError != null) ErrorBanner(message: threadsError),
            AcornPageIntro(
              icon: _homeIcon(pending, activeRuns, readiness),
              title: _homeTitle(pending, activeRuns, readiness),
              body: system?.runtimeReadiness.reason ?? 'Remote server status',
              tone: _homeTone(pending, activeRuns, readiness),
              trailing: StatusPill(label: readiness, tone: readinessTone),
            ),
            if (pending.isNotEmpty) ...[
              const SectionHeader(title: 'Needs action'),
              for (final action in pending)
                AcornListRow(
                  icon: Icons.rule_folder_outlined,
                  title: action.title,
                  subtitle:
                      '${action.kind} · ${formatTimestamp(action.createdAt)} · ${shortId(action.runId)}',
                  tone: AcornStatusTone.warning,
                  trailing: const Icon(Icons.chevron_right),
                  onTap: () => ref.read(shellControllerProvider).selectTab(2),
                ),
            ],
            if (activeRuns.isNotEmpty) ...[
              const SectionHeader(title: 'Running now'),
              for (final run in activeRuns)
                _RunSummaryRow(
                  run: run,
                  tone: AcornStatusTone.info,
                  icon: Icons.play_circle_outline,
                  onTap: () => _openRunDetail(context, run.runId),
                ),
            ],
            if (recentRuns.isNotEmpty) ...[
              const SectionHeader(title: 'Recently finished'),
              for (final run in recentRuns)
                _RunSummaryRow(
                  run: run,
                  tone: _runTone(run.status),
                  icon: _runIcon(run.status),
                  onTap: () => _openRunDetail(context, run.runId),
                ),
            ],
            const SectionHeader(title: 'Continue'),
            if (activeThread == null)
              AcornListRow(
                icon: Icons.add_comment_outlined,
                title: 'Start a thread',
                subtitle: 'Create a backend thread and open Chat.',
                tone: AcornStatusTone.neutral,
                trailing: const Icon(Icons.chevron_right),
                onTap: busy
                    ? null
                    : () => _createThreadAndOpenChat(context, ref),
              )
            else
              AcornListRow(
                icon: Icons.chat_bubble_outline,
                title: activeThread.title.isEmpty
                    ? 'Current thread'
                    : activeThread.title,
                subtitle:
                    '${statusLabel(activeThread.state)} · ${formatTimestamp(activeThread.updatedAt)} · ${shortId(activeThread.id)}',
                tone: AcornStatusTone.info,
                trailing: const Icon(Icons.chevron_right),
                onTap: () => _openChat(context),
              ),
            const SectionHeader(title: 'System'),
            AcornListRow(
              icon: Icons.health_and_safety_outlined,
              title: system?.model.name ?? 'Unknown model',
              subtitle: system == null
                  ? 'Inbox has not loaded yet.'
                  : '${system.summary.enabledToolCount}/${system.summary.toolCount} tools · ${system.summary.skillCount} skills · ${system.workspaceRoot}',
              tone: readinessTone,
            ),
            if (pending.isEmpty && activeRuns.isEmpty && recentRuns.isEmpty)
              const Padding(
                padding: EdgeInsets.only(top: 12),
                child: AcornEmptyState(
                  icon: Icons.inbox_outlined,
                  title: 'No active backend work',
                  body:
                      'Start a thread or refresh when another device runs Acorn.',
                ),
              ),
          ],
        ),
      ),
    );
  }

  Future<void> _createThreadAndOpenChat(
    BuildContext context,
    WidgetRef ref,
  ) async {
    final thread = await ref
        .read(threadsControllerProvider)
        .createThreadAndSelect();
    if (!context.mounted || thread == null) {
      return;
    }
    _openChat(context);
  }

  Future<void> _refreshHome(WidgetRef ref) async {
    await Future.wait([
      ref.read(inboxControllerProvider).refresh(),
      ref.read(threadsControllerProvider).refresh(),
    ]);
  }

  void _openChat(BuildContext context) {
    Navigator.of(
      context,
    ).push(MaterialPageRoute<void>(builder: (_) => const ChatScreen()));
  }

  void _openRunDetail(BuildContext context, String runId) {
    Navigator.of(context).push(
      MaterialPageRoute<void>(builder: (_) => RunDetailScreen(runId: runId)),
    );
  }
}

class _RunSummaryRow extends StatelessWidget {
  const _RunSummaryRow({
    required this.run,
    required this.tone,
    required this.icon,
    required this.onTap,
  });

  final RunSummary run;
  final AcornStatusTone tone;
  final IconData icon;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final detail = <String>[
      run.lastEventLabel,
      if (run.preview.trim().isNotEmpty) run.preview.trim(),
      'updated ${formatTimestamp(run.updatedAt)}',
      _formatDurationMs(run.durationMs),
    ].where((item) => item.trim().isNotEmpty).join(' · ');
    return AcornListRow(
      icon: icon,
      title: run.threadTitle.trim().isEmpty
          ? shortId(run.threadId)
          : run.threadTitle,
      subtitle: detail,
      tone: tone,
      trailing: const Icon(Icons.chevron_right),
      onTap: onTap,
    );
  }
}

String _homeTitle(
  List<PendingActionSummary> pending,
  List<RunSummary> activeRuns,
  String readiness,
) {
  if (pending.isNotEmpty) {
    return '${pending.length} decision${pending.length == 1 ? '' : 's'} waiting';
  }
  if (activeRuns.isNotEmpty) {
    return '${activeRuns.length} active run${activeRuns.length == 1 ? '' : 's'}';
  }
  if (readiness == 'ready') {
    return 'Acorn is ready';
  }
  return 'Acorn needs attention';
}

IconData _homeIcon(
  List<PendingActionSummary> pending,
  List<RunSummary> activeRuns,
  String readiness,
) {
  if (pending.isNotEmpty) {
    return Icons.rule_folder_outlined;
  }
  if (activeRuns.isNotEmpty) {
    return Icons.play_circle_outline;
  }
  if (readiness == 'ready') {
    return Icons.check_circle_outline;
  }
  return Icons.error_outline;
}

AcornStatusTone _homeTone(
  List<PendingActionSummary> pending,
  List<RunSummary> activeRuns,
  String readiness,
) {
  if (pending.isNotEmpty) {
    return AcornStatusTone.warning;
  }
  if (activeRuns.isNotEmpty) {
    return AcornStatusTone.info;
  }
  return _readinessTone(readiness);
}

AcornStatusTone _readinessTone(String status) {
  return status == 'ready' ? AcornStatusTone.success : AcornStatusTone.error;
}

AcornStatusTone _runTone(String status) {
  return switch (status) {
    'completed' => AcornStatusTone.success,
    'failed' => AcornStatusTone.error,
    'interrupted' => AcornStatusTone.warning,
    _ => AcornStatusTone.info,
  };
}

IconData _runIcon(String status) {
  return switch (status) {
    'completed' => Icons.check_circle_outline,
    'failed' => Icons.error_outline,
    'interrupted' => Icons.pause_circle_outline,
    _ => Icons.play_circle_outline,
  };
}

String _formatDurationMs(int durationMs) {
  if (durationMs <= 0) {
    return '';
  }
  final seconds = durationMs ~/ 1000;
  if (seconds < 60) {
    return '${seconds}s';
  }
  final minutes = seconds ~/ 60;
  final remainingSeconds = seconds % 60;
  if (minutes < 60) {
    return remainingSeconds == 0
        ? '${minutes}m'
        : '${minutes}m ${remainingSeconds}s';
  }
  final hours = minutes ~/ 60;
  final remainingMinutes = minutes % 60;
  return remainingMinutes == 0 ? '${hours}h' : '${hours}h ${remainingMinutes}m';
}
