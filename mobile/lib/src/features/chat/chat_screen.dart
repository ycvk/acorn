import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/providers.dart';
import '../../ui/theme/acorn_theme.dart';
import '../../ui/widgets/acorn_status.dart';
import '../../ui/widgets/acorn_surfaces.dart';
import '../../ui/widgets/acorn_formatters.dart';
import '../../ui/widgets/empty_state.dart';
import '../../ui/widgets/message_widgets.dart';
import '../threads/thread_titles.dart';
import 'assistant_markdown.dart';
import 'chat_controller.dart';
import 'chat_models.dart';
import '../threads/threads_controller.dart';

class ChatScreen extends ConsumerStatefulWidget {
  const ChatScreen({super.key});

  @override
  ConsumerState<ChatScreen> createState() => _ChatScreenState();
}

class _ChatScreenState extends ConsumerState<ChatScreen> {
  static const double _autoFollowBottomTolerance = 220;

  final _composer = TextEditingController();
  final _focusNode = FocusNode();
  final _scrollController = ScrollController();
  ChatController? _listenedController;
  bool _autoScrollScheduled = false;
  bool _forceNextScrollToBottom = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      final controller = ref.read(chatControllerProvider);
      _listenedController = controller;
      controller.addListener(_scrollToBottom);
      unawaited(controller.loadActiveThread(force: true));
    });
  }

  @override
  void dispose() {
    _listenedController?.removeListener(_scrollToBottom);
    _composer.dispose();
    _focusNode.dispose();
    _scrollController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final chat = ref.watch(chatControllerProvider);
    final threadsLoading = ref.watch(
      threadsControllerProvider.select((controller) => controller.loading),
    );
    final threadsError = ref.watch(
      threadsControllerProvider.select((controller) => controller.errorMessage),
    );
    final thread = ref.watch(
      threadsControllerProvider.select((controller) => controller.activeThread),
    );
    final threadsController = ref.read(threadsControllerProvider);

    return GestureDetector(
      onTap: _focusNode.unfocus,
      child: Scaffold(
        appBar: AppBar(
          title: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(threadDisplayTitle(thread)),
              if (thread != null)
                Text(
                  shortId(thread.id),
                  style: Theme.of(context).textTheme.labelSmall?.copyWith(
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
                ),
            ],
          ),
          actions: [
            IconButton.filledTonal(
              tooltip: 'Refresh',
              icon: const Icon(Icons.refresh),
              onPressed: threadsLoading || chat.sending
                  ? null
                  : () => _refresh(chat),
            ),
            IconButton.filledTonal(
              tooltip: 'New thread',
              icon: const Icon(Icons.add_comment_outlined),
              onPressed: threadsLoading || chat.sending
                  ? null
                  : () => _startDraftThread(threadsController, chat),
            ),
          ],
        ),
        body: Column(
          children: [
            if (threadsError != null) ErrorBanner(message: threadsError),
            if (chat.errorMessage != null)
              ErrorBanner(message: chat.errorMessage!),
            Expanded(child: _buildBody(chat, threadsController)),
            _ChatInputBar(
              controller: _composer,
              focusNode: _focusNode,
              sending: chat.sending,
              onSend: () => _send(chat),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildBody(
    ChatController controller,
    ThreadsController threadsController,
  ) {
    final items = controller.chatItems;
    if (controller.loading && items.isEmpty) {
      return const Center(child: CircularProgressIndicator());
    }
    if (items.isEmpty) {
      return AcornEmptyState(
        icon: Icons.chat_bubble_outline,
        title: 'Start a thread',
        body:
            'Send the first message; Acorn will save the conversation on this server.',
        action: FilledButton.icon(
          onPressed: () => _startDraftThread(threadsController, controller),
          icon: const Icon(Icons.add),
          label: const Text('New thread'),
        ),
      );
    }

    return ListView.builder(
      controller: _scrollController,
      addRepaintBoundaries: false,
      cacheExtent: 640,
      findChildIndexCallback: (key) => _findChatItemIndex(items, key),
      keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
      padding: const EdgeInsets.fromLTRB(16, 10, 16, 14),
      itemCount: items.length,
      itemBuilder: (context, index) {
        final item = items[index];
        return RepaintBoundary(
          key: ValueKey(_chatItemIdentity(item)),
          child: _ChatItemView(item: item),
        );
      },
    );
  }

  Future<void> _send(ChatController controller) async {
    final text = _composer.text.trim();
    if (text.isEmpty) {
      return;
    }
    _forceNextScrollToBottom = true;
    HapticFeedback.lightImpact();
    _composer.clear();
    _focusNode.requestFocus();
    await controller.sendChatMessage(text);
  }

  Future<void> _refresh(ChatController chatController) async {
    await chatController.loadActiveThread(force: true);
  }

  Future<void> _startDraftThread(
    ThreadsController threadsController,
    ChatController chatController,
  ) async {
    threadsController.startDraftThread();
    await chatController.loadActiveThread(force: true);
  }

  void _scrollToBottom() {
    if (_autoScrollScheduled) {
      return;
    }
    _autoScrollScheduled = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _autoScrollScheduled = false;
      if (!_scrollController.hasClients) {
        _forceNextScrollToBottom = false;
        return;
      }
      final position = _scrollController.position;
      final distanceFromBottom = position.maxScrollExtent - position.pixels;
      final shouldFollow =
          _forceNextScrollToBottom ||
          distanceFromBottom <= _autoFollowBottomTolerance;
      _forceNextScrollToBottom = false;
      if (!shouldFollow) {
        return;
      }
      _scrollController.jumpTo(position.maxScrollExtent);
    });
  }
}

int? _findChatItemIndex(List<ChatItem> items, Key key) {
  if (key is! ValueKey<String>) {
    return null;
  }
  final value = key.value;
  for (var index = 0; index < items.length; index += 1) {
    if (_chatItemIdentity(items[index]) == value) {
      return index;
    }
  }
  return null;
}

String _chatItemIdentity(ChatItem item) {
  return '${item.kind.name}:${item.id}';
}

class _ChatItemView extends StatelessWidget {
  const _ChatItemView({required this.item});

  final ChatItem item;

  @override
  Widget build(BuildContext context) {
    return switch (item.kind) {
      ChatItemKind.message => _MessageBubble(item: item),
      ChatItemKind.activity => _ActivityRow(item: item),
    };
  }
}

class _MessageBubble extends StatelessWidget {
  const _MessageBubble({required this.item});

  final ChatItem item;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final isUser = item.isUser;
    final textColor = isUser ? colors.onPrimary : colors.onSurface;

    return GestureDetector(
      onLongPress: () {
        unawaited(_copyMessageText(context, item));
      },
      child: AcornMessageBubble(
        outbound: isUser,
        footer: item.isAssistant && item.status != ChatRunStatus.idle
            ? AcornMessageStatusFooter(
                label: _runStatusLabel(item.status),
                tone: _runStatusTone(item.status),
                foregroundColor: textColor,
              )
            : null,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            if (item.isAssistant && item.hasReasoning) ...[
              AcornThinkingSection(reasoning: item.reasoning),
              const SizedBox(height: 10),
            ],
            switch ((item.isStreaming, item.isAssistant)) {
              (true, _) when item.text.isEmpty => const AcornTypingIndicator(),
              (_, true) => AssistantMarkdown(
                text: item.text,
                textColor: textColor,
              ),
              _ => Text(
                item.text,
                style: TextStyle(color: textColor, fontSize: 15),
              ),
            },
          ],
        ),
      ),
    );
  }
}

Future<void> _copyMessageText(BuildContext context, ChatItem item) async {
  final parts = <String>[
    if (item.reasoning.trim().isNotEmpty) item.reasoning.trim(),
    if (item.text.trim().isNotEmpty) item.text.trim(),
  ];
  if (parts.isEmpty) {
    return;
  }
  await Clipboard.setData(ClipboardData(text: parts.join('\n\n')));
  if (!context.mounted) {
    return;
  }
  ScaffoldMessenger.of(
    context,
  ).showSnackBar(const SnackBar(content: Text('Copied message')));
}

class _ActivityRow extends StatelessWidget {
  const _ActivityRow({required this.item});

  final ChatItem item;

  @override
  Widget build(BuildContext context) {
    return AcornActivityRow(
      title: item.text,
      detail: item.detail,
      timestamp: formatTimestamp(item.createdAt),
    );
  }
}

class _ChatInputBar extends StatefulWidget {
  const _ChatInputBar({
    required this.controller,
    required this.focusNode,
    required this.sending,
    required this.onSend,
  });

  final TextEditingController controller;
  final FocusNode focusNode;
  final bool sending;
  final VoidCallback onSend;

  @override
  State<_ChatInputBar> createState() => _ChatInputBarState();
}

class _ChatInputBarState extends State<_ChatInputBar> {
  @override
  void initState() {
    super.initState();
    widget.controller.addListener(_onTextChanged);
  }

  @override
  void dispose() {
    widget.controller.removeListener(_onTextChanged);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final hasText = widget.controller.text.trim().isNotEmpty;
    return AcornBottomSurface(
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.end,
        children: [
          Expanded(
            child: TextField(
              controller: widget.controller,
              focusNode: widget.focusNode,
              enabled: !widget.sending,
              minLines: 1,
              maxLines: 5,
              textInputAction: TextInputAction.send,
              onSubmitted: widget.sending ? null : (_) => widget.onSend(),
              decoration: const InputDecoration(hintText: 'Message Acorn'),
            ),
          ),
          const SizedBox(width: 8),
          if (widget.sending)
            const SizedBox(
              width: 48,
              height: 48,
              child: Padding(
                padding: EdgeInsets.all(12),
                child: CircularProgressIndicator(strokeWidth: 2.4),
              ),
            )
          else
            IconButton.filled(
              tooltip: 'Send',
              onPressed: hasText ? widget.onSend : null,
              icon: const Icon(Icons.arrow_upward),
            ),
        ],
      ),
    );
  }

  void _onTextChanged() {
    setState(() {});
  }
}

String _runStatusLabel(ChatRunStatus status) {
  return switch (status) {
    ChatRunStatus.streaming => 'streaming',
    ChatRunStatus.completed => 'completed',
    ChatRunStatus.failed => 'failed',
    ChatRunStatus.interrupted => 'interrupted',
    ChatRunStatus.idle => '',
  };
}

AcornStatusTone _runStatusTone(ChatRunStatus status) {
  return switch (status) {
    ChatRunStatus.streaming => AcornStatusTone.info,
    ChatRunStatus.completed => AcornStatusTone.success,
    ChatRunStatus.failed => AcornStatusTone.error,
    ChatRunStatus.interrupted => AcornStatusTone.warning,
    ChatRunStatus.idle => AcornStatusTone.neutral,
  };
}
