import 'package:flutter/foundation.dart';

import '../../api/acorn_api.dart';
import '../../core/connection_controller.dart';
import '../inbox/inbox_controller.dart';

class ApprovalsController extends ChangeNotifier {
  ApprovalsController({
    required ConnectionController connectionController,
    required InboxController inboxController,
  }) : _connectionController = connectionController,
       _inboxController = inboxController;

  final ConnectionController _connectionController;
  final InboxController _inboxController;

  bool busy = false;
  String? errorMessage;
  PendingActionDetail? pendingActionDetail;

  Future<PendingActionDetail?> loadPendingAction(String actionId) async {
    busy = true;
    errorMessage = null;
    notifyListeners();
    try {
      pendingActionDetail = await _connectionController.api.getPendingAction(
        actionId,
      );
      return pendingActionDetail;
    } catch (error) {
      errorMessage = acornUserFacingErrorText(error);
      return null;
    } finally {
      busy = false;
      notifyListeners();
    }
  }

  Future<void> decidePendingAction(
    String actionId, {
    required String decision,
    String? selectedOptionId,
    String? answer,
  }) async {
    busy = true;
    errorMessage = null;
    notifyListeners();
    try {
      await _connectionController.api.decidePendingAction(
        actionId,
        PendingActionDecisionRequest(
          decision: decision,
          selectedOptionId: selectedOptionId,
          answer: answer,
        ),
      );
      pendingActionDetail = null;
      await _inboxController.refresh();
    } catch (error) {
      errorMessage = acornUserFacingErrorText(error);
    } finally {
      busy = false;
      notifyListeners();
    }
  }

  void clear() {
    busy = false;
    errorMessage = null;
    pendingActionDetail = null;
    notifyListeners();
  }
}
