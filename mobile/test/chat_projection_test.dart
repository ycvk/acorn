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

  test('reads assistant delta message identity', () {
    final event = _event('assistant.delta', {
      'assistant_delta': {
        'message_id': 'run_1:assistant:1',
        'sequence': 1,
        'delta': 'hello',
      },
    });

    expect(assistantDeltaMessageId(event), 'run_1:assistant:1');
  });

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

  test(
    'keeps live multi-segment assistant transcript after message reload',
    () {
      final persisted = chatItemsFromMessages([
        const Message(
          id: 'msg_user',
          threadId: 'thread_1',
          role: 'user',
          contentText: 'hello',
          contentParts: [MessagePart(kind: 'text', text: 'hello')],
          createdAt: '2026-05-16T00:00:00Z',
        ),
        const Message(
          id: 'msg_assistant',
          threadId: 'thread_1',
          role: 'assistant',
          contentText: 'Second pass',
          contentParts: [MessagePart(kind: 'text', text: 'Second pass')],
          createdAt: '2026-05-16T00:00:05Z',
          runId: 'run_1',
        ),
      ]);
      const firstLive = ChatItem.message(
        id: 'assistant:run_1:run_1:assistant:0',
        role: ChatRole.assistant,
        text: 'First pass',
        reasoning: 'thinking before tool',
        createdAt: '2026-05-16T00:00:01Z',
        runId: 'run_1',
        status: ChatRunStatus.idle,
      );
      const secondLive = ChatItem.message(
        id: 'assistant:run_1:run_1:assistant:1',
        role: ChatRole.assistant,
        text: 'Second pass',
        reasoning: 'thinking after tool',
        createdAt: '2026-05-16T00:00:04Z',
        runId: 'run_1',
        status: ChatRunStatus.completed,
      );

      final merged = mergePersistedChatItemsWithLiveRunFeedback(
        persisted: persisted,
        live: const [firstLive, secondLive],
      );

      expect(merged.map((item) => item.id), [
        'msg_user',
        'assistant:run_1:run_1:assistant:0',
        'assistant:run_1:run_1:assistant:1',
      ]);
      expect(merged.map((item) => item.text), [
        'hello',
        'First pass',
        'Second pass',
      ]);
    },
  );

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

  test('keeps native workflow tool progress out of chat activity', () {
    for (final toolName in const [
      'multi_edit',
      'run_verification',
      'git_summary',
      'artifact_write',
    ]) {
      for (final type in const [
        'tool.call.started',
        'tool.call.progress',
        'tool.call.succeeded',
        'tool.call.failed',
      ]) {
        expect(
          activityFromEvent(
            _event(type, {
              'tool_call': {
                'call_id': 'call_$toolName',
                'name': toolName,
                'delta': '$toolName output',
                'error': '$toolName failed',
              },
            }),
          ),
          isNull,
          reason: '$type $toolName',
        );
      }
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

  test('suppresses context pressure in chat activity', () {
    final event = _event('context.pressure', {
      'context_pressure': {'state': 'warning', 'percent_used': 78},
    });

    expect(activityFromEvent(event), isNull);
  });

  test('suppresses context compressed in chat activity', () {
    final event = _event('context.compressed', {
      'context_compressed': {'summary_snippet': 'older turns summarized'},
    });

    expect(activityFromEvent(event), isNull);
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
