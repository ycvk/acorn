import 'package:flutter_secure_storage/flutter_secure_storage.dart';

import 'connection_profile.dart';

abstract interface class ConnectionStore {
  Future<ConnectionProfile?> load();
  Future<void> save(ConnectionProfile profile);
  Future<void> clear();
}

class SecureConnectionStore implements ConnectionStore {
  const SecureConnectionStore({FlutterSecureStorage? storage})
    : _storage = storage ?? const FlutterSecureStorage();

  static const _serverUrlKey = 'acorn.server_url';
  static const _deviceIdKey = 'acorn.device_id';
  static const _accessTokenKey = 'acorn.access_token';

  final FlutterSecureStorage _storage;

  @override
  Future<ConnectionProfile?> load() async {
    final serverUrl = await _storage.read(key: _serverUrlKey);
    final deviceId = await _storage.read(key: _deviceIdKey);
    final accessToken = await _storage.read(key: _accessTokenKey);
    if (serverUrl == null || deviceId == null || accessToken == null) {
      return null;
    }
    return ConnectionProfile(
      serverUrl: serverUrl,
      deviceId: deviceId,
      accessToken: accessToken,
    );
  }

  @override
  Future<void> save(ConnectionProfile profile) async {
    await _storage.write(key: _serverUrlKey, value: profile.serverUrl);
    await _storage.write(key: _deviceIdKey, value: profile.deviceId);
    await _storage.write(key: _accessTokenKey, value: profile.accessToken);
  }

  @override
  Future<void> clear() async {
    await _storage.delete(key: _serverUrlKey);
    await _storage.delete(key: _deviceIdKey);
    await _storage.delete(key: _accessTokenKey);
  }
}

class MemoryConnectionStore implements ConnectionStore {
  ConnectionProfile? profile;

  @override
  Future<ConnectionProfile?> load() async => profile;

  @override
  Future<void> save(ConnectionProfile profile) async {
    this.profile = profile;
  }

  @override
  Future<void> clear() async {
    profile = null;
  }
}
