import 'package:flutter/foundation.dart';

import '../../api/acorn_api.dart';
import '../../core/connection_controller.dart';

class InboxController extends ChangeNotifier {
  InboxController({required ConnectionController connectionController})
    : _connectionController = connectionController;

  final ConnectionController _connectionController;

  bool loading = false;
  String? errorMessage;
  InboxResponse? inbox;

  List<PendingActionSummary> get pendingActions =>
      inbox?.pendingActions ?? const <PendingActionSummary>[];
  List<RunSummary> get activeRuns => inbox?.activeRuns ?? const <RunSummary>[];
  List<RunSummary> get recentTerminalRuns =>
      inbox?.recentTerminalRuns ?? const <RunSummary>[];
  SystemStatus? get system => inbox?.system;

  Future<void> refresh() async {
    loading = true;
    errorMessage = null;
    notifyListeners();
    try {
      inbox = await _connectionController.api.getInbox();
    } catch (error) {
      errorMessage = acornUserFacingErrorText(error);
    } finally {
      loading = false;
      notifyListeners();
    }
  }

  void clear() {
    loading = false;
    errorMessage = null;
    inbox = null;
    notifyListeners();
  }
}
