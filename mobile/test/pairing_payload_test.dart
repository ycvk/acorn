import 'package:acorn_mobile/src/core/connection_profile.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('parses Acorn pairing payload JSON', () {
    final payload = parsePairingPayload(
      '{"pairing_code":" ABCD-EFGH-IJKL-MNOP ","expires_at":"2026-05-15T12:00:00Z","server_url":" https://acorn.example.com "}',
    );

    expect(payload.serverUrl, 'https://acorn.example.com');
    expect(payload.pairingCode, 'ABCD-EFGH-IJKL-MNOP');
    expect(payload.expiresAt, DateTime.parse('2026-05-15T12:00:00Z'));
  });

  test('rejects non-json payloads', () {
    expect(
      () => parsePairingPayload('not json'),
      throwsA(isA<FormatException>()),
    );
  });

  test('rejects missing required fields', () {
    expect(
      () => parsePairingPayload('{"server_url":"https://acorn.example.com"}'),
      throwsA(isA<FormatException>()),
    );
  });

  test('rejects invalid expires_at field', () {
    expect(
      () => parsePairingPayload(
        '{"pairing_code":"ABCD","expires_at":"soon","server_url":"https://acorn.example.com"}',
      ),
      throwsA(isA<FormatException>()),
    );
  });
}
