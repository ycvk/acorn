import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

import '../api/acorn_api.dart';
import '../api/run_event_stream.dart';
import 'connection_profile.dart';
import 'connection_store.dart';

typedef AcornApiClientFactory =
    AcornApiClient Function({
      required String serverUrl,
      String? accessToken,
      http.Client? httpClient,
    });

typedef RunEventStreamClientFactory =
    RunEventStreamClient Function({
      required ConnectionProfile profile,
      http.Client? httpClient,
    });

AcornApiClient _defaultApiClientFactory({
  required String serverUrl,
  String? accessToken,
  http.Client? httpClient,
}) {
  return AcornApiClient(
    serverUrl: serverUrl,
    accessToken: accessToken,
    httpClient: httpClient,
  );
}

RunEventStreamClient _defaultStreamClientFactory({
  required ConnectionProfile profile,
  http.Client? httpClient,
}) {
  return RunEventStreamClient(profile: profile, httpClient: httpClient);
}

class ConnectionController extends ChangeNotifier {
  ConnectionController({
    required ConnectionStore connectionStore,
    AcornApiClientFactory apiClientFactory = _defaultApiClientFactory,
    RunEventStreamClientFactory streamClientFactory =
        _defaultStreamClientFactory,
  }) : _connectionStore = connectionStore,
       _apiClientFactory = apiClientFactory,
       _streamClientFactory = streamClientFactory;

  final ConnectionStore _connectionStore;
  final AcornApiClientFactory _apiClientFactory;
  final RunEventStreamClientFactory _streamClientFactory;

  ConnectionProfile? _profile;
  AcornApiClient? _api;
  RunEventStreamClient? _stream;

  bool initializing = true;
  bool busy = false;
  String? errorMessage;

  ConnectionProfile? get profile => _profile;
  AcornApiClient get api {
    final client = _api;
    if (_profile == null || client == null) {
      throw const AcornApiException(
        401,
        'not_connected',
        'Connect to an Acorn server first.',
      );
    }
    return client;
  }

  Future<void> boot() async {
    try {
      _setProfile(await _connectionStore.load());
    } catch (error) {
      errorMessage = acornUserFacingErrorText(error);
    } finally {
      initializing = false;
      notifyListeners();
    }
  }

  Future<void> pair({
    required String serverUrl,
    required String pairingCode,
    required String deviceName,
    required String platform,
  }) async {
    await _runBusy(() async {
      final temporary = _apiClientFactory(serverUrl: serverUrl);
      final PairDeviceResponse result;
      try {
        result = await temporary.pairDevice(
          PairDeviceRequest(
            pairingCode: pairingCode.trim(),
            deviceName: deviceName.trim(),
            platform: platform,
          ),
        );
      } finally {
        temporary.close();
      }
      final next = ConnectionProfile(
        serverUrl: serverUrl.trim(),
        deviceId: result.device.deviceId,
        accessToken: result.accessToken,
      );
      await _connectionStore.save(next);
      _setProfile(next);
    });
  }

  Future<void> disconnect() async {
    await _connectionStore.clear();
    _setProfile(null);
    errorMessage = null;
    notifyListeners();
  }

  Stream<RunEvent> followRunEvents(String runId) {
    final stream = _stream;
    if (stream == null) {
      throw const AcornApiException(
        401,
        'not_connected',
        'Connect to an Acorn server first.',
      );
    }
    return stream.followRunEvents(runId);
  }

  Future<void> _runBusy(Future<void> Function() action) async {
    busy = true;
    errorMessage = null;
    notifyListeners();
    try {
      await action();
    } catch (error) {
      errorMessage = acornUserFacingErrorText(error);
    } finally {
      busy = false;
      notifyListeners();
    }
  }

  void _setProfile(ConnectionProfile? profile) {
    _api?.close();
    _stream?.close();
    _profile = profile;
    _api = profile == null
        ? null
        : _apiClientFactory(
            serverUrl: profile.serverUrl,
            accessToken: profile.accessToken,
          );
    _stream = profile == null ? null : _streamClientFactory(profile: profile);
  }

  @override
  void dispose() {
    _api?.close();
    _stream?.close();
    super.dispose();
  }
}

String acornUserFacingErrorText(Object error) {
  if (error is AcornApiException) {
    return error.message.isEmpty ? error.code : error.message;
  }
  if (error is RunEventStreamException) {
    return error.message;
  }
  if (error is FormatException) {
    return error.message;
  }
  return error.toString();
}
