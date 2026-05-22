import 'package:acorn_mobile/src/api/acorn_api.dart';
import 'package:acorn_mobile/src/core/connection_controller.dart';
import 'package:acorn_mobile/src/core/connection_store.dart';
import 'package:acorn_mobile/src/core/providers.dart';
import 'package:acorn_mobile/src/features/approvals/approvals_controller.dart';
import 'package:acorn_mobile/src/features/chat/chat_controller.dart';
import 'package:acorn_mobile/src/features/inbox/inbox_controller.dart';
import 'package:acorn_mobile/src/features/shell/acorn_shell.dart';
import 'package:acorn_mobile/src/features/shell/shell_controller.dart';
import 'package:acorn_mobile/src/features/threads/threads_controller.dart';
import 'package:acorn_mobile/src/ui/theme/acorn_theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('shell opens on the threads work surface and has no home tab', (
    tester,
  ) async {
    final connection = ConnectionController(
      connectionStore: MemoryConnectionStore(),
    )..initializing = false;

    await tester.pumpWidget(
      _withControllerOverrides(
        connectionController: connection,
        child: MaterialApp(
          theme: buildAcornTheme(Brightness.light),
          home: const AcornShell(),
        ),
      ),
    );

    expect(find.text('Needs your decision'), findsOneWidget);
    expect(find.text('Review deployment?'), findsOneWidget);
    expect(find.text('Threads'), findsWidgets);
    expect(find.text('Approvals'), findsOneWidget);
    expect(find.text('Settings'), findsOneWidget);
    expect(find.text('Home'), findsNothing);
  });
}

Widget _withControllerOverrides({
  required Widget child,
  required ConnectionController connectionController,
}) {
  final inboxController = _TestInboxController(connectionController)
    ..inbox = _inbox();
  final threadsController = _TestThreadsController(connectionController);
  final shell = ShellController();
  final approvalsController = ApprovalsController(
    connectionController: connectionController,
    inboxController: inboxController,
  );
  final chatController = ChatController(
    connectionController: connectionController,
    threadsController: threadsController,
    inboxController: inboxController,
  );
  addTearDown(connectionController.dispose);
  addTearDown(inboxController.dispose);
  addTearDown(threadsController.dispose);
  addTearDown(approvalsController.dispose);
  addTearDown(chatController.dispose);
  addTearDown(shell.dispose);
  return ProviderScope(
    overrides: [
      connectionControllerProvider.overrideWith(
        (ref) => connectionController,
        disposeNotifier: false,
      ),
      inboxControllerProvider.overrideWith(
        (ref) => inboxController,
        disposeNotifier: false,
      ),
      threadsControllerProvider.overrideWith(
        (ref) => threadsController,
        disposeNotifier: false,
      ),
      approvalsControllerProvider.overrideWith(
        (ref) => approvalsController,
        disposeNotifier: false,
      ),
      chatControllerProvider.overrideWith(
        (ref) => chatController,
        disposeNotifier: false,
      ),
      shellControllerProvider.overrideWith(
        (ref) => shell,
        disposeNotifier: false,
      ),
    ],
    child: child,
  );
}

class _TestInboxController extends InboxController {
  _TestInboxController(ConnectionController connectionController)
    : super(connectionController: connectionController);

  @override
  Future<void> refresh() async {}
}

class _TestThreadsController extends ThreadsController {
  _TestThreadsController(ConnectionController connectionController)
    : super(connectionController: connectionController);

  @override
  Future<void> refresh({bool selectFirstThread = false}) async {}
}

InboxResponse _inbox() {
  return InboxResponse(
    pendingActions: const [
      PendingActionSummary(
        actionId: 'action-1',
        runId: 'run-1234567890',
        threadId: 'thread-1',
        kind: 'operator_question',
        status: 'pending',
        title: 'Review deployment?',
        body: 'Acorn needs a device decision before it continues.',
        options: [],
        createdAt: '2026-05-22T08:00:00Z',
      ),
    ],
    activeRuns: const [
      RunSummary(
        runId: 'run-active-123456',
        threadId: 'thread-1',
        threadTitle: 'Ship mobile shell polish',
        status: 'running',
        mode: 'plan_execute',
        preview: 'Updating the Flutter mobile control surface.',
        lastEventLabel: 'tool.call.progress',
        attentionLevel: 'running',
        durationMs: 91000,
        createdAt: '2026-05-22T08:00:00Z',
        updatedAt: '2026-05-22T08:01:30Z',
      ),
    ],
    recentTerminalRuns: [],
    system: const SystemStatus(
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
