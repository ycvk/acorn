import 'package:acorn_mobile/src/api/acorn_api.dart';
import 'package:acorn_mobile/src/api/run_event_stream.dart';
import 'package:acorn_mobile/src/core/connection_controller.dart';
import 'package:acorn_mobile/src/core/connection_store.dart';
import 'package:acorn_mobile/src/features/chat/chat_controller.dart';
import 'package:acorn_mobile/src/features/chat/chat_models.dart';
import 'package:acorn_mobile/src/features/inbox/inbox_controller.dart';
import 'package:acorn_mobile/src/features/threads/threads_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

void main() {
  test('chat controller owns streaming state outside app controller', () async {
    final api = _FakeChatApi();
    final app = _FakeConnectionController(api);
    final inbox = InboxController(connectionController: app);
    final threads = ThreadsController(connectionController: app)
      ..activeThread = _thread('');
    final chat = ChatController(
      connectionController: app,
      threadsController: threads,
      inboxController: inbox,
    );
    addTearDown(app.dispose);
    addTearDown(inbox.dispose);
    addTearDown(threads.dispose);
    addTearDown(chat.dispose);

    await chat.loadActiveThread(force: true);
    expect(chat.chatItems, isEmpty);

    await chat.sendChatMessage('hello');

    expect(chat.sending, isFalse);
    expect(chat.errorMessage, isNull);
    expect(threads.threads.single.title, 'Backend title');
    expect(inbox.inbox, isNotNull);
    expect(chat.thread?.title, 'Backend title');
    expect(chat.chatItems.map((item) => item.text), ['hello', 'done']);
    expect(chat.chatItems.last.status, ChatRunStatus.completed);
  });

  test(
    'draft chat creates backend thread on first message and adopts title',
    () async {
      final api = _FakeChatApi();
      final app = _FakeConnectionController(api);
      final inbox = InboxController(connectionController: app);
      final threads = ThreadsController(connectionController: app);
      final chat = ChatController(
        connectionController: app,
        threadsController: threads,
        inboxController: inbox,
      );
      addTearDown(app.dispose);
      addTearDown(inbox.dispose);
      addTearDown(threads.dispose);
      addTearDown(chat.dispose);

      threads.startDraftThread();
      await chat.sendChatMessage('first real request');

      expect(api.createThreadCount, 1);
      expect(threads.threads.single.title, 'Backend title');
      expect(chat.thread?.title, 'Backend title');
      expect(chat.chatItems.map((item) => item.text), [
        'first real request',
        'done',
      ]);
    },
  );

  test('reconnects run stream from the last observed sequence', () async {
    final api = _FakeChatApi();
    final app = _FakeConnectionController(
      api,
      followEvents: (runId, afterSeq) async* {
        if (afterSeq == 0) {
          yield _runEvent(runId, 'event_delta_1', 1, 'assistant.delta', {
            'assistant_delta': {'delta': 'wor'},
          });
          throw http.ClientException('Connection closed while receiving data');
        }
        if (afterSeq == 1) {
          yield _runEvent(runId, 'event_delta_2', 2, 'assistant.delta', {
            'assistant_delta': {'delta': 'king'},
          });
          yield _runEvent(runId, 'event_completed', 3, 'run.completed', {
            'message': {'role': 'assistant', 'content': 'done'},
          });
          return;
        }
        throw StateError('unexpected afterSeq $afterSeq');
      },
    );
    final inbox = InboxController(connectionController: app);
    final threads = ThreadsController(connectionController: app)
      ..activeThread = _thread('');
    final chat = ChatController(
      connectionController: app,
      threadsController: threads,
      inboxController: inbox,
      runReconnectDelay: Duration.zero,
    );
    final statuses = <ChatRunStatus>[];
    chat.addListener(() {
      final assistant = _lastAssistant(chat.chatItems);
      if (assistant != null) {
        statuses.add(assistant.status);
      }
    });
    addTearDown(app.dispose);
    addTearDown(inbox.dispose);
    addTearDown(threads.dispose);
    addTearDown(chat.dispose);

    await chat.sendChatMessage('hello');

    expect(app.afterSeqs, [0, 1]);
    expect(statuses, contains(ChatRunStatus.reconnecting));
    expect(chat.noticeMessage, isNull);
    expect(chat.errorMessage, isNull);
    expect(chat.chatItems.last.text, 'done');
    expect(chat.chatItems.last.status, ChatRunStatus.completed);
  });

  test(
    'renders each assistant stream message as its own transcript segment',
    () async {
      final api = _FakeChatApi();
      final app = _FakeConnectionController(
        api,
        followEvents: (runId, afterSeq) async* {
          yield _runEvent(runId, 'event_m0_reasoning', 1, 'assistant.delta', {
            'assistant_delta': {
              'message_id': '$runId:assistant:0',
              'sequence': 1,
              'reasoning': 'thinking about first tool',
            },
          });
          yield _runEvent(runId, 'event_m0_text', 2, 'assistant.delta', {
            'assistant_delta': {
              'message_id': '$runId:assistant:0',
              'sequence': 2,
              'delta': 'First pass',
            },
          });
          yield _runEvent(runId, 'event_m0_final', 3, 'agent.message', {
            'message': {'role': 'assistant', 'content': 'First pass'},
          });
          yield _runEvent(runId, 'event_m1_reasoning', 4, 'assistant.delta', {
            'assistant_delta': {
              'message_id': '$runId:assistant:1',
              'sequence': 1,
              'reasoning': 'thinking after tool output',
            },
          });
          yield _runEvent(runId, 'event_m1_text', 5, 'assistant.delta', {
            'assistant_delta': {
              'message_id': '$runId:assistant:1',
              'sequence': 2,
              'delta': 'Second pass',
            },
          });
          yield _runEvent(runId, 'event_completed', 6, 'run.completed', {
            'message': {'role': 'assistant', 'content': 'Second pass'},
          });
        },
      );
      final inbox = InboxController(connectionController: app);
      final threads = ThreadsController(connectionController: app)
        ..activeThread = _thread('');
      final chat = ChatController(
        connectionController: app,
        threadsController: threads,
        inboxController: inbox,
        runReconnectDelay: Duration.zero,
      );
      addTearDown(app.dispose);
      addTearDown(inbox.dispose);
      addTearDown(threads.dispose);
      addTearDown(chat.dispose);

      await chat.sendChatMessage('hello');

      final assistantItems = chat.chatItems
          .where((item) => item.isAssistant)
          .toList(growable: false);
      expect(assistantItems.map((item) => item.text), [
        'First pass',
        'Second pass',
      ]);
      expect(assistantItems.map((item) => item.reasoning), [
        'thinking about first tool',
        'thinking after tool output',
      ]);
      expect(assistantItems.first.status, ChatRunStatus.idle);
      expect(assistantItems.last.status, ChatRunStatus.completed);
    },
  );

  test('reconnects when run stream closes before terminal event', () async {
    final api = _FakeChatApi();
    final app = _FakeConnectionController(
      api,
      followEvents: (runId, afterSeq) async* {
        if (afterSeq == 0) {
          yield _runEvent(runId, 'event_delta_1', 1, 'assistant.delta', {
            'assistant_delta': {'delta': 'partial'},
          });
          return;
        }
        if (afterSeq == 1) {
          yield _runEvent(runId, 'event_completed', 2, 'run.completed', {
            'message': {'role': 'assistant', 'content': 'done'},
          });
          return;
        }
        throw StateError('unexpected afterSeq $afterSeq');
      },
    );
    final inbox = InboxController(connectionController: app);
    final threads = ThreadsController(connectionController: app)
      ..activeThread = _thread('');
    final chat = ChatController(
      connectionController: app,
      threadsController: threads,
      inboxController: inbox,
      runReconnectDelay: Duration.zero,
    );
    addTearDown(app.dispose);
    addTearDown(inbox.dispose);
    addTearDown(threads.dispose);
    addTearDown(chat.dispose);

    await chat.sendChatMessage('hello');

    expect(app.afterSeqs, [0, 1]);
    expect(chat.errorMessage, isNull);
    expect(chat.chatItems.last.text, 'done');
    expect(chat.chatItems.last.status, ChatRunStatus.completed);
  });

  test('does not reconnect on RunEvent contract errors', () async {
    final api = _FakeChatApi();
    final app = _FakeConnectionController(
      api,
      followEvents: (runId, afterSeq) async* {
        throw const RunEventStreamException('RunEvent SSE id mismatch.');
      },
    );
    final inbox = InboxController(connectionController: app);
    final threads = ThreadsController(connectionController: app)
      ..activeThread = _thread('');
    final chat = ChatController(
      connectionController: app,
      threadsController: threads,
      inboxController: inbox,
      runReconnectDelay: Duration.zero,
    );
    addTearDown(app.dispose);
    addTearDown(inbox.dispose);
    addTearDown(threads.dispose);
    addTearDown(chat.dispose);

    await chat.sendChatMessage('hello');

    expect(app.afterSeqs, [0]);
    expect(chat.noticeMessage, isNull);
    expect(chat.errorMessage, 'RunEvent SSE id mismatch.');
    expect(chat.chatItems.last.status, ChatRunStatus.failed);
  });
}

typedef _FollowEvents = Stream<RunEvent> Function(String runId, int afterSeq);

class _FakeConnectionController extends ConnectionController {
  _FakeConnectionController(this._api, {_FollowEvents? followEvents})
    : _followEvents = followEvents,
      super(connectionStore: MemoryConnectionStore());

  final _FakeChatApi _api;
  final _FollowEvents? _followEvents;
  final List<int> afterSeqs = [];

  @override
  AcornApiClient get api => _api;

  @override
  Stream<RunEvent> followRunEvents(String runId, {int afterSeq = 0}) {
    afterSeqs.add(afterSeq);
    final followEvents = _followEvents;
    if (followEvents != null) {
      return followEvents(runId, afterSeq);
    }
    return Stream<RunEvent>.fromIterable([
      _runEvent(runId, 'event_completed', 1, 'run.completed', {
        'message': {'role': 'assistant', 'content': 'done'},
      }),
    ]);
  }
}

class _FakeChatApi extends AcornApiClient {
  _FakeChatApi() : super(serverUrl: 'http://acorn.local', accessToken: 'token');

  final List<Message> messages = [];
  int createThreadCount = 0;

  @override
  Future<InboxResponse> getInbox() {
    return Future.value(_inbox());
  }

  @override
  Future<ThreadListResponse> listThreads({int limit = 40}) {
    return Future.value(ThreadListResponse(items: [_thread('Backend title')]));
  }

  @override
  Future<Thread> createThread({String? title}) {
    createThreadCount += 1;
    return Future.value(_thread(title ?? ''));
  }

  @override
  Future<MessageListResponse> listMessages(String threadId, {int limit = 120}) {
    return Future.value(MessageListResponse(items: messages));
  }

  @override
  Future<Message> createMessage(String threadId, String text) async {
    final message = Message(
      id: 'msg_user',
      threadId: threadId,
      role: 'user',
      contentText: text,
      contentParts: [MessagePart(kind: 'text', text: text)],
      createdAt: '2026-05-20T00:00:00Z',
    );
    messages.add(message);
    return message;
  }

  @override
  Future<Run> createRun(String threadId, {String? skillId, String? mode}) {
    return Future.value(
      Run(
        id: 'run_1',
        threadId: threadId,
        status: 'running',
        mode: mode ?? 'direct_response',
        createdAt: '2026-05-20T00:00:01Z',
      ),
    );
  }

  @override
  Future<Run> getRun(String runId) {
    return Future.value(
      Run(
        id: runId,
        threadId: 'thread_1',
        status: 'running',
        mode: 'direct_response',
        createdAt: '2026-05-20T00:00:01Z',
      ),
    );
  }
}

RunEvent _runEvent(
  String runId,
  String eventId,
  int seq,
  String type,
  Map<String, Object?> data,
) {
  return RunEvent(
    eventId: eventId,
    runId: runId,
    seq: seq,
    ts: '2026-05-20T00:00:02Z',
    type: type,
    data: data,
  );
}

ChatItem? _lastAssistant(List<ChatItem> items) {
  for (var index = items.length - 1; index >= 0; index -= 1) {
    final item = items[index];
    if (item.isAssistant) {
      return item;
    }
  }
  return null;
}

Thread _thread(String title) {
  return Thread(
    id: 'thread_1',
    title: title,
    workspaceRoot: '/repo',
    createdAt: '2026-05-20T00:00:00Z',
    updatedAt: '2026-05-20T00:00:00Z',
    state: 'active',
  );
}

InboxResponse _inbox() {
  return const InboxResponse(
    pendingActions: [],
    activeRuns: [],
    recentTerminalRuns: [],
    system: SystemStatus(
      runtimeReadiness: RuntimeReadiness(status: 'ready'),
      model: CapabilitiesModel(name: 'gpt-test'),
      workspaceRoot: '/repo',
      summary: CapabilitiesSummary(
        toolCount: 1,
        enabledToolCount: 1,
        skillCount: 0,
      ),
      features: CapabilitiesFeatures(
        interruptResume: true,
        traceDebug: true,
        sessionHistory: true,
      ),
    ),
  );
}
