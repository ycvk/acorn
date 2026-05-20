import 'package:flutter/foundation.dart';

import '../../api/acorn_api.dart';
import '../../core/connection_controller.dart';

@immutable
class RunDetailState {
  const RunDetailState({required this.loading, this.detail, this.errorMessage});

  final bool loading;
  final RunDetail? detail;
  final String? errorMessage;

  bool get hasDetail => detail != null;
}

class RunDetailController extends ChangeNotifier {
  RunDetailController({required ConnectionController connectionController})
    : _connectionController = connectionController;

  final ConnectionController _connectionController;
  final Map<String, RunDetailState> _states = <String, RunDetailState>{};

  RunDetailState stateFor(String runId) {
    return _states[runId] ?? const RunDetailState(loading: false);
  }

  Future<void> load(String runId, {bool force = false}) async {
    final current = stateFor(runId);
    if (!force && current.hasDetail) {
      return;
    }

    _setState(runId, RunDetailState(loading: true, detail: current.detail));
    try {
      final detail = await _connectionController.api.getRunDetail(runId);
      _setState(runId, RunDetailState(loading: false, detail: detail));
    } catch (error) {
      _setState(
        runId,
        RunDetailState(
          loading: false,
          detail: current.detail,
          errorMessage: acornUserFacingErrorText(error),
        ),
      );
    }
  }

  void clear() {
    _states.clear();
    notifyListeners();
  }

  void _setState(String runId, RunDetailState state) {
    _states[runId] = state;
    notifyListeners();
  }
}
