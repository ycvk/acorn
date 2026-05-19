import 'package:flutter/material.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import '../../core/connection_profile.dart';
import '../../ui/widgets/acorn_surfaces.dart';

class PairingQrScannerScreen extends StatefulWidget {
  const PairingQrScannerScreen({super.key});

  @override
  State<PairingQrScannerScreen> createState() => _PairingQrScannerScreenState();
}

class _PairingQrScannerScreenState extends State<PairingQrScannerScreen> {
  final MobileScannerController _controller = MobileScannerController(
    formats: const [BarcodeFormat.qrCode],
  );
  String? _errorMessage;
  bool _returning = false;

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Scan pairing QR')),
      body: Stack(
        children: [
          MobileScanner(controller: _controller, onDetect: _handleDetect),
          Align(
            alignment: Alignment.bottomCenter,
            child: AcornCameraInstructionSurface(
              error: _errorMessage,
              child: const Text('Point the camera at the Acorn pairing QR.'),
            ),
          ),
        ],
      ),
    );
  }

  void _handleDetect(BarcodeCapture capture) {
    if (_returning) {
      return;
    }
    String? raw;
    for (final barcode in capture.barcodes) {
      final value = barcode.rawValue;
      if (value != null && value.trim().isNotEmpty) {
        raw = value;
        break;
      }
    }
    if (raw == null) {
      return;
    }

    try {
      final payload = parsePairingPayload(raw);
      _returning = true;
      Navigator.of(context).pop(payload);
    } on FormatException catch (error) {
      setState(() {
        _errorMessage = error.message;
      });
    }
  }
}
