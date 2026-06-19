import 'package:flutter/foundation.dart';

import '../../api/acorn_api.dart';
import '../../core/connection_controller.dart';

class ThreadsController extends ChangeNotifier {
  ThreadsController({required ConnectionController connectionController})
    : _connectionController = connectionController;

  final ConnectionController _connectionController;

  bool loading = false;
  String? errorMessage;
  List<Thread> threads = const [];
  Thread? activeThread;

  Future<void> refresh({bool selectFirstThread = false}) async {
    loading = true;
    errorMessage = null;
    notifyListeners();
    try {
      final response = await _connectionController.api.listThreads();
      threads = response.items;

      final current = activeThread;
      if (current != null) {
        final updated = _findThread(current.id);
        if (updated != null) {
          activeThread = updated;
        } else {
          activeThread = null;
        }
      }

      if (activeThread == null && selectFirstThread && threads.isNotEmpty) {
        activeThread = threads.first;
      }
    } catch (error) {
      errorMessage = acornUserFacingErrorText(error);
    } finally {
      loading = false;
      notifyListeners();
    }
  }

  void startDraftThread() {
    errorMessage = null;
    activeThread = null;
    notifyListeners();
  }

  Future<Thread> ensureActiveThread() async {
    final current = activeThread;
    if (current != null) {
      return current;
    }

    loading = true;
    errorMessage = null;
    notifyListeners();
    try {
      final thread = await _connectionController.api.createThread();
      threads = [thread, ...threads];
      activeThread = thread;
      return thread;
    } catch (error) {
      errorMessage = acornUserFacingErrorText(error);
      rethrow;
    } finally {
      loading = false;
      notifyListeners();
    }
  }

  Future<void> renameThread(Thread thread, String title) async {
    final trimmed = title.trim();
    if (trimmed.isEmpty || trimmed == thread.title) {
      return;
    }
    loading = true;
    errorMessage = null;
    notifyListeners();
    try {
      final updated = await _connectionController.api.updateThread(
        thread.id,
        title: trimmed,
      );
      threads = threads
          .map((candidate) => candidate.id == updated.id ? updated : candidate)
          .toList(growable: false);
      if (activeThread?.id == updated.id) {
        activeThread = updated;
      }
    } catch (error) {
      errorMessage = acornUserFacingErrorText(error);
    } finally {
      loading = false;
      notifyListeners();
    }
  }

  Future<void> deleteThread(Thread thread) async {
    loading = true;
    errorMessage = null;
    notifyListeners();
    try {
      await _connectionController.api.deleteThread(thread.id);
      threads = threads
          .where((candidate) => candidate.id != thread.id)
          .toList(growable: false);
      if (activeThread?.id == thread.id) {
        activeThread = null;
      }
    } catch (error) {
      errorMessage = acornUserFacingErrorText(error);
    } finally {
      loading = false;
      notifyListeners();
    }
  }

  void selectThread(Thread thread) {
    activeThread = thread;
    notifyListeners();
  }

  void clear() {
    loading = false;
    errorMessage = null;
    threads = const [];
    activeThread = null;
    notifyListeners();
  }

  Thread? _findThread(String id) {
    for (final thread in threads) {
      if (thread.id == id) {
        return thread;
      }
    }
    return null;
  }
}
