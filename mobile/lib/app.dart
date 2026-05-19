import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'src/core/connection_store.dart';
import 'src/core/providers.dart';
import 'src/features/pairing/pairing_screen.dart';
import 'src/features/shell/acorn_shell.dart';
import 'src/ui/theme/acorn_theme.dart';
import 'src/ui/widgets/acorn_surfaces.dart';

class AcornApp extends StatelessWidget {
  const AcornApp({super.key, this.connectionStore});

  final ConnectionStore? connectionStore;

  @override
  Widget build(BuildContext context) {
    return ProviderScope(
      overrides: [
        if (connectionStore != null)
          connectionStoreProvider.overrideWithValue(connectionStore!),
      ],
      child: MaterialApp(
        title: 'Acorn',
        debugShowCheckedModeBanner: false,
        theme: buildAcornTheme(Brightness.light),
        darkTheme: buildAcornTheme(Brightness.dark),
        themeMode: ThemeMode.system,
        home: const AcornRoot(),
      ),
    );
  }
}

class AcornRoot extends ConsumerWidget {
  const AcornRoot({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final controller = ref.watch(acornControllerProvider);

    if (controller.initializing) {
      return const _BootScreen();
    }

    if (controller.profile == null) {
      return const PairingScreen();
    }

    return const AcornShell();
  }
}

class _BootScreen extends StatelessWidget {
  const _BootScreen();

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    return Scaffold(
      body: Center(
        child: AcornSurface(
          tone: AcornSurfaceTone.low,
          border: true,
          radius: AcornRadius.xl,
          padding: const EdgeInsets.fromLTRB(22, 20, 22, 20),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const CircularProgressIndicator(),
              const SizedBox(height: 16),
              Text(
                'Connecting to Acorn',
                style: Theme.of(context).textTheme.titleMedium,
              ),
              const SizedBox(height: 4),
              Text(
                'Checking device profile',
                style: Theme.of(
                  context,
                ).textTheme.bodySmall?.copyWith(color: colors.onSurfaceVariant),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
