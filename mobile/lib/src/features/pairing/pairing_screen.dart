import 'dart:io' show Platform;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/connection_profile.dart';
import '../../core/providers.dart';
import '../../ui/theme/acorn_theme.dart';
import '../../ui/widgets/acorn_status.dart';
import '../../ui/widgets/acorn_surfaces.dart';
import 'pairing_qr_scanner_screen.dart';

class PairingScreen extends ConsumerStatefulWidget {
  const PairingScreen({super.key});

  @override
  ConsumerState<PairingScreen> createState() => _PairingScreenState();
}

class _PairingScreenState extends ConsumerState<PairingScreen> {
  final _serverUrl = TextEditingController(text: 'http://127.0.0.1:8080');
  final _pairingCode = TextEditingController();
  final _deviceName = TextEditingController(text: 'Acorn Mobile');

  @override
  void dispose() {
    _serverUrl.dispose();
    _pairingCode.dispose();
    _deviceName.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final controller = ref.watch(connectionControllerProvider);

    return Scaffold(
      body: SafeArea(
        bottom: false,
        child: LayoutBuilder(
          builder: (context, constraints) {
            final horizontalPadding = constraints.maxWidth > 480 ? 28.0 : 16.0;
            return Center(
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 560),
                child: ListView(
                  keyboardDismissBehavior:
                      ScrollViewKeyboardDismissBehavior.onDrag,
                  padding: EdgeInsets.fromLTRB(
                    horizontalPadding,
                    14,
                    horizontalPadding,
                    28,
                  ),
                  children: [
                    const _PairingHero(),
                    const SizedBox(height: 16),
                    _PairingForm(
                      serverUrl: _serverUrl,
                      pairingCode: _pairingCode,
                      deviceName: _deviceName,
                      busy: controller.busy,
                      onPair: () => controller.pair(
                        serverUrl: _serverUrl.text,
                        pairingCode: _pairingCode.text,
                        deviceName: _deviceName.text,
                        platform: _platformName(),
                      ),
                      onScanPairingQr: _scanPairingQr,
                    ),
                    if (controller.errorMessage != null) ...[
                      const SizedBox(height: 14),
                      ErrorBanner(message: controller.errorMessage!),
                    ],
                  ],
                ),
              ),
            );
          },
        ),
      ),
    );
  }

  Future<void> _scanPairingQr() async {
    final payload = await Navigator.of(context).push<PairingPayload>(
      MaterialPageRoute(builder: (_) => const PairingQrScannerScreen()),
    );
    if (!mounted || payload == null) {
      return;
    }
    // Backfill the form first so a failed auto-pair leaves editable values.
    _serverUrl.text = payload.serverUrl;
    _pairingCode.text = payload.pairingCode;
    await ref
        .read(connectionControllerProvider)
        .pair(
          serverUrl: payload.serverUrl,
          pairingCode: payload.pairingCode,
          deviceName: _deviceName.text,
          platform: _platformName(),
        );
  }
}

class _PairingHero extends StatelessWidget {
  const _PairingHero();

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;
    final dark = colors.brightness == Brightness.dark;
    final background = dark
        ? colors.surfaceContainerHigh
        : colors.inverseSurface;
    final foreground = dark ? colors.onSurface : colors.onInverseSurface;
    final muted = foreground.withValues(alpha: 0.72);

    return Material(
      color: background,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(34)),
      clipBehavior: Clip.antiAlias,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 18, 20, 20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                SizedBox.square(
                  dimension: 44,
                  child: DecoratedBox(
                    decoration: BoxDecoration(
                      color: foreground.withValues(alpha: 0.11),
                      borderRadius: BorderRadius.circular(AcornRadius.lg),
                      border: Border.all(
                        color: foreground.withValues(alpha: 0.16),
                      ),
                    ),
                    child: Icon(Icons.spa_rounded, color: foreground, size: 25),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Text(
                    'Acorn',
                    style: text.titleLarge?.copyWith(
                      color: foreground,
                      fontWeight: FontWeight.w900,
                    ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 22),
            Text(
              'Control your self-hosted backend.',
              style: text.headlineMedium?.copyWith(
                color: foreground,
                fontWeight: FontWeight.w900,
                height: 1.05,
              ),
            ),
            const SizedBox(height: 8),
            Text(
              'Pair once with a terminal QR or pairing code. After that, this phone only consumes authenticated /v1 server truth.',
              style: text.bodyMedium?.copyWith(
                color: muted,
                fontWeight: FontWeight.w600,
              ),
            ),
            const SizedBox(height: 18),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                _HeroChip(
                  icon: Icons.verified_user_outlined,
                  label: 'Single owner',
                  foreground: foreground,
                ),
                _HeroChip(
                  icon: Icons.route_outlined,
                  label: '/v1 remote',
                  foreground: foreground,
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _HeroChip extends StatelessWidget {
  const _HeroChip({
    required this.icon,
    required this.label,
    required this.foreground,
  });

  final IconData icon;
  final String label;
  final Color foreground;

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: BoxDecoration(
        color: foreground.withValues(alpha: 0.1),
        borderRadius: BorderRadius.circular(AcornRadius.pill),
        border: Border.all(color: foreground.withValues(alpha: 0.14)),
      ),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(icon, color: foreground, size: 17),
            const SizedBox(width: 7),
            Text(
              label,
              style: Theme.of(context).textTheme.labelMedium?.copyWith(
                color: foreground,
                fontWeight: FontWeight.w800,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _PairingForm extends StatelessWidget {
  const _PairingForm({
    required this.serverUrl,
    required this.pairingCode,
    required this.deviceName,
    required this.busy,
    required this.onPair,
    required this.onScanPairingQr,
  });

  final TextEditingController serverUrl;
  final TextEditingController pairingCode;
  final TextEditingController deviceName;
  final bool busy;
  final VoidCallback onPair;
  final VoidCallback onScanPairingQr;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    return AcornSurface(
      tone: AcornSurfaceTone.low,
      border: true,
      radius: AcornRadius.xl,
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 16),
      child: AutofillGroup(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    'Pair this device',
                    style: Theme.of(context).textTheme.titleMedium?.copyWith(
                      fontWeight: FontWeight.w900,
                    ),
                  ),
                ),
                Icon(Icons.lock_outline, color: colors.onSurfaceVariant),
              ],
            ),
            const SizedBox(height: 14),
            TextField(
              controller: serverUrl,
              decoration: const InputDecoration(
                labelText: 'Server URL',
                prefixIcon: Icon(Icons.dns_outlined),
              ),
              keyboardType: TextInputType.url,
              autofillHints: const [AutofillHints.url],
            ),
            const SizedBox(height: 10),
            TextField(
              controller: pairingCode,
              decoration: const InputDecoration(
                labelText: 'Pairing code',
                prefixIcon: Icon(Icons.password_outlined),
              ),
              textCapitalization: TextCapitalization.characters,
            ),
            const SizedBox(height: 10),
            TextField(
              controller: deviceName,
              decoration: const InputDecoration(
                labelText: 'Device name',
                prefixIcon: Icon(Icons.phone_android_outlined),
              ),
            ),
            const SizedBox(height: 16),
            FilledButton.icon(
              onPressed: busy ? null : onPair,
              icon: const Icon(Icons.link),
              label: const Text('Pair device'),
            ),
            const SizedBox(height: 10),
            OutlinedButton.icon(
              onPressed: busy ? null : onScanPairingQr,
              icon: const Icon(Icons.qr_code_scanner),
              label: const Text('Scan pairing QR'),
            ),
            if (busy) ...[
              const SizedBox(height: 16),
              const LinearProgressIndicator(),
            ],
          ],
        ),
      ),
    );
  }
}

String _platformName() {
  if (Platform.isIOS) {
    return 'ios';
  }
  if (Platform.isAndroid) {
    return 'android';
  }
  return 'mobile';
}
