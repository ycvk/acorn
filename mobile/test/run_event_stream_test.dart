import 'dart:convert';

import 'package:acorn_mobile/src/api/acorn_api.dart';
import 'package:acorn_mobile/src/api/run_event_stream.dart';
import 'package:acorn_mobile/src/core/connection_profile.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  test('follows strict RunEvent SSE frames', () async {
    final client = RunEventStreamClient(
      profile: const ConnectionProfile(
        serverUrl: 'http://acorn.local',
        deviceId: 'device_1',
        accessToken: 'token_1',
      ),
      httpClient: MockClient((request) async {
        expect(request.method, 'GET');
        expect(
          request.url.toString(),
          'http://acorn.local/v1/runs/run_1/events?after_seq=7&follow=true',
        );
        expect(request.headers['Accept'], 'text/event-stream');
        expect(request.headers['Authorization'], 'Bearer token_1');
        return http.Response(
          [
            'id: event_1',
            'event: assistant.delta',
            'data: ${jsonEncode(_event('event_1', 8, 'assistant.delta', {
              'assistant_delta': {'delta': 'hi'},
            }))}',
            '',
            'id: event_2',
            'event: run.completed',
            'data: ${jsonEncode(_event('event_2', 9, 'run.completed', {
              'message': {'role': 'assistant', 'content': 'hi'},
            }))}',
            '',
          ].join('\n'),
          200,
          headers: {'content-type': 'text/event-stream'},
        );
      }),
    );

    final events = await client.followRunEvents('run_1', afterSeq: 7).toList();

    expect(events, hasLength(2));
    expect(events.first.type, 'assistant.delta');
    expect(events.last.type, 'run.completed');
  });

  test('rejects SSE metadata mismatches', () async {
    final client = RunEventStreamClient(
      profile: const ConnectionProfile(
        serverUrl: 'http://acorn.local',
        deviceId: 'device_1',
        accessToken: 'token_1',
      ),
      httpClient: MockClient((request) async {
        return http.Response(
          [
            'id: wrong',
            'event: assistant.delta',
            'data: ${jsonEncode(_event('event_1', 1, 'assistant.delta', {
              'assistant_delta': {'delta': 'hi'},
            }))}',
            '',
          ].join('\n'),
          200,
        );
      }),
    );

    expect(
      client.followRunEvents('run_1').toList(),
      throwsA(isA<RunEventStreamException>()),
    );
  });

  test('rejects unknown RunEvent types', () {
    expect(
      () => validateRunEvent(
        RunEvent.fromJson(
          _event('event_future', 1, 'future.backend_event', {}),
        ),
        expectedRunId: 'run_1',
      ),
      throwsA(isA<RunEventStreamException>()),
    );
  });

  test('rejects removed crystallization RunEvent types', () {
    expect(
      () => validateRunEvent(
        RunEvent.fromJson(
          _event('event_removed', 1, 'crystallization.verdict', {
            'run_id': 'run_1',
            'verdict': 'crystallized',
          }),
        ),
        expectedRunId: 'run_1',
      ),
      throwsA(isA<RunEventStreamException>()),
    );
  });
}

Map<String, Object?> _event(
  String id,
  int seq,
  String type,
  Map<String, Object?> data,
) {
  return {
    'event_id': id,
    'run_id': 'run_1',
    'seq': seq,
    'ts': '2026-05-16T00:00:00Z',
    'type': type,
    'data': data,
  };
}
