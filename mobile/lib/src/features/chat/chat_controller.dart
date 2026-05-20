import 'package:flutter/foundation.dart';

import '../../api/acorn_api.dart';
import '../../api/run_event_stream.dart';
import '../../core/connection_controller.dart';
import '../inbox/inbox_controller.dart';
import '../threads/threads_controller.dart';
import 'chat_models.dart';

class ChatController extends ChangeNotifier {
  ChatController({
    required ConnectionController connectionController,
    required ThreadsController threadsController,
    required InboxController inboxController,
  }) : _connectionController = connectionController,
       _threadsController = threadsController,
       _inboxController = inboxController;

  final ConnectionController _connectionController;
  final ThreadsController _threadsController;
  final InboxController _inboxController;

  bool loading = false;
  bool sending = false;
  String? errorMessage;
  Thread? thread;
  List<ChatItem> chatItems = const [];

  Future<void> loadActiveThread({bool force = false}) async {
    final activeThread = _threadsController.activeThread;
    if (activeThread == null) {
      if (thread == null && chatItems.isEmpty && errorMessage == null) {
        return;
      }
      thread = null;
      chatItems = const [];
      errorMessage = null;
      notifyListeners();
      return;
    }
    if (!force && thread?.id == activeThread.id) {
      thread = activeThread;
      notifyListeners();
      return;
    }

    loading = true;
    errorMessage = null;
    notifyListeners();
    try {
      final response = await _connectionController.api.listMessages(
        activeThread.id,
      );
      thread = activeThread;
      chatItems = chatItemsFromMessages(response.items);
    } catch (error) {
      errorMessage = acornUserFacingErrorText(error);
    } finally {
      loading = false;
      notifyListeners();
    }
  }

  Future<void> sendChatMessage(String text) async {
    final trimmed = text.trim();
    if (trimmed.isEmpty || sending) {
      return;
    }

    sending = true;
    errorMessage = null;
    notifyListeners();

    try {
      final activeThread = await _threadsController.ensureActiveThread();
      thread = activeThread;

      final userMessage = await _connectionController.api.createMessage(
        activeThread.id,
        trimmed,
      );
      chatItems = [
        ...chatItems,
        ...chatItemsFromMessages([userMessage]),
      ];
      notifyListeners();
      await _threadsController.refresh();
      final refreshedThread = _threadsController.activeThread;
      if (refreshedThread != null && refreshedThread.id == activeThread.id) {
        thread = refreshedThread;
      }

      final run = await _connectionController.api.createRun(
        activeThread.id,
        mode: 'direct_response',
      );
      _appendLiveAssistant(run);
      await _followRun(run);
      await _reloadThreadMessages(activeThread.id);
      await _inboxController.refresh();
    } catch (error) {
      errorMessage = acornUserFacingErrorText(error);
      _markStreamingAssistantFailed();
    } finally {
      sending = false;
      notifyListeners();
    }
  }

  Future<void> _followRun(Run run) async {
    var sawTerminalEvent = false;
    await for (final event in _connectionController.followRunEvents(run.id)) {
      _applyRunEvent(event);
      notifyListeners();
      if (isTerminalRunEvent(event)) {
        sawTerminalEvent = true;
        break;
      }
    }
    if (!sawTerminalEvent) {
      throw const RunEventStreamException(
        'Run event stream closed before a terminal run event.',
      );
    }
  }

  void _applyRunEvent(RunEvent event) {
    final delta = assistantDeltaText(event);
    final reasoningDelta = assistantDeltaReasoning(event);
    if (delta != null || reasoningDelta != null) {
      _updateAssistantForRun(
        event.runId,
        (item) => item.copyWith(
          text: delta == null ? item.text : '${item.text}$delta',
          reasoning: reasoningDelta == null
              ? item.reasoning
              : _appendAssistantReasoning(item.reasoning, reasoningDelta),
        ),
      );
      return;
    }

    final messageText = agentMessageText(event);
    final messageReasoning = agentMessageReasoning(event);
    if (messageText != null || messageReasoning != null) {
      _updateAssistantForRun(
        event.runId,
        (item) => item.copyWith(
          text: messageText ?? item.text,
          reasoning: messageReasoning ?? item.reasoning,
          status: statusFromTerminalEvent(event),
        ),
      );
      return;
    }

    if (isTerminalRunEvent(event)) {
      _updateAssistantForRun(
        event.runId,
        (item) => item.copyWith(status: statusFromTerminalEvent(event)),
      );
      if (event.type != 'run.completed') {
        final activity = activityFromEvent(event);
        if (activity != null) {
          chatItems = [...chatItems, activity];
        }
      }
      return;
    }

    if (event.type == 'run.started') {
      return;
    }
    final activity = activityFromEvent(event);
    if (activity != null) {
      chatItems = [...chatItems, activity];
    }
  }

  void _appendLiveAssistant(Run run) {
    chatItems = [
      ...chatItems,
      ChatItem.message(
        id: 'assistant:${run.id}',
        role: ChatRole.assistant,
        text: '',
        createdAt: run.createdAt,
        runId: run.id,
        status: ChatRunStatus.streaming,
      ),
    ];
    notifyListeners();
  }

  void _updateAssistantForRun(
    String runId,
    ChatItem Function(ChatItem item) update,
  ) {
    final index = chatItems.lastIndexWhere(
      (item) => item.kind == ChatItemKind.message && item.runId == runId,
    );
    if (index < 0) {
      chatItems = [
        ...chatItems,
        update(
          ChatItem.message(
            id: 'assistant:$runId',
            role: ChatRole.assistant,
            text: '',
            createdAt: DateTime.now().toUtc().toIso8601String(),
            runId: runId,
            status: ChatRunStatus.streaming,
          ),
        ),
      ];
      return;
    }
    final next = [...chatItems];
    next[index] = update(next[index]);
    chatItems = next;
  }

  void _markStreamingAssistantFailed() {
    final index = chatItems.lastIndexWhere(
      (item) => item.kind == ChatItemKind.message && item.isStreaming,
    );
    if (index < 0) {
      return;
    }
    final next = [...chatItems];
    next[index] = next[index].copyWith(status: ChatRunStatus.failed);
    chatItems = next;
  }

  Future<void> _reloadThreadMessages(String threadId) async {
    final response = await _connectionController.api.listMessages(threadId);
    final persisted = chatItemsFromMessages(response.items);
    if (persisted.isNotEmpty) {
      chatItems = mergePersistedChatItemsWithLiveRunFeedback(
        persisted: persisted,
        live: chatItems,
      );
    }
  }

  void clear() {
    loading = false;
    sending = false;
    errorMessage = null;
    thread = null;
    chatItems = const [];
    notifyListeners();
  }
}

String _appendAssistantReasoning(String current, String delta) {
  if (current.isEmpty) {
    return delta;
  }
  return '$current$delta';
}
