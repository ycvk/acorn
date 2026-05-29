import '../../api/acorn_api.dart';

enum ChatItemKind { message, activity }

enum ChatRole { user, assistant, system }

enum ChatRunStatus {
  idle,
  streaming,
  reconnecting,
  completed,
  failed,
  interrupted,
}

class ChatItem {
  const ChatItem.message({
    required this.id,
    required this.role,
    required this.text,
    required this.createdAt,
    this.reasoning = '',
    this.runId,
    this.status = ChatRunStatus.idle,
  }) : kind = ChatItemKind.message,
       eventType = null,
       detail = null;

  const ChatItem.activity({
    required this.id,
    required this.runId,
    required this.eventType,
    required this.text,
    required this.createdAt,
    this.detail,
  }) : kind = ChatItemKind.activity,
       role = ChatRole.system,
       reasoning = '',
       status = ChatRunStatus.idle;

  final String id;
  final ChatItemKind kind;
  final ChatRole role;
  final String text;
  final String reasoning;
  final String? detail;
  final String? runId;
  final String? eventType;
  final String createdAt;
  final ChatRunStatus status;

  bool get isUser => role == ChatRole.user;
  bool get isAssistant => role == ChatRole.assistant;
  bool get isStreaming =>
      status == ChatRunStatus.streaming || status == ChatRunStatus.reconnecting;
  bool get hasReasoning => reasoning.trim().isNotEmpty;

  ChatItem copyWith({
    String? id,
    String? text,
    String? reasoning,
    String? detail,
    ChatRunStatus? status,
  }) {
    if (kind == ChatItemKind.activity) {
      return ChatItem.activity(
        id: id ?? this.id,
        runId: runId ?? '',
        eventType: eventType ?? '',
        text: text ?? this.text,
        detail: detail ?? this.detail,
        createdAt: createdAt,
      );
    }
    return ChatItem.message(
      id: id ?? this.id,
      role: role,
      text: text ?? this.text,
      reasoning: reasoning ?? this.reasoning,
      createdAt: createdAt,
      runId: runId,
      status: status ?? this.status,
    );
  }
}

List<ChatItem> chatItemsFromMessages(List<Message> messages) {
  return messages
      .map(
        (message) => ChatItem.message(
          id: message.id,
          role: _roleFromMessage(message.role),
          text: message.contentText,
          reasoning: _reasoningFromMessage(message),
          createdAt: message.createdAt,
          runId: message.runId,
          status: ChatRunStatus.idle,
        ),
      )
      .toList(growable: false);
}

List<ChatItem> mergePersistedChatItemsWithLiveRunFeedback({
  required List<ChatItem> persisted,
  required List<ChatItem> live,
}) {
  final multiSegmentRunIDs = _multiSegmentAssistantRunIDs(live);
  final persistedAssistantRunIDs = persisted
      .where((item) => item.isAssistant && item.runId != null)
      .map((item) => item.runId!)
      .toSet();
  final filteredPersisted = persisted.where((item) {
    final runID = item.runId;
    if (item.isAssistant && runID != null) {
      return !multiSegmentRunIDs.contains(runID);
    }
    return true;
  });
  final preservedLiveItems = live.where((item) {
    if (item.kind == ChatItemKind.activity) {
      return true;
    }
    if (!item.isAssistant) {
      return false;
    }
    final runID = item.runId;
    if (runID == null || runID.isEmpty) {
      return false;
    }
    if (multiSegmentRunIDs.contains(runID)) {
      return true;
    }
    if (persistedAssistantRunIDs.contains(runID)) {
      return false;
    }
    return !item.isStreaming ||
        item.text.trim().isNotEmpty ||
        item.reasoning.trim().isNotEmpty;
  });
  return [...filteredPersisted, ...preservedLiveItems];
}

Set<String> _multiSegmentAssistantRunIDs(List<ChatItem> items) {
  final counts = <String, int>{};
  for (final item in items) {
    final runID = item.runId;
    if (!item.isAssistant || runID == null || runID.isEmpty) {
      continue;
    }
    counts[runID] = (counts[runID] ?? 0) + 1;
  }
  return counts.entries
      .where((entry) => entry.value > 1)
      .map((entry) => entry.key)
      .toSet();
}

ChatRole _roleFromMessage(String role) {
  return switch (role) {
    'user' => ChatRole.user,
    'assistant' => ChatRole.assistant,
    _ => ChatRole.system,
  };
}

String _reasoningFromMessage(Message message) {
  final parts = message.contentParts
      .where((part) => part.kind == 'reasoning')
      .map((part) => part.reasoning?.trim() ?? '')
      .where((reasoning) => reasoning.isNotEmpty)
      .toList(growable: false);
  return parts.join('\n\n');
}

bool isTerminalRunEvent(RunEvent event) {
  return event.type == 'run.completed' ||
      event.type == 'run.failed' ||
      event.type == 'run.interrupted';
}

String? assistantDeltaText(RunEvent event) {
  if (event.type != 'assistant.delta') {
    return null;
  }
  final delta = event.data['assistant_delta'];
  if (delta is! Map) {
    throw const FormatException('assistant.delta missing assistant_delta.');
  }
  final text = delta['delta'];
  if (text == null) {
    return null;
  }
  if (text is! String) {
    throw const FormatException('assistant.delta delta must be text.');
  }
  return text.isEmpty ? null : text;
}

String? assistantDeltaReasoning(RunEvent event) {
  if (event.type != 'assistant.delta') {
    return null;
  }
  final delta = event.data['assistant_delta'];
  if (delta is! Map) {
    throw const FormatException('assistant.delta missing assistant_delta.');
  }
  final reasoning = delta['reasoning'];
  if (reasoning == null) {
    return null;
  }
  if (reasoning is! String) {
    throw const FormatException('assistant.delta reasoning must be text.');
  }
  return reasoning.trim().isEmpty ? null : reasoning;
}

String? assistantDeltaMessageId(RunEvent event) {
  if (event.type != 'assistant.delta') {
    return null;
  }
  final delta = event.data['assistant_delta'];
  if (delta is! Map) {
    throw const FormatException('assistant.delta missing assistant_delta.');
  }
  return _nonEmptyString(delta['message_id']);
}

String? agentMessageText(RunEvent event) {
  if (event.type != 'agent.message' && event.type != 'run.completed') {
    return null;
  }
  final message = event.data['message'];
  if (message is! Map) {
    return null;
  }
  final content = message['content'];
  return content is String && content.isNotEmpty ? content : null;
}

String? agentMessageReasoning(RunEvent event) {
  if (event.type != 'agent.message' && event.type != 'run.completed') {
    return null;
  }
  final message = event.data['message'];
  if (message is! Map) {
    return null;
  }
  final reasoning = message['reasoning'];
  if (reasoning == null) {
    return null;
  }
  if (reasoning is! String) {
    throw const FormatException('agent message reasoning must be text.');
  }
  final trimmed = reasoning.trim();
  return trimmed.isEmpty ? null : trimmed;
}

String? agentMessageId(RunEvent event) {
  if (event.type != 'agent.message' && event.type != 'run.completed') {
    return null;
  }
  final message = event.data['message'];
  if (message is! Map) {
    return null;
  }
  return _nonEmptyString(message['message_id']) ??
      _nonEmptyString(_record(message['meta'])?['message_id']);
}

ChatRunStatus statusFromTerminalEvent(RunEvent event) {
  return switch (event.type) {
    'run.completed' => ChatRunStatus.completed,
    'run.failed' => ChatRunStatus.failed,
    'run.interrupted' => ChatRunStatus.interrupted,
    _ => ChatRunStatus.streaming,
  };
}

ChatItem? activityFromEvent(RunEvent event) {
  if (event.type == 'assistant.delta') {
    return null;
  }
  if (!_shouldShowActivityInChat(event.type)) {
    return null;
  }
  final label = _activityLabel(event);
  final detail = _activityDetail(event);
  return ChatItem.activity(
    id: 'activity:${event.eventId}',
    runId: event.runId,
    eventType: event.type,
    text: label,
    detail: detail,
    createdAt: event.ts,
  );
}

String runEventLabel(RunEvent event) {
  return _activityLabel(event);
}

String? runEventDetail(RunEvent event) {
  return _activityDetail(event);
}

bool _shouldShowActivityInChat(String eventType) {
  return switch (eventType) {
    'run.failed' ||
    'run.interrupted' ||
    'run.resume_requested' ||
    'elicitation.pending' ||
    'operator_question.pending' ||
    'decision_blocked' => true,
    _ => false,
  };
}

String _activityLabel(RunEvent event) {
  return switch (event.type) {
    'run.started' => 'Run started',
    'run.failed' => 'Run failed',
    'run.interrupted' => 'Run interrupted',
    'run.resume_requested' => 'Resume requested',
    'elicitation.pending' => 'Input requested',
    'elicitation.decided' => 'Input answered',
    'operator_question.pending' => 'Question pending',
    'operator_question.decided' => 'Question answered',
    'decision_blocked' => 'Decision blocked',
    _ => event.type,
  };
}

String? _activityDetail(RunEvent event) {
  final data = event.data;

  final error = data['error'];
  if (error is String && error.trim().isNotEmpty) {
    return error.trim();
  }

  final message = data['message'];
  if (message is String && message.trim().isNotEmpty) {
    return message.trim();
  }

  final question = data['question'];
  if (question is String && question.trim().isNotEmpty) {
    return question.trim();
  }

  final answer = data['answer'];
  if (answer is String && answer.trim().isNotEmpty) {
    return answer.trim();
  }

  final reason = data['reason'];
  if (reason is String && reason.trim().isNotEmpty) {
    return reason.trim();
  }

  final decisionReason = data['decision_reason'];
  if (decisionReason is String && decisionReason.trim().isNotEmpty) {
    return decisionReason.trim();
  }

  return null;
}

Map<Object?, Object?>? _record(Object? value) {
  if (value is Map) {
    return value;
  }
  return null;
}

String? _nonEmptyString(Object? value) {
  if (value is! String) {
    return null;
  }
  final trimmed = value.trim();
  return trimmed.isEmpty ? null : trimmed;
}
