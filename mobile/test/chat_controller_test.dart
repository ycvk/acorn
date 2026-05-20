import 'package:acorn_mobile/src/api/acorn_api.dart';
import 'package:acorn_mobile/src/core/connection_controller.dart';
import 'package:acorn_mobile/src/core/connection_store.dart';
import 'package:acorn_mobile/src/features/chat/chat_controller.dart';
import 'package:acorn_mobile/src/features/chat/chat_models.dart';
import 'package:acorn_mobile/src/features/inbox/inbox_controller.dart';
import 'package:acorn_mobile/src/features/threads/threads_controller.dart';
import 'package:flutter_test/flutter_test.dart';

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
}

class _FakeConnectionController extends ConnectionController {
  _FakeConnectionController(this._api)
    : super(connectionStore: MemoryConnectionStore());

  final _FakeChatApi _api;

  @override
  AcornApiClient get api => _api;

  @override
  Stream<RunEvent> followRunEvents(String runId) {
    return Stream<RunEvent>.fromIterable([
      RunEvent(
        eventId: 'event_completed',
        runId: runId,
        seq: 1,
        ts: '2026-05-20T00:00:02Z',
        type: 'run.completed',
        data: {
          'message': {'role': 'assistant', 'content': 'done'},
        },
      ),
    ]);
  }
}

class _FakeChatApi extends AcornApiClient {
  _FakeChatApi() : super(serverUrl: 'http://acorn.local', accessToken: 'token');

  final List<Message> messages = [];

  @override
  Future<InboxResponse> getInbox() {
    return Future.value(_inbox());
  }

  @override
  Future<ThreadListResponse> listThreads({int limit = 40}) {
    return Future.value(ThreadListResponse(items: [_thread('Backend title')]));
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
