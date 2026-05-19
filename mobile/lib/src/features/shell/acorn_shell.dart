import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/providers.dart';
import '../approvals/approvals_screen.dart';
import '../chat/chat_screen.dart';
import '../settings/settings_screen.dart';
import '../threads/threads_screen.dart';

class AcornShell extends ConsumerWidget {
  const AcornShell({super.key});

  static const _screens = <Widget>[
    ChatScreen(),
    ThreadsScreen(),
    ApprovalsScreen(),
    SettingsScreen(),
  ];

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final controller = ref.watch(acornControllerProvider);
    final colors = Theme.of(context).colorScheme;
    final pendingCount = controller.inbox?.pendingActions.length ?? 0;

    return Scaffold(
      resizeToAvoidBottomInset: false,
      body: IndexedStack(index: controller.selectedTab, children: _screens),
      bottomNavigationBar: DecoratedBox(
        decoration: BoxDecoration(
          border: Border(
            top: BorderSide(
              color: colors.outlineVariant.withValues(alpha: 0.72),
            ),
          ),
        ),
        child: NavigationBar(
          selectedIndex: controller.selectedTab,
          onDestinationSelected: controller.selectTab,
          destinations: [
            const NavigationDestination(
              icon: Icon(Icons.chat_bubble_outline),
              selectedIcon: Icon(Icons.chat_bubble),
              label: 'Chat',
            ),
            const NavigationDestination(
              icon: Icon(Icons.forum_outlined),
              selectedIcon: Icon(Icons.forum),
              label: 'Threads',
            ),
            NavigationDestination(
              icon: _BadgeIcon(
                count: pendingCount,
                child: const Icon(Icons.rule_folder_outlined),
              ),
              selectedIcon: _BadgeIcon(
                count: pendingCount,
                child: const Icon(Icons.rule_folder),
              ),
              label: 'Approvals',
            ),
            const NavigationDestination(
              icon: Icon(Icons.settings_outlined),
              selectedIcon: Icon(Icons.settings),
              label: 'Settings',
            ),
          ],
        ),
      ),
    );
  }
}

class _BadgeIcon extends StatelessWidget {
  const _BadgeIcon({required this.count, required this.child});

  final int count;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    if (count == 0) {
      return child;
    }
    return Badge(label: Text(count > 9 ? '9+' : '$count'), child: child);
  }
}
