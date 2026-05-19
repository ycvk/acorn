import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_riverpod/legacy.dart';

import 'acorn_controller.dart';
import 'connection_store.dart';

final connectionStoreProvider = Provider<ConnectionStore>(
  (ref) => const SecureConnectionStore(),
);

final acornControllerProvider = ChangeNotifierProvider<AcornController>((ref) {
  final controller = AcornController(
    connectionStore: ref.watch(connectionStoreProvider),
  );
  unawaited(controller.boot());
  return controller;
});
