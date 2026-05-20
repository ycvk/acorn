import 'dart:async';

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

    return Column(
      children: [
        if (state.loading) const LinearProgressIndicator(minHeight: 2),
        if (state.errorMessage != null)
          ErrorBanner(message: state.errorMessage!),
        Expanded(
          child: _RunDetailBody(
            detail: detail,
            onOpenThread: () => _openThread(context, detail.thread),
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
  const _RunDetailBody({required this.detail, required this.onOpenThread});

  final RunDetail detail;
  final VoidCallback onOpenThread;

  @override
  Widget build(BuildContext context) {
    final run = detail.run;
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
        const SliverToBoxAdapter(child: SectionHeader(title: 'Activity')),
        if (detail.events.isEmpty)
          const SliverToBoxAdapter(
            child: AcornEmptyState(
              icon: Icons.timeline_outlined,
              title: 'No run events',
              body: 'The backend returned an empty event projection.',
            ),
          )
        else
          SliverList.builder(
            itemCount: detail.events.length,
            itemBuilder: (context, index) {
              final event = detail.events[index];
              return AcornListRow(
                icon: _eventIcon(event),
                title: runEventLabel(event),
                subtitle:
                    '${event.type} · seq ${event.seq} · ${formatTimestamp(event.ts)}${_eventDetailSuffix(event)}',
                tone: _eventTone(event),
              );
            },
          ),
        const SliverToBoxAdapter(child: SectionHeader(title: 'Artifacts')),
        if (detail.artifacts.isEmpty)
          const SliverToBoxAdapter(
            child: _EmptySectionRow(
              icon: Icons.inventory_2_outlined,
              title: 'No artifacts',
              subtitle: 'This run did not publish artifact records.',
            ),
          )
        else
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
        const SliverToBoxAdapter(
          child: SectionHeader(title: 'Terminal sessions'),
        ),
        if (detail.terminalSessions.isEmpty)
          const SliverToBoxAdapter(
            child: _EmptySectionRow(
              icon: Icons.terminal_outlined,
              title: 'No terminal sessions',
              subtitle: 'No terminal process records are attached to this run.',
            ),
          )
        else
          SliverList.builder(
            itemCount: detail.terminalSessions.length,
            itemBuilder: (context, index) {
              final session = detail.terminalSessions[index];
              return AcornListRow(
                icon: Icons.terminal_outlined,
                title: session.label ?? shortId(session.terminalSessionId),
                subtitle:
                    '${session.status} · ${session.cwd} · ${_terminalExit(session)}',
                tone: _terminalTone(session.status),
              );
            },
          ),
        const SliverToBoxAdapter(child: SizedBox(height: 18)),
      ],
    );
  }
}

class _EmptySectionRow extends StatelessWidget {
  const _EmptySectionRow({
    required this.icon,
    required this.title,
    required this.subtitle,
  });

  final IconData icon;
  final String title;
  final String subtitle;

  @override
  Widget build(BuildContext context) {
    return AcornListRow(
      icon: icon,
      title: title,
      subtitle: subtitle,
      tone: AcornStatusTone.neutral,
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
  if (event.type == 'run.interrupted' || event.type == 'context.pressure') {
    return AcornStatusTone.warning;
  }
  if (event.type == 'run.completed') {
    return AcornStatusTone.success;
  }
  return AcornStatusTone.neutral;
}

AcornStatusTone _terminalTone(String status) {
  return switch (status) {
    'completed' => AcornStatusTone.success,
    'failed' => AcornStatusTone.error,
    'interrupted' => AcornStatusTone.warning,
    'running' => AcornStatusTone.info,
    _ => AcornStatusTone.neutral,
  };
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
  if (event.type.startsWith('tool.call.')) {
    return Icons.build_outlined;
  }
  if (event.type.startsWith('skill.')) {
    return Icons.extension_outlined;
  }
  if (event.type.startsWith('plan.') || event.type.startsWith('step.')) {
    return Icons.account_tree_outlined;
  }
  if (event.type.startsWith('context.')) {
    return Icons.compress_outlined;
  }
  if (event.type.startsWith('memory.') ||
      event.type.startsWith('crystallization.')) {
    return Icons.psychology_alt_outlined;
  }
  if (event.type == 'assistant.delta' || event.type == 'agent.message') {
    return Icons.chat_bubble_outline;
  }
  return _statusIcon(event.type.replaceFirst('run.', ''));
}

String _terminalExit(RunTerminalSession session) {
  if (session.exitCode != null) {
    return 'exit ${session.exitCode}';
  }
  if (session.signal != null) {
    return 'signal ${session.signal}';
  }
  return shortId(session.terminalSessionId);
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
