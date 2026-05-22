import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../api/acorn_api.dart';
import '../../core/providers.dart';
import '../../ui/theme/acorn_theme.dart';
import '../../ui/widgets/acorn_formatters.dart';
import '../../ui/widgets/acorn_status.dart';
import '../../ui/widgets/acorn_surfaces.dart';
import '../../ui/widgets/empty_state.dart';
import '../../ui/widgets/section_header.dart';
import '../chat/chat_screen.dart';
import '../runs/run_detail_screen.dart';
import 'thread_titles.dart';

const _approvalsTabIndex = 1;

class ThreadsScreen extends ConsumerWidget {
  const ThreadsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final threads = ref.watch(
      threadsControllerProvider.select((controller) => controller.threads),
    );
    final activeThreadId = ref.watch(
      threadsControllerProvider.select(
        (controller) => controller.activeThread?.id,
      ),
    );
    final threadsLoading = ref.watch(
      threadsControllerProvider.select((controller) => controller.loading),
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
    final recentTerminalRuns = ref.watch(
      inboxControllerProvider.select(
        (controller) => controller.recentTerminalRuns,
      ),
    );
    final inboxLoading = ref.watch(
      inboxControllerProvider.select((controller) => controller.loading),
    );
    final inboxError = ref.watch(
      inboxControllerProvider.select((controller) => controller.errorMessage),
    );
    final system = ref.watch(
      inboxControllerProvider.select((controller) => controller.system),
    );
    final profile = ref.watch(
      connectionControllerProvider.select((controller) => controller.profile),
    );
    final priorityRuns = _priorityRuns(activeRuns, recentTerminalRuns);
    final busy = threadsLoading || inboxLoading;

    return Scaffold(
      body: SafeArea(
        bottom: false,
        child: RefreshIndicator(
          onRefresh: () => _refresh(ref),
          child: CustomScrollView(
            physics: const AlwaysScrollableScrollPhysics(),
            slivers: [
              SliverToBoxAdapter(
                child: _ThreadsHero(
                  serverLabel: _serverLabel(profile?.serverUrl),
                  modelName: system?.model.name,
                  readiness: system?.runtimeReadiness.status ?? 'unknown',
                  readinessReason: system?.runtimeReadiness.reason,
                  threadCount: threads.length,
                  pendingCount: pending.length,
                  activeRunCount: activeRuns.length,
                  recentRunCount: recentTerminalRuns.length,
                  busy: busy,
                  onRefresh: () => _refresh(ref),
                  onNewThread: () => _createThreadAndOpenChat(context, ref),
                  onOpenApprovals: () => _openApprovals(ref),
                ),
              ),
              if (threadsError != null)
                SliverToBoxAdapter(child: ErrorBanner(message: threadsError)),
              if (inboxError != null)
                SliverToBoxAdapter(child: ErrorBanner(message: inboxError)),
              if (pending.isNotEmpty) ...[
                SliverToBoxAdapter(
                  child: SectionHeader(
                    title: 'Needs decision',
                    trailing: TextButton(
                      onPressed: () => _openApprovals(ref),
                      child: const Text('Review'),
                    ),
                  ),
                ),
                SliverToBoxAdapter(
                  child: _ApprovalAttentionCard(
                    action: pending.first,
                    count: pending.length,
                    onTap: () => _openApprovals(ref),
                  ),
                ),
              ],
              if (priorityRuns.isNotEmpty) ...[
                const SliverToBoxAdapter(
                  child: SectionHeader(title: 'Active backend work'),
                ),
                SliverList.builder(
                  itemCount: priorityRuns.length,
                  itemBuilder: (context, index) {
                    final run = priorityRuns[index];
                    return _RunSummaryCard(
                      run: run,
                      onTap: () => _openRun(context, run),
                    );
                  },
                ),
              ],
              SliverToBoxAdapter(
                child: SectionHeader(
                  title: 'Threads',
                  trailing: IconButton.filledTonal(
                    tooltip: 'New thread',
                    icon: const Icon(Icons.add),
                    onPressed: busy
                        ? null
                        : () => _createThreadAndOpenChat(context, ref),
                  ),
                ),
              ),
              if (threads.isEmpty)
                SliverToBoxAdapter(
                  child: Padding(
                    padding: const EdgeInsets.fromLTRB(12, 4, 12, 28),
                    child: AcornEmptyState(
                      icon: Icons.add_comment_outlined,
                      title: 'Start a thread',
                      body:
                          'Create a backend thread from this phone; Acorn will keep the conversation and run state on your server.',
                      action: FilledButton.icon(
                        onPressed: busy
                            ? null
                            : () => _createThreadAndOpenChat(context, ref),
                        icon: const Icon(Icons.add),
                        label: const Text('New thread'),
                      ),
                    ),
                  ),
                )
              else
                SliverList.builder(
                  itemCount: threads.length,
                  itemBuilder: (context, index) {
                    final thread = threads[index];
                    return _ThreadCard(
                      thread: thread,
                      selected: activeThreadId == thread.id,
                      busy: busy,
                      onTap: () => _openThread(context, ref, thread),
                      onDelete: () =>
                          _confirmDeleteThread(context, ref, thread),
                    );
                  },
                ),
              const SliverToBoxAdapter(child: SizedBox(height: 24)),
            ],
          ),
        ),
      ),
    );
  }

  Future<void> _refresh(WidgetRef ref) async {
    await Future.wait([
      ref.read(inboxControllerProvider).refresh(),
      ref.read(threadsControllerProvider).refresh(),
    ]);
  }

  Future<void> _createThreadAndOpenChat(
    BuildContext context,
    WidgetRef ref,
  ) async {
    ref.read(threadsControllerProvider).startDraftThread();
    if (!context.mounted) {
      return;
    }
    _pushChat(context);
  }

  Future<void> _openThread(
    BuildContext context,
    WidgetRef ref,
    Thread thread,
  ) async {
    ref.read(threadsControllerProvider).selectThread(thread);
    if (!context.mounted) {
      return;
    }
    _pushChat(context);
  }

  void _pushChat(BuildContext context) {
    Navigator.of(
      context,
    ).push(MaterialPageRoute<void>(builder: (_) => const ChatScreen()));
  }

  void _openApprovals(WidgetRef ref) {
    ref.read(shellControllerProvider).selectTab(_approvalsTabIndex);
  }

  Future<void> _openRun(BuildContext context, RunSummary run) async {
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => RunDetailScreen(runId: run.runId),
      ),
    );
  }

  Future<void> _confirmDeleteThread(
    BuildContext context,
    WidgetRef ref,
    Thread thread,
  ) async {
    final colors = Theme.of(context).colorScheme;
    final title = threadDisplayTitle(thread);
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        icon: Icon(Icons.delete_outline, color: colors.error),
        title: const Text('Delete thread?'),
        content: Text('Delete "$title" and its messages from this server.'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            style: FilledButton.styleFrom(
              backgroundColor: colors.error,
              foregroundColor: colors.onError,
            ),
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    if (confirmed != true || !context.mounted) {
      return;
    }
    await ref.read(threadsControllerProvider).deleteThread(thread);
    await ref.read(inboxControllerProvider).refresh();
  }
}

class _ThreadsHero extends StatelessWidget {
  const _ThreadsHero({
    required this.serverLabel,
    required this.modelName,
    required this.readiness,
    required this.threadCount,
    required this.pendingCount,
    required this.activeRunCount,
    required this.recentRunCount,
    required this.busy,
    required this.onRefresh,
    required this.onNewThread,
    required this.onOpenApprovals,
    this.readinessReason,
  });

  final String serverLabel;
  final String? modelName;
  final String readiness;
  final String? readinessReason;
  final int threadCount;
  final int pendingCount;
  final int activeRunCount;
  final int recentRunCount;
  final bool busy;
  final VoidCallback onRefresh;
  final VoidCallback onNewThread;
  final VoidCallback onOpenApprovals;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final dark = colors.brightness == Brightness.dark;
    final background = dark
        ? colors.surfaceContainerHigh
        : colors.inverseSurface;
    final foreground = dark ? colors.onSurface : colors.onInverseSurface;
    final muted = foreground.withValues(alpha: 0.72);
    final readinessTone = _readinessTone(readiness);
    final title = pendingCount > 0
        ? 'Needs your decision'
        : activeRunCount > 0
        ? 'Backend work is running'
        : 'Ready for next work';
    final reason = readinessReason?.trim();
    final subtitle = reason == null || reason.isEmpty
        ? '${statusLabel(readiness)} on ${modelName ?? 'configured model'}'
        : reason;

    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 14, 16, 10),
      child: Material(
        color: background,
        surfaceTintColor: Colors.transparent,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(34)),
        clipBehavior: Clip.antiAlias,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(20, 18, 20, 20),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  _BrandMark(color: foreground),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          'Acorn',
                          style: text.titleMedium?.copyWith(
                            color: foreground,
                            fontWeight: FontWeight.w900,
                          ),
                        ),
                        const SizedBox(height: 1),
                        Text(
                          serverLabel,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: text.labelSmall?.copyWith(
                            color: muted,
                            fontWeight: FontWeight.w700,
                          ),
                        ),
                      ],
                    ),
                  ),
                  StatusPill(
                    label: statusLabel(readiness),
                    tone: readinessTone,
                  ),
                ],
              ),
              const SizedBox(height: 24),
              Text(
                title,
                style: text.headlineMedium?.copyWith(
                  color: foreground,
                  fontWeight: FontWeight.w900,
                  height: 1.02,
                ),
              ),
              const SizedBox(height: 8),
              Text(
                subtitle,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: text.bodyMedium?.copyWith(
                  color: muted,
                  fontWeight: FontWeight.w600,
                ),
              ),
              const SizedBox(height: 22),
              Wrap(
                spacing: 14,
                runSpacing: 12,
                children: [
                  _HeroStat(
                    value: '$threadCount',
                    label: 'threads',
                    foreground: foreground,
                  ),
                  _HeroStat(
                    value: '$activeRunCount',
                    label: 'running',
                    foreground: foreground,
                  ),
                  _HeroStat(
                    value: '$pendingCount',
                    label: 'pending',
                    foreground: foreground,
                  ),
                  _HeroStat(
                    value: '$recentRunCount',
                    label: 'recent runs',
                    foreground: foreground,
                  ),
                ],
              ),
              const SizedBox(height: 22),
              Row(
                children: [
                  Expanded(
                    child: FilledButton.icon(
                      onPressed: busy ? null : onNewThread,
                      icon: const Icon(Icons.add),
                      label: const Text('New thread'),
                    ),
                  ),
                  const SizedBox(width: 10),
                  IconButton.filledTonal(
                    tooltip: pendingCount > 0 ? 'Open approvals' : 'Refresh',
                    onPressed: busy
                        ? null
                        : pendingCount > 0
                        ? onOpenApprovals
                        : onRefresh,
                    icon: Icon(
                      pendingCount > 0
                          ? Icons.rule_folder_outlined
                          : Icons.refresh,
                    ),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _HeroStat extends StatelessWidget {
  const _HeroStat({
    required this.value,
    required this.label,
    required this.foreground,
  });

  final String value;
  final String label;
  final Color foreground;

  @override
  Widget build(BuildContext context) {
    final text = Theme.of(context).textTheme;
    return SizedBox(
      width: 82,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            value,
            style: text.titleLarge?.copyWith(
              color: foreground,
              fontWeight: FontWeight.w900,
              height: 1,
            ),
          ),
          const SizedBox(height: 3),
          Text(
            label,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: text.labelSmall?.copyWith(
              color: foreground.withValues(alpha: 0.64),
              fontWeight: FontWeight.w700,
            ),
          ),
        ],
      ),
    );
  }
}

class _BrandMark extends StatelessWidget {
  const _BrandMark({required this.color});

  final Color color;

  @override
  Widget build(BuildContext context) {
    return SizedBox.square(
      dimension: 34,
      child: DecoratedBox(
        decoration: BoxDecoration(
          color: color.withValues(alpha: 0.11),
          shape: BoxShape.circle,
          border: Border.all(color: color.withValues(alpha: 0.16)),
        ),
        child: Icon(Icons.grid_view_rounded, color: color, size: 18),
      ),
    );
  }
}

class _ApprovalAttentionCard extends StatelessWidget {
  const _ApprovalAttentionCard({
    required this.action,
    required this.count,
    required this.onTap,
  });

  final PendingActionSummary action;
  final int count;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final statuses = AcornStatusColors.of(context);
    final tone = statuses.warning;
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
      child: Material(
        color: tone.container,
        surfaceTintColor: Colors.transparent,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(AcornRadius.xl),
          side: BorderSide(color: tone.color.withValues(alpha: 0.22)),
        ),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 14, 12, 14),
            child: Row(
              children: [
                Icon(Icons.priority_high_rounded, color: tone.onContainer),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        count == 1 ? action.title : '$count actions waiting',
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: Theme.of(context).textTheme.titleSmall?.copyWith(
                          color: tone.onContainer,
                          fontWeight: FontWeight.w900,
                        ),
                      ),
                      const SizedBox(height: 3),
                      Text(
                        action.body ??
                            '${action.kind} for run ${shortId(action.runId)}',
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: tone.onContainer.withValues(alpha: 0.76),
                        ),
                      ),
                    ],
                  ),
                ),
                const SizedBox(width: 8),
                Icon(Icons.arrow_forward_rounded, color: tone.onContainer),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _RunSummaryCard extends StatelessWidget {
  const _RunSummaryCard({required this.run, required this.onTap});

  final RunSummary run;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final tone = AcornStatusColors.of(context).tone(_runTone(run));
    final preview = run.preview.trim();
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
      child: Material(
        color: tone.container,
        surfaceTintColor: Colors.transparent,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(AcornRadius.xl),
          side: BorderSide(color: tone.color.withValues(alpha: 0.18)),
        ),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 14, 12, 14),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Icon(_runIcon(run), color: tone.onContainer, size: 22),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Text(
                        run.threadTitle.trim().isEmpty
                            ? 'Run ${shortId(run.runId)}'
                            : run.threadTitle,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: Theme.of(context).textTheme.titleSmall?.copyWith(
                          color: tone.onContainer,
                          fontWeight: FontWeight.w900,
                        ),
                      ),
                    ),
                    const SizedBox(width: 8),
                    StatusPill(
                      label: statusLabel(run.status),
                      tone: _runTone(run),
                    ),
                  ],
                ),
                if (preview.isNotEmpty) ...[
                  const SizedBox(height: 8),
                  Text(
                    preview,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: tone.onContainer.withValues(alpha: 0.76),
                    ),
                  ),
                ],
                const SizedBox(height: 10),
                Wrap(
                  spacing: 8,
                  runSpacing: 6,
                  children: [
                    InlineStatusLabel(
                      label: statusLabel(run.mode),
                      tone: AcornStatusTone.neutral,
                    ),
                    InlineStatusLabel(
                      label: run.lastEventLabel.isEmpty
                          ? formatTimestamp(run.updatedAt)
                          : run.lastEventLabel,
                      tone: _runTone(run),
                    ),
                    InlineStatusLabel(
                      label: _formatDuration(run.durationMs),
                      tone: AcornStatusTone.neutral,
                    ),
                  ],
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _ThreadCard extends StatelessWidget {
  const _ThreadCard({
    required this.thread,
    required this.selected,
    required this.busy,
    required this.onTap,
    required this.onDelete,
  });

  final Thread thread;
  final bool selected;
  final bool busy;
  final VoidCallback onTap;
  final VoidCallback onDelete;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final background = selected
        ? colors.primaryContainer
        : colors.surfaceContainerLow;
    final foreground = selected ? colors.onPrimaryContainer : colors.onSurface;
    final muted = selected
        ? colors.onPrimaryContainer.withValues(alpha: 0.72)
        : colors.onSurfaceVariant;
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
      child: Material(
        color: background,
        surfaceTintColor: Colors.transparent,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(AcornRadius.xl),
          side: BorderSide(
            color: selected
                ? colors.primary.withValues(alpha: 0.44)
                : colors.outlineVariant,
            width: selected ? 1.3 : 1,
          ),
        ),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(14, 14, 10, 12),
            child: Row(
              children: [
                AcornTonalIcon(
                  icon: selected ? Icons.forum : Icons.forum_outlined,
                  tone: selected
                      ? AcornStatusTone.info
                      : AcornStatusTone.neutral,
                  size: 44,
                  iconSize: 23,
                  radius: AcornRadius.lg,
                ),
                const SizedBox(width: 13),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        threadDisplayTitle(thread),
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: text.titleSmall?.copyWith(
                          color: foreground,
                          fontWeight: FontWeight.w900,
                        ),
                      ),
                      const SizedBox(height: 5),
                      Text(
                        '${statusLabel(thread.state)} · ${formatTimestamp(thread.updatedAt)} · ${shortId(thread.id)}',
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: text.bodySmall?.copyWith(
                          color: muted,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                    ],
                  ),
                ),
                const SizedBox(width: 8),
                IconButton(
                  tooltip: 'Delete thread',
                  icon: const Icon(Icons.delete_outline),
                  onPressed: busy ? null : onDelete,
                ),
                Icon(
                  selected ? Icons.check_circle_rounded : Icons.chevron_right,
                  color: selected ? colors.primary : colors.onSurfaceVariant,
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

List<RunSummary> _priorityRuns(
  List<RunSummary> activeRuns,
  List<RunSummary> recentTerminalRuns,
) {
  final seen = <String>{};
  final result = <RunSummary>[];
  for (final run in [...activeRuns, ...recentTerminalRuns]) {
    if (!seen.add(run.runId)) {
      continue;
    }
    if (run.status == 'running' ||
        run.attentionLevel == 'needs_action' ||
        run.attentionLevel == 'failed' ||
        run.status == 'failed' ||
        run.status == 'interrupted') {
      result.add(run);
    }
    if (result.length == 3) {
      break;
    }
  }
  return result;
}

String _serverLabel(String? serverUrl) {
  final raw = serverUrl?.trim();
  if (raw == null || raw.isEmpty) {
    return 'No server';
  }
  final uri = Uri.tryParse(raw);
  final host = uri?.host.trim();
  if (host != null && host.isNotEmpty) {
    return host;
  }
  return raw;
}

AcornStatusTone _readinessTone(String readiness) {
  return switch (readiness) {
    'ready' => AcornStatusTone.success,
    'degraded' => AcornStatusTone.warning,
    'unknown' => AcornStatusTone.neutral,
    _ => AcornStatusTone.error,
  };
}

AcornStatusTone _runTone(RunSummary run) {
  if (run.attentionLevel == 'needs_action') {
    return AcornStatusTone.warning;
  }
  if (run.attentionLevel == 'failed' || run.status == 'failed') {
    return AcornStatusTone.error;
  }
  return switch (run.status) {
    'completed' => AcornStatusTone.success,
    'running' => AcornStatusTone.info,
    'interrupted' => AcornStatusTone.warning,
    _ => AcornStatusTone.neutral,
  };
}

IconData _runIcon(RunSummary run) {
  return switch (_runTone(run)) {
    AcornStatusTone.success => Icons.check_circle_outline,
    AcornStatusTone.warning => Icons.priority_high_rounded,
    AcornStatusTone.info => Icons.play_circle_outline,
    AcornStatusTone.error => Icons.error_outline,
    AcornStatusTone.neutral => Icons.terminal_outlined,
  };
}

String _formatDuration(int durationMs) {
  if (durationMs <= 0) {
    return 'just started';
  }
  final seconds = (durationMs / 1000).round();
  if (seconds < 60) {
    return '${seconds}s';
  }
  final minutes = (seconds / 60).round();
  if (minutes < 60) {
    return '${minutes}m';
  }
  final hours = (minutes / 60).round();
  return '${hours}h';
}
