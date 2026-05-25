import 'dart:convert';

import 'package:acorn_mobile/src/api/acorn_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  test('pairDevice posts to the unauthenticated pairing endpoint', () async {
    final client = AcornApiClient(
      serverUrl: 'http://acorn.local/',
      httpClient: MockClient((request) async {
        expect(request.method, 'POST');
        expect(request.url.toString(), 'http://acorn.local/v1/devices:pair');
        expect(request.headers.containsKey('Authorization'), isFalse);
        expect(jsonDecode(request.body), {
          'pairing_code': 'ABCD',
          'device_name': 'Phone',
          'platform': 'ios',
        });
        return http.Response(
          jsonEncode({
            'device': {
              'device_id': 'dev_1',
              'name': 'Phone',
              'platform': 'ios',
              'created_at': '2026-05-15T00:00:00Z',
            },
            'access_token': 'token_1',
          }),
          200,
          headers: {'content-type': 'application/json'},
        );
      }),
    );

    final response = await client.pairDevice(
      const PairDeviceRequest(
        pairingCode: 'ABCD',
        deviceName: 'Phone',
        platform: 'ios',
      ),
    );

    expect(response.device.deviceId, 'dev_1');
    expect(response.accessToken, 'token_1');
  });

  test('getInbox sends bearer token and parses mobile inbox', () async {
    final client = AcornApiClient(
      serverUrl: 'http://acorn.local',
      accessToken: 'device_token',
      httpClient: MockClient((request) async {
        expect(request.method, 'GET');
        expect(request.url.toString(), 'http://acorn.local/v1/inbox');
        expect(request.headers['Authorization'], 'Bearer device_token');
        return http.Response(jsonEncode(_inboxJson()), 200);
      }),
    );

    final inbox = await client.getInbox();

    expect(inbox.pendingActions.single.actionId, 'act_1');
    expect(inbox.pendingActions.single.options.first.description, 'Allow');
    expect(inbox.activeRuns.single.runId, 'run_1');
    expect(inbox.activeRuns.single.threadTitle, 'Deploy Acorn');
    expect(inbox.activeRuns.single.preview, 'Run the release workflow');
    expect(inbox.activeRuns.single.attentionLevel, 'running');
    expect(inbox.activeRuns.single.durationMs, 1000);
    expect(inbox.system.runtimeReadiness.status, 'ready');
  });

  test('decidePendingAction sends structured answer request', () async {
    final client = AcornApiClient(
      serverUrl: 'http://acorn.local',
      accessToken: 'device_token',
      httpClient: MockClient((request) async {
        expect(request.method, 'POST');
        expect(
          request.url.toString(),
          'http://acorn.local/v1/pending-actions/action_1:decide',
        );
        expect(request.headers['Authorization'], 'Bearer device_token');
        expect(jsonDecode(request.body), {
          'decision': 'answer',
          'selected_option_id': 'fast',
          'answer': 'Ship it',
        });
        return http.Response(
          jsonEncode({
            'action_id': 'action_1',
            'run_id': 'run_1',
            'status': 'approved',
            'decision': 'answer',
            'selected_option_id': 'fast',
            'answer': 'Ship it',
            'decided_at': '2026-05-20T00:00:00Z',
          }),
          200,
        );
      }),
    );

    final decision = await client.decidePendingAction(
      'action_1',
      const PendingActionDecisionRequest(
        decision: 'answer',
        selectedOptionId: 'fast',
        answer: 'Ship it',
      ),
    );

    expect(decision.decision, 'answer');
    expect(decision.selectedOptionId, 'fast');
    expect(decision.answer, 'Ship it');
  });

  test('getRunDetail parses workbench artifacts', () async {
    final client = AcornApiClient(
      serverUrl: 'http://acorn.local',
      accessToken: 'device_token',
      httpClient: MockClient((request) async {
        expect(request.method, 'GET');
        expect(
          request.url.toString(),
          'http://acorn.local/v1/runs/run_1/detail',
        );
        expect(request.headers['Authorization'], 'Bearer device_token');
        return http.Response(
          jsonEncode({
            'run': {
              'id': 'run_1',
              'thread_id': 'thread_1',
              'status': 'succeeded',
              'mode': 'plan_execute',
              'created_at': '2026-05-20T00:00:00Z',
            },
            'thread': {
              'id': 'thread_1',
              'title': 'Artifact run',
              'workspace_root': '/repo',
              'created_at': '2026-05-20T00:00:00Z',
              'updated_at': '2026-05-20T00:00:00Z',
              'state': 'completed',
            },
            'events': [],
            'workbench': {
              'session_id': 'thread_1',
              'title': 'Artifact run',
              'resumable': false,
              'workspace_root': '/repo',
              'git_status': {
                'workspace_root': '/repo',
                'available': true,
                'clean': true,
              },
              'context_economy': {
                'tool_result_count': 1,
                'elided_tool_result_count': 0,
                'tool_result_token_estimate': 12,
              },
              'provider_usage': {
                'call_count': 1,
                'prompt_tokens': 10,
                'completion_tokens': 2,
                'total_tokens': 12,
                'cached_tokens': 0,
                'reasoning_tokens': 0,
              },
              'artifacts': [
                {
                  'artifact_id': 'artifact_report',
                  'run_id': 'run_1',
                  'session_id': 'thread_1',
                  'source_tool_result_ref': 'tool_result:run_1:call_1',
                  'kind': 'markdown',
                  'title': 'Report',
                  'mime_type': 'text/markdown',
                  'size_bytes': 42,
                  'sha256':
                      'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
                  'created_at': '2026-05-20T00:00:01Z',
                },
              ],
            },
            'trace': null,
            'raw': {'unsupported_events': []},
          }),
          200,
        );
      }),
    );

    final detail = await client.getRunDetail('run_1');
    final artifact = detail.artifacts.single;
    expect(artifact.artifactId, 'artifact_report');
    expect(artifact.sourceToolResultRef, 'tool_result:run_1:call_1');
    expect(artifact.kind, 'markdown');
    expect(artifact.sizeBytes, 42);
  });

  test(
    'listMemoryFacts sends include flags and parses record v2 fields',
    () async {
      final client = AcornApiClient(
        serverUrl: 'http://acorn.local',
        accessToken: 'device_token',
        httpClient: MockClient((request) async {
          expect(request.method, 'GET');
          expect(request.url.path, '/v1/memory/facts');
          expect(request.url.queryParameters, {
            'limit': '5',
            'include_inactive': 'true',
            'include_retired': 'true',
          });
          expect(request.headers['Authorization'], 'Bearer device_token');
          return http.Response(
            jsonEncode({
              'items': [
                {
                  'ref': 'facts/workspaces/acorn/repo.md#repo-root',
                  'kind': 'fact',
                  'title': 'Repo root',
                  'status': 'verified',
                  'scope': 'workspace:acorn',
                  'tags': ['repo'],
                  'path': 'facts/workspaces/acorn/repo.md',
                  'body': 'repo root is /repo',
                  'created': '2026-05-02',
                  'updated': '2026-05-02',
                  'valid_from': '2026-05-01',
                  'source_run': 'run_1',
                  'source_refs': ['history/thread_1.md#summary'],
                  'evidence_refs': ['runs/run_1/events/7'],
                  'relations': [
                    {
                      'type': 'supports',
                      'target':
                          'skills/learned/release-closeout.md#release-closeout',
                      'reason': 'same workflow evidence',
                    },
                  ],
                },
              ],
            }),
            200,
          );
        }),
      );

      final response = await client.listMemoryFacts(
        limit: 5,
        includeInactive: true,
        includeRetired: true,
      );

      final item = response.items.single;
      expect(item.ref, 'facts/workspaces/acorn/repo.md#repo-root');
      expect(item.scope, 'workspace:acorn');
      expect(item.tags, ['repo']);
      expect(item.validFrom, '2026-05-01');
      expect(item.sourceRefs, ['history/thread_1.md#summary']);
      expect(item.evidenceRefs, ['runs/run_1/events/7']);
      expect(item.relations.single.type, 'supports');
    },
  );

  test('searchMemory sends typed filters and parses search metadata', () async {
    final client = AcornApiClient(
      serverUrl: 'http://acorn.local',
      accessToken: 'device_token',
      httpClient: MockClient((request) async {
        expect(request.method, 'GET');
        expect(request.url.path, '/v1/memory/search');
        expect(request.url.queryParameters, {
          'limit': '7',
          'include_inactive': 'true',
          'query': 'repo',
          'kind': 'history',
          'scope': 'workspace:acorn',
        });
        return http.Response(
          jsonEncode({
            'items': [
              {
                'ref': 'history/thread_1.md',
                'kind': 'history',
                'title': 'thread_1',
                'status': 'verified',
                'scope': 'workspace:acorn',
                'tags': ['history'],
                'path': 'history/thread_1.md',
                'snippet': 'history hit from previous run',
                'score': 1.25,
                'created': '2026-05-02',
                'updated': '2026-05-02',
                'source_run': 'run_1',
                'source_refs': ['facts/workspaces/acorn/repo.md#repo-root'],
                'evidence_refs': ['runs/run_1/events/11'],
                'relations': [
                  {
                    'type': 'derived_from',
                    'target': 'facts/workspaces/acorn/repo.md#repo-root',
                  },
                ],
              },
            ],
          }),
          200,
        );
      }),
    );

    final response = await client.searchMemory(
      query: ' repo ',
      kind: 'history',
      scope: 'workspace:acorn',
      limit: 7,
      includeInactive: true,
    );

    final item = response.items.single;
    expect(item.kind, 'history');
    expect(item.score, 1.25);
    expect(item.sourceRun, 'run_1');
    expect(item.relations.single.type, 'derived_from');
  });

  test('deleteThread sends authenticated delete request', () async {
    final client = AcornApiClient(
      serverUrl: 'http://acorn.local',
      accessToken: 'device_token',
      httpClient: MockClient((request) async {
        expect(request.method, 'DELETE');
        expect(
          request.url.toString(),
          'http://acorn.local/v1/threads/thread_1',
        );
        expect(request.headers['Authorization'], 'Bearer device_token');
        return http.Response('', 204);
      }),
    );

    await expectLater(client.deleteThread('thread_1'), completes);
  });

  test(
    'registerDevicePushToken sends token without expecting it back',
    () async {
      final client = AcornApiClient(
        serverUrl: 'http://acorn.local',
        accessToken: 'device_token',
        httpClient: MockClient((request) async {
          expect(request.method, 'PUT');
          expect(
            request.url.toString(),
            'http://acorn.local/v1/devices/device_1/push-token',
          );
          expect(request.headers['Authorization'], 'Bearer device_token');
          expect(jsonDecode(request.body), {
            'provider': 'apns',
            'platform': 'ios',
            'token': 'secret-token',
          });
          return http.Response(
            jsonEncode({
              'device_id': 'device_1',
              'provider': 'apns',
              'platform': 'ios',
              'updated_at': '2026-05-15T00:00:00Z',
            }),
            200,
          );
        }),
      );

      final token = await client.registerDevicePushToken(
        'device_1',
        const RegisterDevicePushTokenRequest(
          provider: 'apns',
          platform: 'ios',
          token: 'secret-token',
        ),
      );

      expect(token.provider, 'apns');
      expect(token.updatedAt, '2026-05-15T00:00:00Z');
    },
  );

  test('revokeDevicePushToken accepts empty success body', () async {
    final client = AcornApiClient(
      serverUrl: 'http://acorn.local',
      accessToken: 'device_token',
      httpClient: MockClient((request) async {
        expect(request.method, 'DELETE');
        expect(
          request.url.toString(),
          'http://acorn.local/v1/devices/device_1/push-token/apns',
        );
        expect(request.headers['Authorization'], 'Bearer device_token');
        return http.Response('', 204);
      }),
    );

    await expectLater(
      client.revokeDevicePushToken('device_1', 'apns'),
      completes,
    );
  });

  test('backend errors throw AcornApiException', () async {
    final client = AcornApiClient(
      serverUrl: 'http://acorn.local',
      accessToken: 'device_token',
      httpClient: MockClient((request) async {
        return http.Response(
          jsonEncode({
            'error': {'code': 'unauthorized', 'message': 'bad token'},
          }),
          401,
        );
      }),
    );

    expect(
      client.getInbox(),
      throwsA(
        isA<AcornApiException>()
            .having((error) => error.statusCode, 'statusCode', 401)
            .having((error) => error.code, 'code', 'unauthorized')
            .having((error) => error.message, 'message', 'bad token'),
      ),
    );
  });
}

Map<String, Object?> _inboxJson() {
  return {
    'system': {
      'runtime_readiness': {'status': 'ready', 'reason': null},
      'model': {'name': 'gpt-test', 'provider': 'openai'},
      'workspace_root': '/tmp/acorn',
      'summary': {'total_threads': 1, 'active_runs': 1, 'pending_actions': 1},
      'features': {
        'approvals': true,
        'skills': true,
        'workspace_mutation': true,
      },
      'provider_readiness': [],
    },
    'pending_actions': [
      {
        'action_id': 'act_1',
        'run_id': 'run_1',
        'thread_id': 'thread_1',
        'kind': 'elicitation',
        'status': 'pending',
        'title': 'Approve tool',
        'body': 'run command',
        'options': [
          {'id': 'accept', 'label': 'Accept', 'description': 'Allow'},
          {'id': 'decline', 'label': 'Decline'},
        ],
        'created_at': '2026-05-15T00:00:00Z',
      },
    ],
    'active_runs': [
      {
        'run_id': 'run_1',
        'thread_id': 'thread_1',
        'thread_title': 'Deploy Acorn',
        'status': 'running',
        'mode': 'plan_execute',
        'preview': 'Run the release workflow',
        'last_event_label': 'Run is running',
        'attention_level': 'running',
        'duration_ms': 1000,
        'created_at': '2026-05-15T00:00:00Z',
        'updated_at': '2026-05-15T00:00:01Z',
      },
    ],
    'recent_terminal_runs': [],
  };
}
