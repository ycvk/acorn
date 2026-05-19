import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

import '../api/acorn_api.dart';
import '../api/run_event_stream.dart';
import '../features/chat/chat_models.dart';
import 'connection_profile.dart';
import 'connection_store.dart';

typedef AcornApiClientFactory =
    AcornApiClient Function({
      required String serverUrl,
      String? accessToken,
      http.Client? httpClient,
    });

typedef RunEventStreamClientFactory =
    RunEventStreamClient Function({
      required ConnectionProfile profile,
      http.Client? httpClient,
    });

AcornApiClient _defaultApiClientFactory({
  required String serverUrl,
  String? accessToken,
  http.Client? httpClient,
}) {
  return AcornApiClient(
    serverUrl: serverUrl,
    accessToken: accessToken,
    httpClient: httpClient,
  );
}

RunEventStreamClient _defaultStreamClientFactory({
  required ConnectionProfile profile,
  http.Client? httpClient,
}) {
  return RunEventStreamClient(profile: profile, httpClient: httpClient);
}

class AcornController extends ChangeNotifier {
  AcornController({
    required ConnectionStore connectionStore,
    AcornApiClientFactory apiClientFactory = _defaultApiClientFactory,
    RunEventStreamClientFactory streamClientFactory =
        _defaultStreamClientFactory,
  }) : _connectionStore = connectionStore,
       _apiClientFactory = apiClientFactory,
       _streamClientFactory = streamClientFactory;

  final ConnectionStore _connectionStore;
  final AcornApiClientFactory _apiClientFactory;
  final RunEventStreamClientFactory _streamClientFactory;

  ConnectionProfile? _profile;
  AcornApiClient? _api;
  RunEventStreamClient? _stream;

  bool initializing = true;
  bool busy = false;
  bool sending = false;
  int selectedTab = 0;
  String? errorMessage;
  InboxResponse? inbox;
  List<Thread> threads = const [];
  Thread? activeThread;
  List<ChatItem> chatItems = const [];
  PendingActionDetail? pendingActionDetail;

  ConnectionProfile? get profile => _profile;
  AcornApiClient get api {
    final client = _api;
    if (_profile == null || client == null) {
      throw const AcornApiException(
        401,
        'not_connected',
        'Connect to an Acorn server first.',
      );
    }
    return client;
  }

  Future<void> boot() async {
    try {
      _setProfile(await _connectionStore.load());
      if (_profile != null) {
        await refreshAll(selectFirstThread: true);
      }
    } catch (error) {
      errorMessage = _errorText(error);
    } finally {
      initializing = false;
      notifyListeners();
    }
  }

  Future<void> pair({
    required String serverUrl,
    required String pairingCode,
    required String deviceName,
    required String platform,
  }) async {
    await _runBusy(() async {
      final temporary = _apiClientFactory(serverUrl: serverUrl);
      final PairDeviceResponse result;
      try {
        result = await temporary.pairDevice(
          PairDeviceRequest(
            pairingCode: pairingCode.trim(),
            deviceName: deviceName.trim(),
            platform: platform,
          ),
        );
      } finally {
        temporary.close();
      }
      final next = ConnectionProfile(
        serverUrl: serverUrl.trim(),
        deviceId: result.device.deviceId,
        accessToken: result.accessToken,
      );
      await _connectionStore.save(next);
      _setProfile(next);
      await refreshAll(selectFirstThread: true);
    });
  }

  Future<void> disconnect() async {
    await _connectionStore.clear();
    _setProfile(null);
    inbox = null;
    threads = const [];
    activeThread = null;
    chatItems = const [];
    pendingActionDetail = null;
    selectedTab = 0;
    errorMessage = null;
    notifyListeners();
  }

  void selectTab(int index) {
    selectedTab = index;
    notifyListeners();
  }

  Future<void> refreshAll({bool selectFirstThread = false}) async {
    await _runBusy(() async {
      final nextInbox = await api.getInbox();
      final nextThreads = await api.listThreads();
      inbox = nextInbox;
      threads = nextThreads.items;

      final current = activeThread;
      if (current != null) {
        final updated = _findThread(current.id);
        if (updated != null) {
          await selectThread(updated, notify: false);
          return;
        }
      }

      if (selectFirstThread && threads.isNotEmpty) {
        await selectThread(threads.first, notify: false);
      }
    });
  }

  Future<void> createThreadAndSelect() async {
    await _runBusy(() async {
      final thread = await api.createThread();
      threads = [thread, ...threads];
      await selectThread(thread, notify: false);
      selectedTab = 0;
    });
  }

  Future<void> deleteThread(Thread thread) async {
    await _runBusy(() async {
      await api.deleteThread(thread.id);
      threads = threads
          .where((candidate) => candidate.id != thread.id)
          .toList(growable: false);
      if (activeThread?.id == thread.id) {
        activeThread = null;
        chatItems = const [];
      }
      if (pendingActionDetail?.threadId == thread.id) {
        pendingActionDetail = null;
      }
      await _refreshInboxOnly();
    });
  }

  Future<void> selectThread(Thread thread, {bool notify = true}) async {
    final response = await api.listMessages(thread.id);
    activeThread = thread;
    chatItems = chatItemsFromMessages(response.items);
    selectedTab = 0;
    if (notify) {
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
      var thread = activeThread;
      if (thread == null) {
        thread = await api.createThread();
        activeThread = thread;
        threads = [thread, ...threads];
      }

      final userMessage = await api.createMessage(thread.id, trimmed);
      chatItems = [
        ...chatItems,
        ...chatItemsFromMessages([userMessage]),
      ];
      notifyListeners();

      final run = await api.createRun(thread.id, mode: 'direct_response');
      _appendLiveAssistant(run);
      await _followRun(run);
      await _reloadActiveThreadMessages();
      await _refreshInboxOnly();
    } catch (error) {
      errorMessage = _errorText(error);
      _markStreamingAssistantFailed();
    } finally {
      sending = false;
      notifyListeners();
    }
  }

  Future<void> loadPendingAction(String actionId) async {
    await _runBusy(() async {
      pendingActionDetail = await api.getPendingAction(actionId);
    });
  }

  Future<void> decidePendingAction(String actionId, String decision) async {
    await _runBusy(() async {
      await api.decidePendingAction(actionId, decision);
      pendingActionDetail = null;
      await _refreshInboxOnly();
    });
  }

  Future<RunDetail> loadRunDetail(String runId) {
    return api.getRunDetail(runId);
  }

  Future<void> _followRun(Run run) async {
    final stream = _stream;
    if (stream == null) {
      throw const AcornApiException(
        401,
        'not_connected',
        'Connect to an Acorn server first.',
      );
    }
    var sawTerminalEvent = false;
    await for (final event in stream.followRunEvents(run.id)) {
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

  Future<void> _reloadActiveThreadMessages() async {
    final thread = activeThread;
    if (thread == null) {
      return;
    }
    final response = await api.listMessages(thread.id);
    final persisted = chatItemsFromMessages(response.items);
    if (persisted.isNotEmpty) {
      chatItems = mergePersistedChatItemsWithLiveRunFeedback(
        persisted: persisted,
        live: chatItems,
      );
    }
  }

  Future<void> _refreshInboxOnly() async {
    inbox = await api.getInbox();
  }

  Future<void> _runBusy(Future<void> Function() action) async {
    busy = true;
    errorMessage = null;
    notifyListeners();
    try {
      await action();
    } catch (error) {
      errorMessage = _errorText(error);
    } finally {
      busy = false;
      notifyListeners();
    }
  }

  Thread? _findThread(String id) {
    for (final thread in threads) {
      if (thread.id == id) {
        return thread;
      }
    }
    return null;
  }

  void _setProfile(ConnectionProfile? profile) {
    _api?.close();
    _stream?.close();
    _profile = profile;
    _api = profile == null
        ? null
        : _apiClientFactory(
            serverUrl: profile.serverUrl,
            accessToken: profile.accessToken,
          );
    _stream = profile == null ? null : _streamClientFactory(profile: profile);
  }

  String _errorText(Object error) {
    if (error is AcornApiException) {
      return error.message.isEmpty ? error.code : error.message;
    }
    if (error is RunEventStreamException) {
      return error.message;
    }
    if (error is FormatException) {
      return error.message;
    }
    return error.toString();
  }

  @override
  void dispose() {
    _api?.close();
    _stream?.close();
    super.dispose();
  }
}

String _appendAssistantReasoning(String current, String delta) {
  if (current.isEmpty) {
    return delta;
  }
  return '$current$delta';
}
