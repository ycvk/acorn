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
    final diagnosticCount = detail.events.length;
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
        SliverToBoxAdapter(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 8, 16, 0),
            child: OutlinedButton.icon(
              onPressed: () => Navigator.of(context).push(
                MaterialPageRoute<void>(
                  builder: (_) => _RunDiagnosticsScreen(detail: detail),
                ),
              ),
              icon: const Icon(Icons.bug_report_outlined),
              label: Text('Diagnostics ($diagnosticCount)'),
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

class _RunDiagnosticsScreen extends StatelessWidget {
  const _RunDiagnosticsScreen({required this.detail});

  final RunDetail detail;

  @override
  Widget build(BuildContext context) {
    final important = _importantEvents(detail.events);
    final counts = _eventTypeCounts(detail.events.map((event) => event.type));
    final totalCount = detail.events.length;
    return Scaffold(
      appBar: AppBar(title: Text('Diagnostics ${shortId(detail.run.id)}')),
      body: ListView(
        children: [
          AcornPageIntro(
            icon: Icons.bug_report_outlined,
            title: '$totalCount backend events',
            body: 'Diagnostics are separated from the product flow.',
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
          const SectionHeader(title: 'Event types'),
          for (final entry in counts.entries)
            AcornListRow(
              icon: Icons.tag_outlined,
              title: entry.key,
              subtitle: '${entry.value} event${entry.value == 1 ? '' : 's'}',
              tone: AcornStatusTone.neutral,
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

Map<String, int> _eventTypeCounts(Iterable<String> eventTypes) {
  final counts = <String, int>{};
  for (final type in eventTypes) {
    counts.update(type, (count) => count + 1, ifAbsent: () => 1);
  }
  final entries = counts.entries.toList()
    ..sort((left, right) {
      final countCompare = right.value.compareTo(left.value);
      if (countCompare != 0) {
        return countCompare;
      }
      return left.key.compareTo(right.key);
    });
  return Map<String, int>.fromEntries(entries);
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
