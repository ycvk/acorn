import 'package:flutter/foundation.dart';

class ShellController extends ChangeNotifier {
  int selectedIndex = 0;

  void selectTab(int index) {
    if (selectedIndex == index) {
      return;
    }
    selectedIndex = index;
    notifyListeners();
  }

  void reset() {
    if (selectedIndex == 0) {
      return;
    }
    selectedIndex = 0;
    notifyListeners();
  }
}
