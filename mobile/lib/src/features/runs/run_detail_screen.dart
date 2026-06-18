import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../api/acorn_api.dart';
import '../../core/connection_controller.dart';
import '../../core/providers.dart';
import '../../ui/theme/acorn_theme.dart';
import '../../ui/widgets/acorn_formatters.dart';
import '../../ui/widgets/acorn_status.dart';
import '../../ui/widgets/acorn_surfaces.dart';
import '../../ui/widgets/empty_state.dart';
import '../../ui/widgets/list_rows.dart';
import '../../ui/widgets/section_header.dart';
import '../chat/chat_models.dart';
import '../chat/chat_screen.dart';
import 'run_detail_controller.dart';

class RunDetailScreen extends ConsumerStatefulWidget {
  const RunDetailScreen({super.key, required this.runId});

  final String runId;

  @override
  ConsumerState<RunDetailScreen> createState() => _RunDetailScreenState();
}

class _RunDetailScreenState extends ConsumerState<RunDetailScreen> {
  @override
  void initState() {
    super.initState();
    unawaited(ref.read(runDetailControllerProvider).load(widget.runId));
  }

  @override
  void didUpdateWidget(covariant RunDetailScreen oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.runId != widget.runId) {
      unawaited(ref.read(runDetailControllerProvider).load(widget.runId));
    }
  }

  bool _actionPending = false;

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(
      runDetailControllerProvider.select(
        (controller) => controller.stateFor(widget.runId),
      ),
    );
    return Scaffold(
      appBar: AppBar(
        title: Text('Run ${shortId(widget.runId)}'),
        actions: [
          IconButton.filledTonal(
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh),
            onPressed: state.loading ? null : _refresh,
          ),
        ],
      ),
      body: _buildBody(context, state),
    );
  }

  void _refresh() {
    unawaited(
      ref.read(runDetailControllerProvider).load(widget.runId, force: true),
    );
  }

  Future<void> _interrupt() async {
    await _runLifecycleAction(
      (api) => api.interruptRun(widget.runId),
    );
  }

  Future<void> _resume() async {
    await _runLifecycleAction(
      (api) => api.resumeRun(widget.runId),
    );
  }

  Future<void> _runLifecycleAction(
    Future<void> Function(AcornApiClient api) action,
  ) async {
    if (_actionPending) {
      return;
    }
    setState(() => _actionPending = true);
    final messenger = ScaffoldMessenger.of(context);
    try {
      await action(ref.read(connectionControllerProvider).api);
      await ref
          .read(runDetailControllerProvider)
          .load(widget.runId, force: true);
    } catch (error) {
      messenger.showSnackBar(
        SnackBar(content: Text(acornUserFacingErrorText(error))),
      );
    } finally {
      if (mounted) {
        setState(() => _actionPending = false);
      }
    }
  }

  Widget _buildBody(BuildContext context, RunDetailState state) {
    final detail = state.detail;
    if (detail == null) {
      final error = state.errorMessage;
      if (error != null) {
        return ListView(
          children: [
            ErrorBanner(message: error),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16),
              child: FilledButton.icon(
                onPressed: state.loading ? null : _refresh,
                icon: const Icon(Icons.refresh),
                label: const Text('Retry'),
              ),
            ),
          ],
        );
      }
      return const Center(child: CircularProgressIndicator());
    }

    final interruptResumeEnabled =
        ref
            .watch(
              inboxControllerProvider.select(
                (controller) => controller.system,
              ),
            )
            ?.features
            .interruptResume ??
        false;

    return Column(
      children: [
        if (state.loading) const LinearProgressIndicator(minHeight: 2),
        if (state.errorMessage != null)
          ErrorBanner(message: state.errorMessage!),
        Expanded(
          child: _RunDetailBody(
            detail: detail,
            onOpenThread: () => _openThread(context, detail.thread),
            interruptResumeEnabled: interruptResumeEnabled,
            actionPending: _actionPending,
            onInterrupt: _interrupt,
            onResume: _resume,
          ),
        ),
      ],
    );
  }

  Future<void> _openThread(BuildContext context, Thread thread) async {
    ref.read(threadsControllerProvider).selectThread(thread);
    if (!context.mounted) {
      return;
    }
    await Navigator.of(
      context,
    ).push(MaterialPageRoute<void>(builder: (_) => const ChatScreen()));
  }
}

class _RunDetailBody extends StatelessWidget {
  const _RunDetailBody({
    required this.detail,
    required this.onOpenThread,
    required this.interruptResumeEnabled,
    required this.actionPending,
    required this.onInterrupt,
    required this.onResume,
  });

  final RunDetail detail;
  final VoidCallback onOpenThread;
  final bool interruptResumeEnabled;
  final bool actionPending;
  final VoidCallback onInterrupt;
  final VoidCallback onResume;

  @override
  Widget build(BuildContext context) {
    final run = detail.run;
    final activityCount = detail.events.length;
    final threadTitle = detail.thread.title.trim().isEmpty
        ? 'Thread ${shortId(detail.thread.id)}'
        : detail.thread.title.trim();
    final tone = _statusTone(run.status);
    return CustomScrollView(
      slivers: [
        SliverToBoxAdapter(
          child: AcornPageIntro(
            icon: _statusIcon(run.status),
            title: '${statusLabel(run.status)} · ${statusLabel(run.mode)}',
            body:
                '$threadTitle · started ${formatTimestamp(run.createdAt)}${run.completedAt == null ? '' : ' · ended ${formatTimestamp(run.completedAt!)}'}',
            tone: tone,
            trailing: StatusPill(label: run.status, tone: tone),
          ),
        ),
        SliverToBoxAdapter(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
            child: FilledButton.tonalIcon(
              onPressed: onOpenThread,
              icon: const Icon(Icons.chat_bubble_outline),
              label: const Text('Open thread'),
            ),
          ),
        ),
        if (interruptResumeEnabled && run.status == 'running')
          SliverToBoxAdapter(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
              child: FilledButton.icon(
                onPressed: actionPending ? null : onInterrupt,
                icon: const Icon(Icons.stop_circle_outlined),
                label: const Text('中断'),
              ),
            ),
          ),
        if (interruptResumeEnabled && run.status == 'interrupted')
          SliverToBoxAdapter(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
              child: FilledButton.icon(
                onPressed: actionPending ? null : onResume,
                icon: const Icon(Icons.play_circle_outline),
                label: const Text('恢复'),
              ),
            ),
          ),
        SliverToBoxAdapter(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 8, 16, 0),
            child: OutlinedButton.icon(
              onPressed: () => Navigator.of(context).push(
                MaterialPageRoute<void>(
                  builder: (_) => _RunActivityScreen(detail: detail),
                ),
              ),
              icon: const Icon(Icons.timeline_outlined),
              label: Text('Activity ($activityCount)'),
            ),
          ),
        ),
        if (detail.artifacts.isNotEmpty) ...[
          const SliverToBoxAdapter(child: SectionHeader(title: 'Artifacts')),
          SliverList.builder(
            itemCount: detail.artifacts.length,
            itemBuilder: (context, index) {
              final artifact = detail.artifacts[index];
              return AcornListRow(
                icon: Icons.inventory_2_outlined,
                title: artifact.title ?? artifact.kind,
                subtitle:
                    '${artifact.kind} · ${_formatBytes(artifact.sizeBytes)} · ${shortId(artifact.sha256)}',
                tone: AcornStatusTone.neutral,
              );
            },
          ),
        ],
        const SliverToBoxAdapter(child: SizedBox(height: 18)),
      ],
    );
  }
}

class _RunActivityScreen extends StatelessWidget {
  const _RunActivityScreen({required this.detail});

  final RunDetail detail;

  @override
  Widget build(BuildContext context) {
    final important = _importantEvents(detail.events);
    return Scaffold(
      appBar: AppBar(title: Text('Run activity ${shortId(detail.run.id)}')),
      body: ListView(
        children: [
          AcornPageIntro(
            icon: Icons.timeline_outlined,
            title: 'Run activity',
            body:
                '${statusLabel(detail.run.status)} · ${shortId(detail.thread.id)}',
            tone: AcornStatusTone.neutral,
          ),
          const SectionHeader(title: 'Issues'),
          if (important.isEmpty)
            const AcornEmptyState(
              icon: Icons.check_circle_outline,
              title: 'No issues recorded',
              body: 'This run did not publish failed or interrupted events.',
            )
          else
            for (final event in important)
              AcornListRow(
                icon: _eventIcon(event),
                title: runEventLabel(event),
                subtitle:
                    '${event.type} · seq ${event.seq} · ${formatTimestamp(event.ts)}${_eventDetailSuffix(event)}',
                tone: _eventTone(event),
              ),
          const SizedBox(height: 18),
        ],
      ),
    );
  }
}

String _eventDetailSuffix(RunEvent event) {
  final detail = runEventDetail(event);
  if (detail == null || detail.isEmpty) {
    return '';
  }
  return ' · $detail';
}

AcornStatusTone _statusTone(String status) {
  return switch (status) {
    'completed' => AcornStatusTone.success,
    'failed' => AcornStatusTone.error,
    'interrupted' => AcornStatusTone.warning,
    'running' => AcornStatusTone.info,
    _ => AcornStatusTone.neutral,
  };
}

AcornStatusTone _eventTone(RunEvent event) {
  if (event.type.endsWith('.failed') || event.type == 'run.failed') {
    return AcornStatusTone.error;
  }
  if (event.type == 'run.interrupted') {
    return AcornStatusTone.warning;
  }
  if (event.type == 'run.completed') {
    return AcornStatusTone.success;
  }
  return AcornStatusTone.neutral;
}

IconData _statusIcon(String status) {
  return switch (status) {
    'completed' => Icons.check_circle_outline,
    'failed' => Icons.error_outline,
    'interrupted' => Icons.pause_circle_outline,
    'running' => Icons.play_circle_outline,
    _ => Icons.timeline_outlined,
  };
}

IconData _eventIcon(RunEvent event) {
  if (event.type == 'assistant.delta' || event.type == 'agent.message') {
    return Icons.chat_bubble_outline;
  }
  return _statusIcon(event.type.replaceFirst('run.', ''));
}

List<RunEvent> _importantEvents(List<RunEvent> events) {
  return events
      .where((event) {
        if (event.type == 'run.failed' || event.type == 'run.interrupted') {
          return true;
        }
        if (event.type.endsWith('.failed')) {
          return true;
        }
        return false;
      })
      .toList(growable: false);
}

String _formatBytes(int bytes) {
  if (bytes < 1024) {
    return '$bytes B';
  }
  final kib = bytes / 1024;
  if (kib < 1024) {
    return '${kib.toStringAsFixed(1)} KiB';
  }
  final mib = kib / 1024;
  return '${mib.toStringAsFixed(1)} MiB';
}
