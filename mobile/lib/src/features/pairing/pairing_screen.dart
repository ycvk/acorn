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
    final controller = ref.watch(acornControllerProvider);
    final colors = Theme.of(context).colorScheme;
    final text = Theme.of(context).textTheme;

    return Scaffold(
      body: SafeArea(
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 560),
            child: ListView(
              padding: const EdgeInsets.fromLTRB(20, 28, 20, 28),
              children: [
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    AcornTonalIcon(
                      icon: Icons.spa_rounded,
                      tone: AcornStatusTone.success,
                      size: 64,
                      iconSize: 34,
                      radius: AcornRadius.xl,
                    ),
                    const SizedBox(width: 16),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text('Acorn', style: text.headlineMedium),
                          const SizedBox(height: 6),
                          Text(
                            'Mobile control for your self-hosted backend.',
                            style: text.bodyLarge?.copyWith(
                              color: colors.onSurfaceVariant,
                            ),
                          ),
                        ],
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 24),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: const [
                    StatusPill(
                      label: 'Single owner',
                      tone: AcornStatusTone.success,
                      icon: Icons.verified_user_outlined,
                    ),
                    StatusPill(
                      label: '/v1 remote',
                      tone: AcornStatusTone.info,
                      icon: Icons.route_outlined,
                    ),
                  ],
                ),
                const SizedBox(height: 28),
                AcornSurface(
                  tone: AcornSurfaceTone.low,
                  border: true,
                  radius: AcornRadius.xl,
                  padding: const EdgeInsets.all(16),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      TextField(
                        controller: _serverUrl,
                        decoration: const InputDecoration(
                          labelText: 'Server URL',
                          prefixIcon: Icon(Icons.dns_outlined),
                        ),
                        keyboardType: TextInputType.url,
                      ),
                      const SizedBox(height: 12),
                      TextField(
                        controller: _pairingCode,
                        decoration: const InputDecoration(
                          labelText: 'Pairing code',
                          prefixIcon: Icon(Icons.password_outlined),
                        ),
                        textCapitalization: TextCapitalization.characters,
                      ),
                      const SizedBox(height: 12),
                      TextField(
                        controller: _deviceName,
                        decoration: const InputDecoration(
                          labelText: 'Device name',
                          prefixIcon: Icon(Icons.phone_android_outlined),
                        ),
                      ),
                      const SizedBox(height: 18),
                      FilledButton.icon(
                        onPressed: controller.busy
                            ? null
                            : () => controller.pair(
                                serverUrl: _serverUrl.text,
                                pairingCode: _pairingCode.text,
                                deviceName: _deviceName.text,
                                platform: _platformName(),
                              ),
                        icon: const Icon(Icons.link),
                        label: const Text('Pair device'),
                      ),
                      const SizedBox(height: 10),
                      OutlinedButton.icon(
                        onPressed: controller.busy ? null : _scanPairingQr,
                        icon: const Icon(Icons.qr_code_scanner),
                        label: const Text('Scan pairing QR'),
                      ),
                      if (controller.busy) ...[
                        const SizedBox(height: 18),
                        const LinearProgressIndicator(),
                      ],
                    ],
                  ),
                ),
                if (controller.errorMessage != null) ...[
                  const SizedBox(height: 14),
                  ErrorBanner(message: controller.errorMessage!),
                ],
              ],
            ),
          ),
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
    _serverUrl.text = payload.serverUrl;
    _pairingCode.text = payload.pairingCode;
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
