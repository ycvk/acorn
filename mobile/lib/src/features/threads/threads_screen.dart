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

class ThreadsScreen extends ConsumerWidget {
  const ThreadsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final controller = ref.watch(acornControllerProvider);
    final threads = controller.threads;

    return Scaffold(
      appBar: AppBar(
        title: const Text('Threads'),
        actions: [
          IconButton.filledTonal(
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh),
            onPressed: controller.busy ? null : controller.refreshAll,
          ),
          IconButton.filledTonal(
            tooltip: 'New thread',
            icon: const Icon(Icons.add),
            onPressed: controller.busy
                ? null
                : controller.createThreadAndSelect,
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: controller.refreshAll,
        child: threads.isEmpty
            ? ListView(
                children: [
                  if (controller.errorMessage != null)
                    ErrorBanner(message: controller.errorMessage!),
                  SizedBox(height: MediaQuery.of(context).size.height * 0.18),
                  AcornEmptyState(
                    icon: Icons.forum_outlined,
                    title: 'No threads yet',
                    body: 'Create a backend thread and start from Chat.',
                    action: FilledButton.icon(
                      onPressed: controller.createThreadAndSelect,
                      icon: const Icon(Icons.add),
                      label: const Text('New thread'),
                    ),
                  ),
                ],
              )
            : ListView(
                children: [
                  if (controller.errorMessage != null)
                    ErrorBanner(message: controller.errorMessage!),
                  AcornPageIntro(
                    icon: Icons.forum_outlined,
                    title: '${threads.length} backend threads',
                    body: 'Select a thread to continue the persisted run.',
                    tone: AcornStatusTone.info,
                  ),
                  const SectionHeader(title: 'Recent'),
                  for (final thread in threads)
                    AcornListRow(
                      icon: Icons.forum_outlined,
                      title: thread.title.isEmpty
                          ? 'Untitled thread'
                          : thread.title,
                      subtitle:
                          '${statusLabel(thread.state)} · ${formatTimestamp(thread.updatedAt)} · ${shortId(thread.id)}',
                      selected: controller.activeThread?.id == thread.id,
                      tone: controller.activeThread?.id == thread.id
                          ? AcornStatusTone.info
                          : AcornStatusTone.neutral,
                      trailing: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          IconButton(
                            tooltip: 'Delete thread',
                            icon: const Icon(Icons.delete_outline),
                            onPressed: controller.busy
                                ? null
                                : () => _confirmDeleteThread(
                                    context,
                                    ref,
                                    thread,
                                  ),
                          ),
                          controller.activeThread?.id == thread.id
                              ? const Icon(Icons.check_circle)
                              : const Icon(Icons.chevron_right),
                        ],
                      ),
                      onTap: () => controller.selectThread(thread),
                    ),
                ],
              ),
      ),
    );
  }

  Future<void> _confirmDeleteThread(
    BuildContext context,
    WidgetRef ref,
    Thread thread,
  ) async {
    final colors = Theme.of(context).colorScheme;
    final title = thread.title.isEmpty ? 'Untitled thread' : thread.title;
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
    await ref.read(acornControllerProvider).deleteThread(thread);
  }
}
