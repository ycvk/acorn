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
import 'thread_titles.dart';

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
    final busy = ref.watch(
      threadsControllerProvider.select((controller) => controller.loading),
    );
    final errorMessage = ref.watch(
      threadsControllerProvider.select((controller) => controller.errorMessage),
    );
    final controller = ref.read(threadsControllerProvider);

    return Scaffold(
      appBar: AppBar(
        title: const Text('Threads'),
        actions: [
          IconButton.filledTonal(
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh),
            onPressed: busy ? null : controller.refresh,
          ),
          IconButton.filledTonal(
            tooltip: 'New thread',
            icon: const Icon(Icons.add),
            onPressed: busy
                ? null
                : () => _createThreadAndOpenChat(context, ref),
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: controller.refresh,
        child: threads.isEmpty
            ? ListView(
                children: [
                  if (errorMessage != null) ErrorBanner(message: errorMessage),
                  SizedBox(height: MediaQuery.of(context).size.height * 0.18),
                  AcornEmptyState(
                    icon: Icons.forum_outlined,
                    title: 'No threads yet',
                    body: 'Create a backend thread and start from Chat.',
                    action: FilledButton.icon(
                      onPressed: busy
                          ? null
                          : () => _createThreadAndOpenChat(context, ref),
                      icon: const Icon(Icons.add),
                      label: const Text('New thread'),
                    ),
                  ),
                ],
              )
            : ListView(
                children: [
                  if (errorMessage != null) ErrorBanner(message: errorMessage),
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
                      title: threadDisplayTitle(thread),
                      subtitle:
                          '${statusLabel(thread.state)} · ${formatTimestamp(thread.updatedAt)} · ${shortId(thread.id)}',
                      selected: activeThreadId == thread.id,
                      tone: activeThreadId == thread.id
                          ? AcornStatusTone.info
                          : AcornStatusTone.neutral,
                      trailing: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          IconButton(
                            tooltip: 'Delete thread',
                            icon: const Icon(Icons.delete_outline),
                            onPressed: busy
                                ? null
                                : () => _confirmDeleteThread(
                                    context,
                                    ref,
                                    thread,
                                  ),
                          ),
                          activeThreadId == thread.id
                              ? const Icon(Icons.check_circle)
                              : const Icon(Icons.chevron_right),
                        ],
                      ),
                      onTap: () => _openThread(context, ref, thread),
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
