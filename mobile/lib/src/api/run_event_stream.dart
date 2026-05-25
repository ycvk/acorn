import 'dart:async';
import 'dart:convert';

import 'package:http/http.dart' as http;

import '../core/connection_profile.dart';
import 'acorn_api.dart';

class RunEventStreamException implements Exception {
  const RunEventStreamException(this.message);

  final String message;

  @override
  String toString() => 'RunEventStreamException($message)';
}

class RunEventStreamClient {
  RunEventStreamClient({
    required ConnectionProfile profile,
    http.Client? httpClient,
  }) : _profile = profile,
       _http = httpClient ?? http.Client();

  final ConnectionProfile _profile;
  final http.Client _http;

  Stream<RunEvent> followRunEvents(String runId, {int afterSeq = 0}) async* {
    if (afterSeq < 0) {
      throw const RunEventStreamException('afterSeq must be non-negative.');
    }

    final request = http.Request('GET', _eventsUri(runId, afterSeq))
      ..headers['Accept'] = 'text/event-stream'
      ..headers['Authorization'] = 'Bearer ${_profile.accessToken.trim()}';

    final response = await _http.send(request);
    if (response.statusCode < 200 || response.statusCode >= 300) {
      throw await _decodeStreamError(response);
    }

    final parser = _SseParser(expectedRunId: runId);
    await for (final chunk in response.stream.transform(utf8.decoder)) {
      final events = parser.feed(chunk);
      for (final event in events) {
        yield event;
      }
    }

    for (final event in parser.close()) {
      yield event;
    }
  }

  void close() {
    _http.close();
  }

  Uri _eventsUri(String runId, int afterSeq) {
    final base = Uri.parse(_profile.serverUrl.trim());
    final path = _joinPaths(
      base.path,
      '/v1/runs/${Uri.encodeComponent(runId)}/events',
    );
    return base.replace(
      path: path,
      queryParameters: {'after_seq': '$afterSeq', 'follow': 'true'},
    );
  }

  static String _joinPaths(String basePath, String path) {
    final cleanBase = basePath.endsWith('/')
        ? basePath.substring(0, basePath.length - 1)
        : basePath;
    return '$cleanBase$path';
  }
}

Future<Exception> _decodeStreamError(http.StreamedResponse response) async {
  final body = await response.stream.bytesToString();
  if (body.trim().isEmpty) {
    return AcornApiException(
      response.statusCode,
      'http_error',
      'HTTP ${response.statusCode}',
    );
  }
  final decoded = jsonDecode(body);
  if (decoded is Map<String, dynamic>) {
    final error = decoded['error'];
    if (error is Map<String, dynamic>) {
      return AcornApiException(
        response.statusCode,
        _string(error['code']),
        _string(error['message']),
      );
    }
  }
  return AcornApiException(
    response.statusCode,
    'invalid_error_response',
    'RunEvent stream error body is not an Acorn error object.',
  );
}

class _SseParser {
  _SseParser({required this.expectedRunId});

  final String expectedRunId;
  String _buffer = '';

  List<RunEvent> feed(String chunk) {
    _buffer += chunk.replaceAll('\r\n', '\n');
    final events = <RunEvent>[];
    var separator = _buffer.indexOf('\n\n');
    while (separator >= 0) {
      final frame = _buffer.substring(0, separator);
      _buffer = _buffer.substring(separator + 2);
      final event = _parseFrame(frame);
      if (event != null) {
        events.add(event);
      }
      separator = _buffer.indexOf('\n\n');
    }
    return events;
  }

  List<RunEvent> close() {
    final trailing = _buffer.trim();
    if (trailing.isEmpty) {
      return const [];
    }
    _buffer = '';
    final event = _parseFrame(trailing);
    return event == null ? const [] : [event];
  }

  RunEvent? _parseFrame(String frame) {
    String? id;
    String? eventType;
    final dataLines = <String>[];

    for (final rawLine in frame.split('\n')) {
      final line = rawLine.endsWith('\r')
          ? rawLine.substring(0, rawLine.length - 1)
          : rawLine;
      if (line.isEmpty || line.startsWith(':')) {
        continue;
      }
      final colon = line.indexOf(':');
      final field = colon >= 0 ? line.substring(0, colon) : line;
      var value = colon >= 0 ? line.substring(colon + 1) : '';
      if (value.startsWith(' ')) {
        value = value.substring(1);
      }
      switch (field) {
        case 'id':
          id = value;
        case 'event':
          eventType = value;
        case 'data':
          dataLines.add(value);
      }
    }

    if (dataLines.isEmpty) {
      return null;
    }

    final decoded = jsonDecode(dataLines.join('\n'));
    if (decoded is! Map<String, dynamic>) {
      throw const RunEventStreamException(
        'RunEvent data must be a JSON object.',
      );
    }
    final event = RunEvent.fromJson(decoded);
    validateRunEvent(event, expectedRunId: expectedRunId);
    if (id != null && id.isNotEmpty && id != event.eventId) {
      throw RunEventStreamException(
        'RunEvent SSE id mismatch: header=$id data=${event.eventId}',
      );
    }
    if (eventType != null && eventType.isNotEmpty && eventType != event.type) {
      throw RunEventStreamException(
        'RunEvent SSE event type mismatch: header=$eventType data=${event.type}',
      );
    }
    return event;
  }
}

void validateRunEvent(RunEvent event, {String? expectedRunId}) {
  if (event.eventId.isEmpty) {
    throw const RunEventStreamException('RunEvent.event_id is required.');
  }
  if (event.runId.isEmpty) {
    throw const RunEventStreamException('RunEvent.run_id is required.');
  }
  if (expectedRunId != null && event.runId != expectedRunId) {
    throw RunEventStreamException(
      'RunEvent.run_id mismatch: expected=$expectedRunId actual=${event.runId}',
    );
  }
  if (event.seq < 0) {
    throw const RunEventStreamException('RunEvent.seq must be non-negative.');
  }
  if (event.ts.isEmpty) {
    throw const RunEventStreamException('RunEvent.ts is required.');
  }
  if (!_supportedRunEventTypes.contains(event.type)) {
    throw RunEventStreamException(
      'RunEvent.type is not supported by the mobile client: ${event.type}',
    );
  }
}

const _supportedRunEventTypes = {
  'run.started',
  'assistant.delta',
  'agent.message',
  'tool.call.started',
  'tool.call.progress',
  'tool.call.succeeded',
  'tool.call.failed',
  'tool.call.interrupted',
  'run.completed',
  'run.failed',
  'run.interrupted',
  'run.resume_requested',
  'elicitation.pending',
  'elicitation.decided',
  'operator_question.pending',
  'operator_question.decided',
  'provider.degraded',
  'mcp.tool_catalog_refreshed',
  'mcp.tool_catalog_refresh_failed',
  'mcp.provider_added',
  'mcp.provider_removed',
  'mcp.provider_restarted',
  'mcp.resource_catalog_refreshed',
  'mcp.resource_catalog_refresh_failed',
  'mcp.prompt_catalog_refreshed',
  'mcp.prompt_catalog_refresh_failed',
  'mcp.auth_status_changed',
  'sampling.started',
  'sampling.completed',
  'sampling.failed',
  'decision_selected',
  'decision_blocked',
  'skill.discovered',
  'skill.selected',
  'skill.loaded',
  'skill.failed',
  'skill.lifecycle',
  'procedure.activation',
  'memory.prepared',
  'context.pressure',
  'context.compressed',
  'crystallization.verdict',
  'crystallization.failed',
  'plan.created',
  'plan.updated',
  'plan.cleared',
  'step.started',
  'step.completed',
  'step.failed',
  'subagent.started',
  'subagent.completed',
  'subagent.failed',
};

String _string(Object? value) => value is String ? value : '';
