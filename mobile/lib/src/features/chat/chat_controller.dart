import 'dart:async';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

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
    Duration runReconnectDelay = const Duration(seconds: 1),
  }) : _connectionController = connectionController,
       _threadsController = threadsController,
       _inboxController = inboxController,
       _runReconnectDelay = runReconnectDelay;

  final ConnectionController _connectionController;
  final ThreadsController _threadsController;
  final InboxController _inboxController;
  final Duration _runReconnectDelay;

  bool loading = false;
  bool sending = false;
  String? errorMessage;
  String? noticeMessage;
  Thread? thread;
  List<ChatItem> chatItems = const [];

  Future<void> loadActiveThread({bool force = false}) async {
    final activeThread = _threadsController.activeThread;
    if (activeThread == null) {
      if (thread == null &&
          chatItems.isEmpty &&
          errorMessage == null &&
          noticeMessage == null) {
        return;
      }
      thread = null;
      chatItems = const [];
      errorMessage = null;
      noticeMessage = null;
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
    noticeMessage = null;
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
    noticeMessage = null;
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
      noticeMessage = null;
      await _reloadThreadMessages(activeThread.id);
      await _inboxController.refresh();
    } catch (error) {
      noticeMessage = null;
      errorMessage = acornUserFacingErrorText(error);
      _markStreamingAssistantFailed();
    } finally {
      sending = false;
      notifyListeners();
    }
  }

  Future<void> _followRun(Run run) async {
    var sawTerminalEvent = false;
    var lastSeq = 0;
    while (!sawTerminalEvent) {
      try {
        await for (final event in _connectionController.followRunEvents(
          run.id,
          afterSeq: lastSeq,
        )) {
          if (event.seq > lastSeq) {
            lastSeq = event.seq;
          }
          noticeMessage = null;
          _applyRunEvent(event);
          notifyListeners();
          if (isTerminalRunEvent(event)) {
            sawTerminalEvent = true;
            break;
          }
        }
        if (!sawTerminalEvent) {
          await _verifyRunStillRunning(run.id, lastSeq);
          await _markRunReconnectingAndPause(run.id, lastSeq);
        }
      } catch (error) {
        if (!_isRecoverableRunEventStreamError(error)) {
          rethrow;
        }
        await _markRunReconnectingAndPause(run.id, lastSeq);
      }
    }
  }

  Future<void> _verifyRunStillRunning(String runId, int lastSeq) async {
    try {
      final current = await _connectionController.api.getRun(runId);
      if (current.status == 'running') {
        return;
      }
      throw RunEventStreamException(
        'Run event stream closed before a terminal event; '
        'server status is ${current.status} after seq $lastSeq.',
      );
    } catch (error) {
      if (_isRecoverableRunEventStreamError(error)) {
        return;
      }
      rethrow;
    }
  }

  Future<void> _markRunReconnectingAndPause(String runId, int lastSeq) async {
    _updateCurrentAssistantForRun(
      runId,
      (item) => item.copyWith(status: ChatRunStatus.reconnecting),
    );
    noticeMessage =
        'Connection interrupted. Reconnecting from event #$lastSeq.';
    notifyListeners();
    if (_runReconnectDelay > Duration.zero) {
      await Future<void>.delayed(_runReconnectDelay);
    }
  }

  void _applyRunEvent(RunEvent event) {
    final delta = assistantDeltaText(event);
    final reasoningDelta = assistantDeltaReasoning(event);
    if (delta != null || reasoningDelta != null) {
      final segmentID =
          assistantDeltaMessageId(event) ??
          _currentAssistantSegmentIDForRun(event.runId) ??
          'event:${event.eventId}';
      _updateAssistantSegment(
        event.runId,
        segmentID,
        event.ts,
        (item) => item.copyWith(
          text: delta == null ? item.text : '${item.text}$delta',
          reasoning: reasoningDelta == null
              ? item.reasoning
              : _appendAssistantReasoning(item.reasoning, reasoningDelta),
          status: ChatRunStatus.streaming,
        ),
      );
      return;
    }

    final messageText = agentMessageText(event);
    final messageReasoning = agentMessageReasoning(event);
    if (messageText != null || messageReasoning != null) {
      ChatItem updateAssistantMessage(ChatItem item) => item.copyWith(
        text: messageText ?? item.text,
        reasoning: messageReasoning ?? item.reasoning,
        status: statusFromTerminalEvent(event),
      );
      final messageID = agentMessageId(event);
      if (messageID == null) {
        _updateCurrentAssistantForRun(event.runId, updateAssistantMessage);
      } else {
        _updateAssistantSegment(
          event.runId,
          messageID,
          event.ts,
          updateAssistantMessage,
        );
      }
      return;
    }

    if (isTerminalRunEvent(event)) {
      _updateCurrentAssistantForRun(
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
        id: _pendingAssistantItemID(run.id),
        role: ChatRole.assistant,
        text: '',
        createdAt: run.createdAt,
        runId: run.id,
        status: ChatRunStatus.streaming,
      ),
    ];
    notifyListeners();
  }

  void _updateAssistantSegment(
    String runId,
    String segmentID,
    String createdAt,
    ChatItem Function(ChatItem item) update,
  ) {
    final itemID = _assistantItemID(runId, segmentID);
    final normalized = _normalizeAssistantRunItems(
      runId,
      currentItemID: itemID,
    );
    final index = normalized.lastIndexWhere((item) => item.id == itemID);
    if (index < 0) {
      final pendingIndex = normalized.lastIndexWhere(
        (item) =>
            item.id == _pendingAssistantItemID(runId) &&
            item.text.trim().isEmpty &&
            item.reasoning.trim().isEmpty,
      );
      if (pendingIndex >= 0) {
        final next = [...normalized];
        next[pendingIndex] = update(
          next[pendingIndex].copyWith(
            id: itemID,
            status: ChatRunStatus.streaming,
          ),
        );
        chatItems = next;
        return;
      }
      chatItems = [
        ...normalized,
        update(
          ChatItem.message(
            id: itemID,
            role: ChatRole.assistant,
            text: '',
            createdAt: createdAt,
            runId: runId,
            status: ChatRunStatus.streaming,
          ),
        ),
      ];
      return;
    }
    final next = [...normalized];
    next[index] = update(next[index]);
    chatItems = next;
  }

  void _updateCurrentAssistantForRun(
    String runId,
    ChatItem Function(ChatItem item) update,
  ) {
    final index = _currentAssistantIndexForRun(runId, chatItems);
    if (index < 0) {
      chatItems = [
        ...chatItems,
        update(
          ChatItem.message(
            id: _pendingAssistantItemID(runId),
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

  List<ChatItem> _normalizeAssistantRunItems(
    String runId, {
    required String currentItemID,
  }) {
    return chatItems
        .map((item) {
          if (item.kind != ChatItemKind.message ||
              item.runId != runId ||
              item.id == currentItemID ||
              !item.isStreaming) {
            return item;
          }
          return item.copyWith(status: ChatRunStatus.idle);
        })
        .toList(growable: false);
  }

  int _currentAssistantIndexForRun(String runId, List<ChatItem> items) {
    final streamingIndex = items.lastIndexWhere(
      (item) =>
          item.kind == ChatItemKind.message &&
          item.runId == runId &&
          item.isStreaming,
    );
    if (streamingIndex >= 0) {
      return streamingIndex;
    }
    return items.lastIndexWhere(
      (item) => item.kind == ChatItemKind.message && item.runId == runId,
    );
  }

  String? _currentAssistantSegmentIDForRun(String runId) {
    final index = _currentAssistantIndexForRun(runId, chatItems);
    if (index < 0) {
      return null;
    }
    final prefix = 'assistant:$runId:';
    final id = chatItems[index].id;
    if (!id.startsWith(prefix)) {
      return null;
    }
    return id.substring(prefix.length);
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
    noticeMessage = null;
    thread = null;
    chatItems = const [];
    notifyListeners();
  }
}

bool _isRecoverableRunEventStreamError(Object error) {
  return error is http.ClientException ||
      error is SocketException ||
      error is TimeoutException;
}

String _assistantItemID(String runId, String segmentID) {
  return 'assistant:$runId:$segmentID';
}

String _pendingAssistantItemID(String runId) {
  return _assistantItemID(runId, 'pending');
}

String _appendAssistantReasoning(String current, String delta) {
  if (current.isEmpty) {
    return delta;
  }
  return '$current$delta';
}
