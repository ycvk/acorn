import 'package:acorn_mobile/src/api/acorn_api.dart';
import 'package:acorn_mobile/src/core/connection_controller.dart';
import 'package:acorn_mobile/src/core/connection_store.dart';
import 'package:acorn_mobile/src/core/providers.dart';
import 'package:acorn_mobile/src/features/approvals/approvals_controller.dart';
import 'package:acorn_mobile/src/features/chat/chat_controller.dart';
import 'package:acorn_mobile/src/features/home/home_screen.dart';
import 'package:acorn_mobile/src/features/inbox/inbox_controller.dart';
import 'package:acorn_mobile/src/features/shell/acorn_shell.dart';
import 'package:acorn_mobile/src/features/shell/shell_controller.dart';
import 'package:acorn_mobile/src/features/threads/threads_controller.dart';
import 'package:acorn_mobile/src/ui/theme/acorn_theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('home projects inbox attention sections', (tester) async {
    final controller = ConnectionController(
      connectionStore: MemoryConnectionStore(),
    )..initializing = false;

    await tester.pumpWidget(
      _withControllerOverrides(
        connectionController: controller,
        inbox: _inbox(),
        child: MaterialApp(
          theme: buildAcornTheme(Brightness.light),
          home: const HomeScreen(),
        ),
      ),
    );

    expect(find.text('Home'), findsOneWidget);
    expect(find.text('Needs action'), findsOneWidget);
    expect(find.text('Running now'), findsOneWidget);
    expect(find.text('Recently finished'), findsOneWidget);
    expect(find.text('Approve command'), findsOneWidget);
    expect(find.text('Active backend run'), findsOneWidget);
    expect(find.text('Completed backend run'), findsOneWidget);
  });

  testWidgets('home pending action switches to approvals through shell state', (
    tester,
  ) async {
    final controller = ConnectionController(
      connectionStore: MemoryConnectionStore(),
    )..initializing = false;
    final shell = ShellController();

    await tester.pumpWidget(
      _withControllerOverrides(
        connectionController: controller,
        shellController: shell,
        inbox: _inbox(),
        child: MaterialApp(
          theme: buildAcornTheme(Brightness.light),
          home: const HomeScreen(),
        ),
      ),
    );

    await tester.tap(find.text('Approve command'));

    expect(shell.selectedIndex, 2);
  });

  testWidgets('shell uses home as the first destination', (tester) async {
    final controller = ConnectionController(
      connectionStore: MemoryConnectionStore(),
    )..initializing = false;

    await tester.pumpWidget(
      _withControllerOverrides(
        connectionController: controller,
        inbox: _inbox(),
        child: MaterialApp(
          theme: buildAcornTheme(Brightness.light),
          home: const AcornShell(),
        ),
      ),
    );

    expect(find.text('Home'), findsWidgets);
    expect(find.text('Threads'), findsOneWidget);
    expect(find.text('Approvals'), findsOneWidget);
    expect(find.text('Settings'), findsOneWidget);
    expect(find.text('Chat'), findsNothing);
  });
}

Widget _withControllerOverrides({
  required Widget child,
  required ConnectionController connectionController,
  ShellController? shellController,
  required InboxResponse inbox,
}) {
  final inboxController = _TestInboxController(connectionController)
    ..inbox = inbox;
  final threadsController = _TestThreadsController(connectionController);
  final shell = shellController ?? ShellController();
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
  return const InboxResponse(
    pendingActions: [
      PendingActionSummary(
        actionId: 'action_1',
        runId: 'run_pending',
        threadId: 'thread_pending',
        kind: 'operator_question',
        status: 'pending',
        title: 'Approve command',
        options: [
          PendingActionOption(id: 'allow', label: 'Allow'),
          PendingActionOption(id: 'deny', label: 'Deny'),
        ],
        createdAt: '2026-05-20T00:00:00Z',
      ),
    ],
    activeRuns: [
      RunSummary(
        runId: 'run_active',
        threadId: 'thread_active',
        threadTitle: 'Active backend run',
        status: 'running',
        mode: 'direct',
        preview: 'Continue the deployment',
        lastEventLabel: 'Run is running',
        attentionLevel: 'running',
        durationMs: 60000,
        createdAt: '2026-05-20T00:00:00Z',
        updatedAt: '2026-05-20T00:01:00Z',
      ),
    ],
    recentTerminalRuns: [
      RunSummary(
        runId: 'run_done',
        threadId: 'thread_done',
        threadTitle: 'Completed backend run',
        status: 'completed',
        mode: 'plan_execute',
        preview: 'Release completed',
        lastEventLabel: 'Run completed',
        attentionLevel: 'normal',
        durationMs: 180000,
        createdAt: '2026-05-20T00:00:00Z',
        updatedAt: '2026-05-20T00:03:00Z',
      ),
    ],
    system: SystemStatus(
      runtimeReadiness: RuntimeReadiness(status: 'ready', reason: 'ready'),
      model: CapabilitiesModel(name: 'gpt-test'),
      workspaceRoot: '/repo',
      summary: CapabilitiesSummary(
        toolCount: 4,
        enabledToolCount: 3,
        skillCount: 2,
      ),
      features: CapabilitiesFeatures(
        interruptResume: true,
        traceDebug: true,
        sessionHistory: true,
      ),
    ),
  );
}
