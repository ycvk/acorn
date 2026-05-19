import 'package:acorn_mobile/src/api/acorn_api.dart';
import 'package:acorn_mobile/src/features/chat/chat_models.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('reads assistant deltas from RunEvent payloads', () {
    final event = _event('assistant.delta', {
      'assistant_delta': {'delta': 'hello'},
    });

    expect(assistantDeltaText(event), 'hello');
  });

  test(
    'reads reasoning-only assistant deltas without making activity rows',
    () {
      final event = _event('assistant.delta', {
        'assistant_delta': {'sequence': 1, 'reasoning': 'thinking '},
      });

      expect(assistantDeltaText(event), isNull);
      expect(assistantDeltaReasoning(event), 'thinking ');
      expect(activityFromEvent(event), isNull);
    },
  );

  test('reads reasoning from final assistant message payloads', () {
    final event = _event('run.completed', {
      'message': {
        'role': 'assistant',
        'content': 'done',
        'reasoning': 'checked tool evidence before answering',
      },
    });

    expect(agentMessageText(event), 'done');
    expect(
      agentMessageReasoning(event),
      'checked tool evidence before answering',
    );
    expect(statusFromTerminalEvent(event), ChatRunStatus.completed);
  });

  test('reads final assistant message content', () {
    final event = _event('run.completed', {
      'message': {'role': 'assistant', 'content': 'done'},
    });

    expect(agentMessageText(event), 'done');
    expect(statusFromTerminalEvent(event), ChatRunStatus.completed);
  });

  test('projects persisted reasoning message parts into chat items', () {
    final items = chatItemsFromMessages([
      const Message(
        id: 'msg_1',
        threadId: 'thread_1',
        role: 'assistant',
        contentText: 'Final answer',
        contentParts: [
          MessagePart(kind: 'text', text: 'Final answer'),
          MessagePart(kind: 'reasoning', reasoning: 'inspected context'),
          MessagePart(kind: 'reasoning', reasoning: 'verified tool output'),
        ],
        createdAt: '2026-05-16T00:00:00Z',
        runId: 'run_1',
      ),
    ]);

    expect(items.single.text, 'Final answer');
    expect(items.single.reasoning, 'inspected context\n\nverified tool output');
    expect(items.single.hasReasoning, isTrue);
  });

  test('builds compact activity rows for tool progress', () {
    final event = _event('tool.call.progress', {
      'tool_call': {'call_id': 'call_1', 'name': 'run_command', 'delta': 'ok'},
    });

    final item = activityFromEvent(event);

    expect(item, isNotNull);
    expect(item!.kind, ChatItemKind.activity);
    expect(item.text, 'Tool output');
    expect(item.detail, contains('run_command'));
    expect(item.detail, contains('ok'));
  });

  test('suppresses normal context pressure in chat activity', () {
    final event = _event('context.pressure', {
      'context_pressure': {'state': 'normal', 'percent_used': 10},
    });

    expect(activityFromEvent(event), isNull);
  });

  test('shows warning context pressure with compact detail', () {
    final event = _event('context.pressure', {
      'context_pressure': {
        'state': 'warning',
        'percent_used': 78,
        'estimated_input_tokens': 7800,
        'effective_window_tokens': 10000,
      },
    });

    final item = activityFromEvent(event);

    expect(item, isNotNull);
    expect(item!.text, 'Context pressure');
    expect(item.detail, 'warning · 78% · 7800/10000 tokens');
  });
}

RunEvent _event(String type, Map<String, dynamic> data) {
  return RunEvent(
    eventId: 'event_1',
    runId: 'run_1',
    seq: 1,
    ts: '2026-05-16T00:00:00Z',
    type: type,
    data: data,
  );
}
