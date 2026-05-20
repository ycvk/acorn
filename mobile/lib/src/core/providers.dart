import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_riverpod/legacy.dart';

import '../features/approvals/approvals_controller.dart';
import '../features/chat/chat_controller.dart';
import '../features/inbox/inbox_controller.dart';
import '../features/runs/run_detail_controller.dart';
import '../features/shell/shell_controller.dart';
import '../features/threads/threads_controller.dart';
import 'connection_controller.dart';
import 'connection_store.dart';

final connectionStoreProvider = Provider<ConnectionStore>(
  (ref) => const SecureConnectionStore(),
);

final connectionControllerProvider =
    ChangeNotifierProvider<ConnectionController>((ref) {
      final controller = ConnectionController(
        connectionStore: ref.watch(connectionStoreProvider),
      );
      unawaited(controller.boot());
      return controller;
    });

final chatControllerProvider = ChangeNotifierProvider<ChatController>((ref) {
  return ChatController(
    connectionController: ref.read(connectionControllerProvider),
    threadsController: ref.read(threadsControllerProvider),
    inboxController: ref.read(inboxControllerProvider),
  );
});

final inboxControllerProvider = ChangeNotifierProvider<InboxController>((ref) {
  return InboxController(
    connectionController: ref.read(connectionControllerProvider),
  );
});

final threadsControllerProvider = ChangeNotifierProvider<ThreadsController>((
  ref,
) {
  return ThreadsController(
    connectionController: ref.read(connectionControllerProvider),
  );
});

final approvalsControllerProvider = ChangeNotifierProvider<ApprovalsController>(
  (ref) {
    return ApprovalsController(
      connectionController: ref.read(connectionControllerProvider),
      inboxController: ref.read(inboxControllerProvider),
    );
  },
);

final runDetailControllerProvider = ChangeNotifierProvider<RunDetailController>(
  (ref) {
    return RunDetailController(
      connectionController: ref.read(connectionControllerProvider),
    );
  },
);

final shellControllerProvider = ChangeNotifierProvider<ShellController>((ref) {
  return ShellController();
});
