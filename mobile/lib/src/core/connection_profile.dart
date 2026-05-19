import 'dart:convert';

class ConnectionProfile {
  const ConnectionProfile({
    required this.serverUrl,
    required this.deviceId,
    required this.accessToken,
  });

  final String serverUrl;
  final String deviceId;
  final String accessToken;
}

class PairingPayload {
  const PairingPayload({
    required this.serverUrl,
    required this.pairingCode,
    this.expiresAt,
  });

  final String serverUrl;
  final String pairingCode;
  final DateTime? expiresAt;
}

PairingPayload parsePairingPayload(String raw) {
  final decoded = jsonDecode(raw);
  if (decoded is! Map<String, dynamic>) {
    throw const FormatException('Pairing QR must contain a JSON object.');
  }

  final serverUrl = _requiredString(decoded, 'server_url');
  final pairingCode = _requiredString(decoded, 'pairing_code');
  final expiresAtRaw = decoded['expires_at'];
  final expiresAt = expiresAtRaw == null
      ? null
      : DateTime.parse(_stringValue(expiresAtRaw, 'expires_at'));

  return PairingPayload(
    serverUrl: serverUrl,
    pairingCode: pairingCode,
    expiresAt: expiresAt,
  );
}

String _requiredString(Map<String, dynamic> payload, String key) {
  final value = _stringValue(payload[key], key).trim();
  if (value.isEmpty) {
    throw FormatException('$key is required.');
  }
  return value;
}

String _stringValue(Object? value, String key) {
  if (value is String) {
    return value;
  }
  throw FormatException('$key must be a string.');
}
