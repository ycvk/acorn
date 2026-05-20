import 'package:acorn_mobile/src/api/acorn_api.dart';
import 'package:acorn_mobile/src/core/connection_controller.dart';
import 'package:acorn_mobile/src/core/connection_store.dart';
import 'package:acorn_mobile/src/features/runs/run_detail_controller.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test(
    'run detail controller caches detail by run and exposes refresh errors',
    () async {
      final api = _FakeRunDetailApi();
      final app = _FakeConnectionController(api);
      final controller = RunDetailController(connectionController: app);
      addTearDown(app.dispose);
      addTearDown(controller.dispose);

      final firstLoad = controller.load('run_1');
      expect(controller.stateFor('run_1').loading, isTrue);
      await firstLoad;

      final loaded = controller.stateFor('run_1');
      expect(loaded.loading, isFalse);
      expect(loaded.errorMessage, isNull);
      expect(loaded.detail?.run.id, 'run_1');
      expect(api.calls, ['run_1']);

      await controller.load('run_1');
      expect(api.calls, ['run_1']);

      api.failNext = true;
      await controller.load('run_1', force: true);

      final failedRefresh = controller.stateFor('run_1');
      expect(failedRefresh.loading, isFalse);
      expect(failedRefresh.detail?.run.id, 'run_1');
      expect(failedRefresh.errorMessage, 'backend unavailable');
      expect(api.calls, ['run_1', 'run_1']);
    },
  );

  test('run detail controller keeps run states isolated', () async {
    final api = _FakeRunDetailApi();
    final app = _FakeConnectionController(api);
    final controller = RunDetailController(connectionController: app);
    addTearDown(app.dispose);
    addTearDown(controller.dispose);

    await controller.load('run_1');
    await controller.load('run_2');

    expect(controller.stateFor('run_1').detail?.thread.title, 'Thread run_1');
    expect(controller.stateFor('run_2').detail?.thread.title, 'Thread run_2');
    expect(api.calls, ['run_1', 'run_2']);
  });
}

class _FakeConnectionController extends ConnectionController {
  _FakeConnectionController(this._api)
    : super(connectionStore: MemoryConnectionStore());

  final _FakeRunDetailApi _api;

  @override
  AcornApiClient get api => _api;
}

class _FakeRunDetailApi extends AcornApiClient {
  _FakeRunDetailApi()
    : super(serverUrl: 'http://acorn.local', accessToken: 'token');

  final List<String> calls = [];
  bool failNext = false;

  @override
  Future<RunDetail> getRunDetail(String runId) {
    calls.add(runId);
    if (failNext) {
      failNext = false;
      throw const AcornApiException(503, 'unavailable', 'backend unavailable');
    }
    return Future.value(_detail(runId));
  }
}

RunDetail _detail(String runId) {
  return RunDetail(
    run: Run(
      id: runId,
      threadId: 'thread_$runId',
      status: 'completed',
      mode: 'direct_response',
      createdAt: '2026-05-20T00:00:00Z',
      completedAt: '2026-05-20T00:01:00Z',
    ),
    thread: Thread(
      id: 'thread_$runId',
      title: 'Thread $runId',
      workspaceRoot: '/repo',
      createdAt: '2026-05-20T00:00:00Z',
      updatedAt: '2026-05-20T00:01:00Z',
      state: 'active',
    ),
    events: [
      RunEvent(
        eventId: 'event_$runId',
        runId: runId,
        seq: 1,
        ts: '2026-05-20T00:01:00Z',
        type: 'run.completed',
        data: const {},
      ),
    ],
    artifacts: const [],
    terminalSessions: const [],
    raw: const {},
  );
}
