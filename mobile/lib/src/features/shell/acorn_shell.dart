import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/providers.dart';
import '../approvals/approvals_screen.dart';
import '../settings/settings_screen.dart';
import '../threads/threads_screen.dart';

class AcornShell extends ConsumerStatefulWidget {
  const AcornShell({super.key});

  static const _screens = <Widget>[
    ThreadsScreen(),
    ApprovalsScreen(),
    SettingsScreen(),
  ];

  @override
  ConsumerState<AcornShell> createState() => _AcornShellState();
}

class _AcornShellState extends ConsumerState<AcornShell> {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      unawaited(ref.read(inboxControllerProvider).refresh());
      unawaited(
        ref.read(threadsControllerProvider).refresh(selectFirstThread: true),
      );
    });
  }

  @override
  Widget build(BuildContext context) {
    final selectedTab = ref.watch(
      shellControllerProvider.select((controller) => controller.selectedIndex),
    );
    final pendingCount = ref.watch(
      inboxControllerProvider.select(
        (controller) => controller.pendingActions.length,
      ),
    );
    final colors = Theme.of(context).colorScheme;

    return Scaffold(
      resizeToAvoidBottomInset: false,
      body: IndexedStack(index: selectedTab, children: AcornShell._screens),
      bottomNavigationBar: DecoratedBox(
        decoration: BoxDecoration(
          border: Border(
            top: BorderSide(
              color: colors.outlineVariant.withValues(alpha: 0.72),
            ),
          ),
        ),
        child: NavigationBar(
          selectedIndex: selectedTab,
          onDestinationSelected: ref.read(shellControllerProvider).selectTab,
          destinations: [
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
