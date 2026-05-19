#!/usr/bin/env python3
import argparse
import json
from pathlib import Path
import subprocess
import sys
import tempfile

import yaml


ROOT = Path(__file__).resolve().parents[2]
OPENAPI = ROOT / "docs" / "openapi.yaml"
OUT = ROOT / "mobile" / "lib" / "src" / "api" / "acorn_api.dart"

REQUIRED_PATHS = [
    "/healthz",
    "/v1/devices:pair",
    "/v1/inbox",
    "/v1/devices/{device_id}/push-token",
    "/v1/devices/{device_id}/push-token/{provider}",
    "/v1/pending-actions",
    "/v1/pending-actions/{action_id}",
    "/v1/pending-actions/{action_id}:decide",
    "/v1/threads",
    "/v1/threads/{thread_id}",
    "/v1/threads/{thread_id}/messages",
    "/v1/threads/{thread_id}/runs",
    "/v1/runs/{run_id}",
    "/v1/runs/{run_id}/detail",
    "/v1/system/status",
    "/v1/memory/facts",
    "/v1/memory/skills",
    "/v1/memory/history",
    "/v1/memory/search",
]

REQUIRED_SCHEMAS = [
    "PairDeviceRequest",
    "PairDeviceResponse",
    "Device",
    "RegisterDevicePushTokenRequest",
    "DevicePushToken",
    "InboxResponse",
    "RunSummary",
    "PendingActionSummary",
    "PendingActionDetail",
    "PendingActionListResponse",
    "Thread",
    "ThreadListResponse",
    "Message",
    "MessageListResponse",
    "CreateMessageRequest",
    "CreateRunRequest",
    "Run",
    "RunDetail",
    "RunEvent",
    "SystemStatus",
    "MemoryRecordRelation",
    "MemoryRecord",
    "MemoryRecordListResponse",
    "MemorySearchItem",
    "MemorySearchResponse",
    "ErrorResponse",
]


def load_openapi() -> dict:
    with OPENAPI.open("r", encoding="utf-8") as fh:
        return yaml.safe_load(fh)


def validate_contract(doc: dict) -> None:
    paths = doc.get("paths") or {}
    schemas = ((doc.get("components") or {}).get("schemas") or {})
    missing_paths = [path for path in REQUIRED_PATHS if path not in paths]
    missing_schemas = [schema for schema in REQUIRED_SCHEMAS if schema not in schemas]
    if missing_paths or missing_schemas:
        parts = []
        if missing_paths:
            parts.append("missing paths: " + ", ".join(missing_paths))
        if missing_schemas:
            parts.append("missing schemas: " + ", ".join(missing_schemas))
        raise SystemExit("; ".join(parts))


def generated_source() -> str:
    return r'''// GENERATED CODE - DO NOT EDIT BY HAND.
// Source: docs/openapi.yaml
// Generator: mobile/tool/generate_openapi_client.py

import 'dart:async';
import 'dart:convert';

import 'package:http/http.dart' as http;

class AcornApiException implements Exception {
  const AcornApiException(this.statusCode, this.code, this.message);

  final int statusCode;
  final String code;
  final String message;

  @override
  String toString() => 'AcornApiException($statusCode, $code, $message)';
}

class AcornApiClient {
  AcornApiClient({
    required String serverUrl,
    String? accessToken,
    http.Client? httpClient,
  })  : _serverUrl = _normalizeServerUrl(serverUrl),
        _accessToken = accessToken,
        _http = httpClient ?? http.Client();

  final String _serverUrl;
  final String? _accessToken;
  final http.Client _http;

  String get serverUrl => _serverUrl;

  void close() {
    _http.close();
  }

  Future<HealthResponse> getHealth() async {
    final json = await _getJson('/healthz', authenticated: false);
    return HealthResponse.fromJson(json);
  }

  Future<PairDeviceResponse> pairDevice(PairDeviceRequest request) async {
    final json = await _postJson('/v1/devices:pair', request.toJson(), authenticated: false);
    return PairDeviceResponse.fromJson(json);
  }

  Future<DevicePushToken> registerDevicePushToken(String deviceId, RegisterDevicePushTokenRequest request) async {
    final json = await _putJson('/v1/devices/${Uri.encodeComponent(deviceId)}/push-token', request.toJson());
    return DevicePushToken.fromJson(json);
  }

  Future<void> revokeDevicePushToken(String deviceId, String provider) async {
    final response = await _http.delete(
      _uri('/v1/devices/${Uri.encodeComponent(deviceId)}/push-token/${Uri.encodeComponent(provider)}', null),
      headers: _headers(true),
    );
    _decodeEmptyResponse(response);
  }

  Future<InboxResponse> getInbox() async {
    final json = await _getJson('/v1/inbox');
    return InboxResponse.fromJson(json);
  }

  Future<PendingActionListResponse> listPendingActions({int limit = 20}) async {
    final json = await _getJson('/v1/pending-actions', query: {'limit': '$limit'});
    return PendingActionListResponse.fromJson(json);
  }

  Future<PendingActionDetail> getPendingAction(String actionId) async {
    final json = await _getJson('/v1/pending-actions/${Uri.encodeComponent(actionId)}');
    return PendingActionDetail.fromJson(json);
  }

  Future<PendingActionDecision> decidePendingAction(String actionId, String decision) async {
    final json = await _postJson(
      '/v1/pending-actions/${Uri.encodeComponent(actionId)}:decide',
      {'decision': decision},
    );
    return PendingActionDecision.fromJson(json);
  }

  Future<ThreadListResponse> listThreads({int limit = 40}) async {
    final json = await _getJson('/v1/threads', query: {'limit': '$limit'});
    return ThreadListResponse.fromJson(json);
  }

  Future<Thread> createThread({String? title}) async {
    final json = await _postJson('/v1/threads', {
      if (title != null && title.trim().isNotEmpty) 'title': title.trim(),
    });
    return Thread.fromJson(json);
  }

  Future<void> deleteThread(String threadId) async {
    final response = await _http.delete(
      _uri('/v1/threads/${Uri.encodeComponent(threadId)}', null),
      headers: _headers(true),
    );
    _decodeEmptyResponse(response);
  }

  Future<MessageListResponse> listMessages(String threadId, {int limit = 120}) async {
    final json = await _getJson(
      '/v1/threads/${Uri.encodeComponent(threadId)}/messages',
      query: {'limit': '$limit'},
    );
    return MessageListResponse.fromJson(json);
  }

  Future<Message> createMessage(String threadId, String text) async {
    final json = await _postJson(
      '/v1/threads/${Uri.encodeComponent(threadId)}/messages',
      {
        'content': {'type': 'text', 'text': text},
      },
    );
    return Message.fromJson(json);
  }

  Future<Run> createRun(String threadId, {String? skillId, String? mode}) async {
    final json = await _postJson(
      '/v1/threads/${Uri.encodeComponent(threadId)}/runs',
      {
        if (skillId != null && skillId.trim().isNotEmpty) 'skill_id': skillId.trim(),
        if (mode != null && mode.trim().isNotEmpty) 'mode': mode.trim(),
      },
    );
    return Run.fromJson(json);
  }

  Future<Run> getRun(String runId) async {
    final json = await _getJson('/v1/runs/${Uri.encodeComponent(runId)}');
    return Run.fromJson(json);
  }

  Future<RunDetail> getRunDetail(String runId) async {
    final json = await _getJson('/v1/runs/${Uri.encodeComponent(runId)}/detail');
    return RunDetail.fromJson(json);
  }

  Future<SystemStatus> getSystemStatus() async {
    final json = await _getJson('/v1/system/status');
    return SystemStatus.fromJson(json);
  }

  Future<MemoryRecordListResponse> listMemoryFacts({
    int limit = 100,
    bool includeInactive = false,
    bool includeRetired = false,
  }) async {
    final json = await _getJson(
      '/v1/memory/facts',
      query: _memoryQuery(limit: limit, includeInactive: includeInactive, includeRetired: includeRetired),
    );
    return MemoryRecordListResponse.fromJson(json);
  }

  Future<MemoryRecordListResponse> listMemorySkills({
    int limit = 100,
    bool includeInactive = false,
    bool includeRetired = false,
  }) async {
    final json = await _getJson(
      '/v1/memory/skills',
      query: _memoryQuery(limit: limit, includeInactive: includeInactive, includeRetired: includeRetired),
    );
    return MemoryRecordListResponse.fromJson(json);
  }

  Future<MemoryRecordListResponse> listMemoryHistory({
    int limit = 100,
    bool includeInactive = false,
    bool includeRetired = false,
  }) async {
    final json = await _getJson(
      '/v1/memory/history',
      query: _memoryQuery(limit: limit, includeInactive: includeInactive, includeRetired: includeRetired),
    );
    return MemoryRecordListResponse.fromJson(json);
  }

  Future<MemorySearchResponse> searchMemory({
    String? query,
    String? kind,
    String? scope,
    int limit = 20,
    bool includeInactive = false,
    bool includeRetired = false,
  }) async {
    final params = _memoryQuery(limit: limit, includeInactive: includeInactive, includeRetired: includeRetired);
    if (query != null && query.trim().isNotEmpty) {
      params['query'] = query.trim();
    }
    if (kind != null && kind.trim().isNotEmpty) {
      params['kind'] = kind.trim();
    }
    if (scope != null && scope.trim().isNotEmpty) {
      params['scope'] = scope.trim();
    }
    final json = await _getJson('/v1/memory/search', query: params);
    return MemorySearchResponse.fromJson(json);
  }

  Map<String, String> _memoryQuery({
    required int limit,
    required bool includeInactive,
    required bool includeRetired,
  }) {
    return {
      'limit': '$limit',
      if (includeInactive) 'include_inactive': 'true',
      if (includeRetired) 'include_retired': 'true',
    };
  }

  Future<Map<String, dynamic>> _getJson(
    String path, {
    Map<String, String>? query,
    bool authenticated = true,
  }) async {
    final response = await _http.get(_uri(path, query), headers: _headers(authenticated));
    return _decodeResponse(response);
  }

  Future<Map<String, dynamic>> _postJson(
    String path,
    Map<String, dynamic> body, {
    bool authenticated = true,
  }) async {
    final response = await _http.post(
      _uri(path, null),
      headers: _headers(authenticated),
      body: jsonEncode(body),
    );
    return _decodeResponse(response);
  }

  Future<Map<String, dynamic>> _putJson(
    String path,
    Map<String, dynamic> body, {
    bool authenticated = true,
  }) async {
    final response = await _http.put(
      _uri(path, null),
      headers: _headers(authenticated),
      body: jsonEncode(body),
    );
    return _decodeResponse(response);
  }

  Uri _uri(String path, Map<String, String>? query) {
    final base = Uri.parse(_serverUrl);
    final normalizedPath = path.startsWith('/') ? path : '/$path';
    return base.replace(
      path: _joinPaths(base.path, normalizedPath),
      queryParameters: query,
    );
  }

  Map<String, String> _headers(bool authenticated) {
    return {
      'Accept': 'application/json',
      'Content-Type': 'application/json',
      if (authenticated) 'Authorization': 'Bearer ${_requiredToken()}',
    };
  }

  String _requiredToken() {
    final token = _accessToken?.trim();
    if (token == null || token.isEmpty) {
      throw const AcornApiException(401, 'missing_device_token', 'Device token is required.');
    }
    return token;
  }

  Map<String, dynamic> _decodeResponse(http.Response response) {
    final body = response.body.trim();
    if (response.statusCode >= 200 && response.statusCode < 300) {
      if (body.isEmpty) {
        return <String, dynamic>{};
      }
      final decoded = jsonDecode(body);
      if (decoded is Map<String, dynamic>) {
        return decoded;
      }
      throw AcornApiException(response.statusCode, 'invalid_response', 'Response body is not a JSON object.');
    }

    if (body.isNotEmpty) {
      final decoded = jsonDecode(body);
      if (decoded is Map<String, dynamic>) {
        final error = decoded['error'];
        if (error is Map<String, dynamic>) {
          throw AcornApiException(
            response.statusCode,
            _string(error['code']),
            _string(error['message']),
          );
        }
      }
    }
    throw AcornApiException(response.statusCode, 'http_error', 'HTTP ${response.statusCode}');
  }

  void _decodeEmptyResponse(http.Response response) {
    if (response.statusCode >= 200 && response.statusCode < 300) {
      return;
    }
    _decodeResponse(response);
  }

  static String _normalizeServerUrl(String raw) {
    final value = raw.trim();
    if (value.isEmpty) {
      throw ArgumentError.value(raw, 'serverUrl', 'Server URL is required.');
    }
    return value.endsWith('/') ? value.substring(0, value.length - 1) : value;
  }

  static String _joinPaths(String basePath, String path) {
    final cleanBase = basePath.endsWith('/') ? basePath.substring(0, basePath.length - 1) : basePath;
    return '$cleanBase$path';
  }
}

class HealthResponse {
  const HealthResponse({required this.ok});

  final bool ok;

  factory HealthResponse.fromJson(Map<String, dynamic> json) {
    return HealthResponse(ok: _bool(json['ok']));
  }
}

class PairDeviceRequest {
  const PairDeviceRequest({
    required this.pairingCode,
    required this.deviceName,
    required this.platform,
  });

  final String pairingCode;
  final String deviceName;
  final String platform;

  Map<String, dynamic> toJson() => {
        'pairing_code': pairingCode,
        'device_name': deviceName,
        'platform': platform,
      };
}

class PairDeviceResponse {
  const PairDeviceResponse({required this.device, required this.accessToken});

  final Device device;
  final String accessToken;

  factory PairDeviceResponse.fromJson(Map<String, dynamic> json) {
    return PairDeviceResponse(
      device: Device.fromJson(_map(json['device'])),
      accessToken: _string(json['access_token']),
    );
  }
}

class RegisterDevicePushTokenRequest {
  const RegisterDevicePushTokenRequest({
    required this.provider,
    required this.token,
    this.platform,
  });

  final String provider;
  final String token;
  final String? platform;

  Map<String, dynamic> toJson() => {
        'provider': provider,
        if (platform != null && platform!.trim().isNotEmpty) 'platform': platform!.trim(),
        'token': token,
      };
}

class DevicePushToken {
  const DevicePushToken({
    required this.deviceId,
    required this.provider,
    required this.platform,
    required this.updatedAt,
  });

  final String deviceId;
  final String provider;
  final String platform;
  final String updatedAt;

  factory DevicePushToken.fromJson(Map<String, dynamic> json) {
    return DevicePushToken(
      deviceId: _string(json['device_id']),
      provider: _string(json['provider']),
      platform: _string(json['platform']),
      updatedAt: _string(json['updated_at']),
    );
  }
}

class Device {
  const Device({
    required this.deviceId,
    required this.name,
    required this.platform,
    required this.createdAt,
    this.lastSeenAt,
  });

  final String deviceId;
  final String name;
  final String platform;
  final String createdAt;
  final String? lastSeenAt;

  factory Device.fromJson(Map<String, dynamic> json) {
    return Device(
      deviceId: _string(json['device_id']),
      name: _string(json['name']),
      platform: _string(json['platform']),
      createdAt: _string(json['created_at']),
      lastSeenAt: _nullableString(json['last_seen_at']),
    );
  }
}

class InboxResponse {
  const InboxResponse({
    required this.pendingActions,
    required this.activeRuns,
    required this.recentTerminalRuns,
    required this.system,
  });

  final List<PendingActionSummary> pendingActions;
  final List<RunSummary> activeRuns;
  final List<RunSummary> recentTerminalRuns;
  final SystemStatus system;

  factory InboxResponse.fromJson(Map<String, dynamic> json) {
    return InboxResponse(
      pendingActions: _list(json['pending_actions'], PendingActionSummary.fromJson),
      activeRuns: _list(json['active_runs'], RunSummary.fromJson),
      recentTerminalRuns: _list(json['recent_terminal_runs'], RunSummary.fromJson),
      system: SystemStatus.fromJson(_map(json['system'])),
    );
  }
}

class RunSummary {
  const RunSummary({
    required this.runId,
    required this.threadId,
    required this.status,
    required this.mode,
    required this.createdAt,
    required this.updatedAt,
  });

  final String runId;
  final String threadId;
  final String status;
  final String mode;
  final String createdAt;
  final String updatedAt;

  factory RunSummary.fromJson(Map<String, dynamic> json) {
    return RunSummary(
      runId: _string(json['run_id']),
      threadId: _string(json['thread_id']),
      status: _string(json['status']),
      mode: _string(json['mode']),
      createdAt: _string(json['created_at']),
      updatedAt: _string(json['updated_at']),
    );
  }
}

class PendingActionSummary {
  const PendingActionSummary({
    required this.actionId,
    required this.runId,
    required this.threadId,
    required this.kind,
    required this.status,
    required this.title,
    required this.options,
    required this.createdAt,
    this.body,
  });

  final String actionId;
  final String runId;
  final String threadId;
  final String kind;
  final String status;
  final String title;
  final String? body;
  final List<PendingActionOption> options;
  final String createdAt;

  factory PendingActionSummary.fromJson(Map<String, dynamic> json) {
    return PendingActionSummary(
      actionId: _string(json['action_id']),
      runId: _string(json['run_id']),
      threadId: _string(json['thread_id']),
      kind: _string(json['kind']),
      status: _string(json['status']),
      title: _string(json['title']),
      body: _nullableString(json['body']),
      options: _list(json['options'], PendingActionOption.fromJson),
      createdAt: _string(json['created_at']),
    );
  }
}

class PendingActionOption {
  const PendingActionOption({required this.id, required this.label});

  final String id;
  final String label;

  factory PendingActionOption.fromJson(Map<String, dynamic> json) {
    return PendingActionOption(id: _string(json['id']), label: _string(json['label']));
  }
}

class PendingActionListResponse {
  const PendingActionListResponse({required this.items});

  final List<PendingActionSummary> items;

  factory PendingActionListResponse.fromJson(Map<String, dynamic> json) {
    return PendingActionListResponse(items: _list(json['items'], PendingActionSummary.fromJson));
  }
}

class PendingActionDetail extends PendingActionSummary {
  const PendingActionDetail({
    required super.actionId,
    required super.runId,
    required super.threadId,
    required super.kind,
    required super.status,
    required super.title,
    required super.options,
    required super.createdAt,
    required this.payload,
    super.body,
    this.reason,
    this.rule,
  });

  final Map<String, dynamic> payload;
  final String? reason;
  final String? rule;

  factory PendingActionDetail.fromJson(Map<String, dynamic> json) {
    return PendingActionDetail(
      actionId: _string(json['action_id']),
      runId: _string(json['run_id']),
      threadId: _string(json['thread_id']),
      kind: _string(json['kind']),
      status: _string(json['status']),
      title: _string(json['title']),
      body: _nullableString(json['body']),
      options: _list(json['options'], PendingActionOption.fromJson),
      payload: _map(json['payload']),
      reason: _nullableString(json['reason']),
      rule: _nullableString(json['rule']),
      createdAt: _string(json['created_at']),
    );
  }
}

class PendingActionDecision {
  const PendingActionDecision({
    required this.actionId,
    required this.runId,
    required this.status,
    required this.selectedOptionId,
    this.decidedAt,
  });

  final String actionId;
  final String runId;
  final String status;
  final String selectedOptionId;
  final String? decidedAt;

  factory PendingActionDecision.fromJson(Map<String, dynamic> json) {
    return PendingActionDecision(
      actionId: _string(json['action_id']),
      runId: _string(json['run_id']),
      status: _string(json['status']),
      selectedOptionId: _string(json['selected_option_id']),
      decidedAt: _nullableString(json['decided_at']),
    );
  }
}

class ThreadListResponse {
  const ThreadListResponse({required this.items});

  final List<Thread> items;

  factory ThreadListResponse.fromJson(Map<String, dynamic> json) {
    return ThreadListResponse(items: _list(json['items'], Thread.fromJson));
  }
}

class Thread {
  const Thread({
    required this.id,
    required this.title,
    required this.workspaceRoot,
    required this.createdAt,
    required this.updatedAt,
    required this.state,
    this.latestRunId,
  });

  final String id;
  final String title;
  final String workspaceRoot;
  final String createdAt;
  final String updatedAt;
  final String state;
  final String? latestRunId;

  factory Thread.fromJson(Map<String, dynamic> json) {
    return Thread(
      id: _string(json['id']),
      title: _string(json['title']),
      workspaceRoot: _string(json['workspace_root']),
      createdAt: _string(json['created_at']),
      updatedAt: _string(json['updated_at']),
      latestRunId: _nullableString(json['latest_run_id']),
      state: _string(json['state']),
    );
  }
}

class MessageListResponse {
  const MessageListResponse({required this.items});

  final List<Message> items;

  factory MessageListResponse.fromJson(Map<String, dynamic> json) {
    return MessageListResponse(items: _list(json['items'], Message.fromJson));
  }
}

class Message {
  const Message({
    required this.id,
    required this.threadId,
    required this.role,
    required this.contentText,
    required this.contentParts,
    required this.createdAt,
    this.runId,
  });

  final String id;
  final String threadId;
  final String role;
  final String contentText;
  final List<MessagePart> contentParts;
  final String createdAt;
  final String? runId;

  factory Message.fromJson(Map<String, dynamic> json) {
    final content = _map(json['content']);
    return Message(
      id: _string(json['id']),
      threadId: _string(json['thread_id']),
      role: _string(json['role']),
      contentText: _string(content['text']),
      contentParts: _list(content['parts'], MessagePart.fromJson),
      createdAt: _string(json['created_at']),
      runId: _nullableString(json['run_id']),
    );
  }
}

class MessagePart {
  const MessagePart({
    required this.kind,
    this.text,
    this.reasoning,
    this.status,
    this.title,
    this.summary,
    this.detailRunId,
    this.runId,
    this.label,
  });

  final String kind;
  final String? text;
  final String? reasoning;
  final String? status;
  final String? title;
  final String? summary;
  final String? detailRunId;
  final String? runId;
  final String? label;

  factory MessagePart.fromJson(Map<String, dynamic> json) {
    return MessagePart(
      kind: _string(json['kind']),
      text: _nullableString(json['text']),
      reasoning: _nullableString(json['reasoning']),
      status: _nullableString(json['status']),
      title: _nullableString(json['title']),
      summary: _nullableString(json['summary']),
      detailRunId: _nullableString(json['detail_run_id']),
      runId: _nullableString(json['run_id']),
      label: _nullableString(json['label']),
    );
  }
}

class Run {
  const Run({
    required this.id,
    required this.threadId,
    required this.status,
    required this.mode,
    required this.createdAt,
    this.completedAt,
  });

  final String id;
  final String threadId;
  final String status;
  final String mode;
  final String createdAt;
  final String? completedAt;

  factory Run.fromJson(Map<String, dynamic> json) {
    return Run(
      id: _string(json['id']),
      threadId: _string(json['thread_id']),
      status: _string(json['status']),
      mode: _string(json['mode']),
      createdAt: _string(json['created_at']),
      completedAt: _nullableString(json['completed_at']),
    );
  }
}

class RunDetail {
  const RunDetail({
    required this.run,
    required this.thread,
    required this.events,
    required this.raw,
  });

  final Run run;
  final Thread thread;
  final List<RunEvent> events;
  final Map<String, dynamic> raw;

  factory RunDetail.fromJson(Map<String, dynamic> json) {
    return RunDetail(
      run: Run.fromJson(_map(json['run'])),
      thread: Thread.fromJson(_map(json['thread'])),
      events: _list(json['events'], RunEvent.fromJson),
      raw: Map<String, dynamic>.from(json),
    );
  }
}

class MemoryRecordRelation {
  const MemoryRecordRelation({
    required this.type,
    required this.target,
    this.reason,
  });

  final String type;
  final String target;
  final String? reason;

  factory MemoryRecordRelation.fromJson(Map<String, dynamic> json) {
    return MemoryRecordRelation(
      type: _string(json['type']),
      target: _string(json['target']),
      reason: _nullableString(json['reason']),
    );
  }
}

class MemoryRecord {
  const MemoryRecord({
    required this.ref,
    required this.kind,
    required this.title,
    required this.status,
    required this.path,
    required this.body,
    this.scope,
    this.tags = const [],
    this.origin,
    this.taskPattern,
    this.created,
    this.updated,
    this.validFrom,
    this.validUntil,
    this.sourceRun,
    this.sourceRefs = const [],
    this.evidenceRefs = const [],
    this.relations = const [],
  });

  final String ref;
  final String kind;
  final String title;
  final String status;
  final String? scope;
  final List<String> tags;
  final String? origin;
  final String? taskPattern;
  final String path;
  final String body;
  final String? created;
  final String? updated;
  final String? validFrom;
  final String? validUntil;
  final String? sourceRun;
  final List<String> sourceRefs;
  final List<String> evidenceRefs;
  final List<MemoryRecordRelation> relations;

  factory MemoryRecord.fromJson(Map<String, dynamic> json) {
    return MemoryRecord(
      ref: _string(json['ref']),
      kind: _string(json['kind']),
      title: _string(json['title']),
      status: _string(json['status']),
      scope: _nullableString(json['scope']),
      tags: _stringList(json['tags']),
      origin: _nullableString(json['origin']),
      taskPattern: _nullableString(json['task_pattern']),
      path: _string(json['path']),
      body: _string(json['body']),
      created: _nullableString(json['created']),
      updated: _nullableString(json['updated']),
      validFrom: _nullableString(json['valid_from']),
      validUntil: _nullableString(json['valid_until']),
      sourceRun: _nullableString(json['source_run']),
      sourceRefs: _stringList(json['source_refs']),
      evidenceRefs: _stringList(json['evidence_refs']),
      relations: _list(json['relations'], MemoryRecordRelation.fromJson),
    );
  }
}

class MemoryRecordListResponse {
  const MemoryRecordListResponse({required this.items});

  final List<MemoryRecord> items;

  factory MemoryRecordListResponse.fromJson(Map<String, dynamic> json) {
    return MemoryRecordListResponse(items: _list(json['items'], MemoryRecord.fromJson));
  }
}

class MemorySearchItem {
  const MemorySearchItem({
    required this.ref,
    required this.kind,
    required this.title,
    required this.status,
    required this.path,
    required this.snippet,
    required this.score,
    this.scope,
    this.tags = const [],
    this.origin,
    this.taskPattern,
    this.created,
    this.updated,
    this.validFrom,
    this.validUntil,
    this.sourceRun,
    this.sourceRefs = const [],
    this.evidenceRefs = const [],
    this.relations = const [],
  });

  final String ref;
  final String kind;
  final String title;
  final String status;
  final String? scope;
  final List<String> tags;
  final String? origin;
  final String? taskPattern;
  final String path;
  final String snippet;
  final double score;
  final String? created;
  final String? updated;
  final String? validFrom;
  final String? validUntil;
  final String? sourceRun;
  final List<String> sourceRefs;
  final List<String> evidenceRefs;
  final List<MemoryRecordRelation> relations;

  factory MemorySearchItem.fromJson(Map<String, dynamic> json) {
    return MemorySearchItem(
      ref: _string(json['ref']),
      kind: _string(json['kind']),
      title: _string(json['title']),
      status: _string(json['status']),
      scope: _nullableString(json['scope']),
      tags: _stringList(json['tags']),
      origin: _nullableString(json['origin']),
      taskPattern: _nullableString(json['task_pattern']),
      path: _string(json['path']),
      snippet: _string(json['snippet']),
      score: _double(json['score']),
      created: _nullableString(json['created']),
      updated: _nullableString(json['updated']),
      validFrom: _nullableString(json['valid_from']),
      validUntil: _nullableString(json['valid_until']),
      sourceRun: _nullableString(json['source_run']),
      sourceRefs: _stringList(json['source_refs']),
      evidenceRefs: _stringList(json['evidence_refs']),
      relations: _list(json['relations'], MemoryRecordRelation.fromJson),
    );
  }
}

class MemorySearchResponse {
  const MemorySearchResponse({required this.items});

  final List<MemorySearchItem> items;

  factory MemorySearchResponse.fromJson(Map<String, dynamic> json) {
    return MemorySearchResponse(items: _list(json['items'], MemorySearchItem.fromJson));
  }
}

class RunEvent {
  const RunEvent({
    required this.eventId,
    required this.runId,
    required this.seq,
    required this.ts,
    required this.type,
    required this.data,
  });

  final String eventId;
  final String runId;
  final int seq;
  final String ts;
  final String type;
  final Map<String, dynamic> data;

  factory RunEvent.fromJson(Map<String, dynamic> json) {
    return RunEvent(
      eventId: _string(json['event_id']),
      runId: _string(json['run_id']),
      seq: _int(json['seq']),
      ts: _string(json['ts']),
      type: _string(json['type']),
      data: _map(json['data']),
    );
  }
}

class SystemStatus {
  const SystemStatus({
    required this.runtimeReadiness,
    required this.model,
    required this.workspaceRoot,
    required this.summary,
    required this.features,
    this.providerReadiness = const [],
  });

  final RuntimeReadiness runtimeReadiness;
  final CapabilitiesModel model;
  final String workspaceRoot;
  final CapabilitiesSummary summary;
  final CapabilitiesFeatures features;
  final List<ProviderReadiness> providerReadiness;

  factory SystemStatus.fromJson(Map<String, dynamic> json) {
    return SystemStatus(
      runtimeReadiness: RuntimeReadiness.fromJson(_map(json['runtime_readiness'])),
      providerReadiness: _list(json['provider_readiness'], ProviderReadiness.fromJson),
      model: CapabilitiesModel.fromJson(_map(json['model'])),
      workspaceRoot: _string(json['workspace_root']),
      summary: CapabilitiesSummary.fromJson(_map(json['summary'])),
      features: CapabilitiesFeatures.fromJson(_map(json['features'])),
    );
  }
}

class RuntimeReadiness {
  const RuntimeReadiness({required this.status, this.reason});

  final String status;
  final String? reason;

  factory RuntimeReadiness.fromJson(Map<String, dynamic> json) {
    return RuntimeReadiness(status: _string(json['status']), reason: _nullableString(json['reason']));
  }
}

class ProviderReadiness {
  const ProviderReadiness({
    required this.scope,
    required this.provider,
    required this.status,
    this.reason,
  });

  final String scope;
  final String provider;
  final String status;
  final String? reason;

  factory ProviderReadiness.fromJson(Map<String, dynamic> json) {
    return ProviderReadiness(
      scope: _string(json['scope']),
      provider: _string(json['provider']),
      status: _string(json['status']),
      reason: _nullableString(json['reason']),
    );
  }
}

class CapabilitiesModel {
  const CapabilitiesModel({required this.name});

  final String name;

  factory CapabilitiesModel.fromJson(Map<String, dynamic> json) {
    return CapabilitiesModel(name: _string(json['name']));
  }
}

class CapabilitiesSummary {
  const CapabilitiesSummary({
    required this.toolCount,
    required this.enabledToolCount,
    required this.skillCount,
  });

  final int toolCount;
  final int enabledToolCount;
  final int skillCount;

  factory CapabilitiesSummary.fromJson(Map<String, dynamic> json) {
    return CapabilitiesSummary(
      toolCount: _int(json['tool_count']),
      enabledToolCount: _int(json['enabled_tool_count']),
      skillCount: _int(json['skill_count']),
    );
  }
}

class CapabilitiesFeatures {
  const CapabilitiesFeatures({
    required this.interruptResume,
    required this.traceDebug,
    required this.sessionHistory,
  });

  final bool interruptResume;
  final bool traceDebug;
  final bool sessionHistory;

  factory CapabilitiesFeatures.fromJson(Map<String, dynamic> json) {
    return CapabilitiesFeatures(
      interruptResume: _bool(json['interrupt_resume']),
      traceDebug: _bool(json['trace_debug']),
      sessionHistory: _bool(json['session_history']),
    );
  }
}

Map<String, dynamic> _map(Object? value) {
  if (value is Map<String, dynamic>) {
    return value;
  }
  if (value is Map) {
    return Map<String, dynamic>.from(value);
  }
  return <String, dynamic>{};
}

List<T> _list<T>(Object? value, T Function(Map<String, dynamic>) mapper) {
  if (value is! List) {
    return <T>[];
  }
  return value.map((item) => mapper(_map(item))).toList(growable: false);
}

List<String> _stringList(Object? value) {
  if (value is! List) {
    return <String>[];
  }
  return value.map(_string).where((item) => item.isNotEmpty).toList(growable: false);
}

String _string(Object? value) => value is String ? value : '';
String? _nullableString(Object? value) => value is String && value.isNotEmpty ? value : null;
bool _bool(Object? value) => value is bool ? value : false;
int _int(Object? value) => value is int ? value : value is num ? value.toInt() : 0;
double _double(Object? value) => value is double ? value : value is num ? value.toDouble() : 0.0;
'''


def format_dart_source(source: str) -> str:
    with tempfile.NamedTemporaryFile("w", encoding="utf-8", suffix=".dart", delete=False) as fh:
        path = Path(fh.name)
        fh.write(source)
    try:
        result = subprocess.run(
            ["dart", "format", "--output=json", str(path)],
            check=True,
            capture_output=True,
            text=True,
        )
    finally:
        path.unlink(missing_ok=True)
    payload = json.loads(result.stdout)
    return payload["source"]


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()

    validate_contract(load_openapi())
    source = format_dart_source(generated_source())
    if args.check:
        if not OUT.exists() or OUT.read_text(encoding="utf-8") != source:
            print(f"{OUT} is not up to date", file=sys.stderr)
            return 1
        print(f"{OUT} is up to date")
        return 0
    OUT.parent.mkdir(parents=True, exist_ok=True)
    OUT.write_text(source, encoding="utf-8")
    print(f"generated {OUT.relative_to(ROOT)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
