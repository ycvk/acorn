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

  test('preserves live run failure feedback after message reload', () {
    final persisted = chatItemsFromMessages([
      const Message(
        id: 'msg_user',
        threadId: 'thread_1',
        role: 'user',
        contentText: 'hello',
        contentParts: [MessagePart(kind: 'text', text: 'hello')],
        createdAt: '2026-05-16T00:00:00Z',
      ),
    ]);
    const failedAssistant = ChatItem.message(
      id: 'assistant:run_1',
      role: ChatRole.assistant,
      text: '',
      createdAt: '2026-05-16T00:00:01Z',
      runId: 'run_1',
      status: ChatRunStatus.failed,
    );
    const failedActivity = ChatItem.activity(
      id: 'activity:event_1',
      runId: 'run_1',
      eventType: 'run.failed',
      text: 'Run failed',
      detail: 'provider api key is missing',
      createdAt: '2026-05-16T00:00:02Z',
    );

    final merged = mergePersistedChatItemsWithLiveRunFeedback(
      persisted: persisted,
      live: const [failedAssistant, failedActivity],
    );

    expect(merged.map((item) => item.id), [
      'msg_user',
      'assistant:run_1',
      'activity:event_1',
    ]);
    expect(merged[1].status, ChatRunStatus.failed);
    expect(merged[2].detail, 'provider api key is missing');
  });

  test('does not duplicate live assistant when persisted assistant exists', () {
    final persisted = chatItemsFromMessages([
      const Message(
        id: 'msg_assistant',
        threadId: 'thread_1',
        role: 'assistant',
        contentText: 'done',
        contentParts: [MessagePart(kind: 'text', text: 'done')],
        createdAt: '2026-05-16T00:00:02Z',
        runId: 'run_1',
      ),
    ]);
    const liveAssistant = ChatItem.message(
      id: 'assistant:run_1',
      role: ChatRole.assistant,
      text: 'done',
      createdAt: '2026-05-16T00:00:01Z',
      runId: 'run_1',
      status: ChatRunStatus.completed,
    );

    final merged = mergePersistedChatItemsWithLiveRunFeedback(
      persisted: persisted,
      live: const [liveAssistant],
    );

    expect(merged, hasLength(1));
    expect(merged.single.id, 'msg_assistant');
  });

  test('suppresses routine run trace events from chat activity', () {
    for (final type in const [
      'memory.prepared',
      'tool.call.started',
      'tool.call.progress',
      'tool.call.succeeded',
      'tool.call.failed',
      'skill.selected',
      'skill.loaded',
      'procedure.activation',
      'plan.created',
      'step.started',
      'step.completed',
      'subagent.started',
      'subagent.completed',
    ]) {
      expect(
        activityFromEvent(
          _event(type, {
            'tool_call': {
              'call_id': 'call_1',
              'name': 'run_command',
              'delta': 'ok',
              'error': 'failed',
            },
          }),
        ),
        isNull,
        reason: type,
      );
    }
  });

  test('shows terminal run failure in chat activity', () {
    final event = _event('run.failed', {
      'error': 'provider api key is missing',
    });

    final item = activityFromEvent(event);

    expect(item, isNotNull);
    expect(item!.kind, ChatItemKind.activity);
    expect(item.text, 'Run failed');
    expect(item.detail, 'provider api key is missing');
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
