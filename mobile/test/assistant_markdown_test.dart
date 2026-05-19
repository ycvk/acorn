import 'package:acorn_mobile/src/features/chat/assistant_markdown.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_markdown_plus/flutter_markdown_plus.dart';

void main() {
  testWidgets('renders assistant markdown body content', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AssistantMarkdown(
            text:
                'Hello **Acorn**\n\n- one\n- two\n\n```go\nfmt.Println("ok")\n```',
            textColor: const Color(0xFF171819),
          ),
        ),
      ),
    );

    expect(find.textContaining('Hello'), findsOneWidget);
    expect(find.textContaining('Acorn'), findsOneWidget);
    expect(find.textContaining('one'), findsOneWidget);
    expect(find.textContaining('two'), findsOneWidget);
    expect(find.textContaining('fmt.Println'), findsOneWidget);
  });

  testWidgets('copies code block content', (tester) async {
    final binding = TestWidgetsFlutterBinding.ensureInitialized();
    final calls = <MethodCall>[];
    binding.defaultBinaryMessenger.setMockMethodCallHandler(
      SystemChannels.platform,
      (call) async {
        calls.add(call);
        return null;
      },
    );
    addTearDown(() {
      binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        null,
      );
    });

    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: AssistantMarkdown(
            text: '```go\nfmt.Println("ok")\n```',
            textColor: Color(0xFF171819),
          ),
        ),
      ),
    );

    await tester.tap(find.byTooltip('Copy code'));
    await tester.pump();

    expect(
      calls,
      contains(
        isA<MethodCall>()
            .having((call) => call.method, 'method', 'Clipboard.setData')
            .having((call) => call.arguments, 'arguments', {
              'text': 'fmt.Println("ok")',
            }),
      ),
    );
    expect(find.text('Copied code'), findsOneWidget);
  });

  testWidgets('uses a bounded scrollable markdown viewport for long messages', (
    tester,
  ) async {
    final longText = List.filled(
      180,
      'Acorn keeps the assistant message readable while the parent chat list remains stable.',
    ).join('\n\n');

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: AssistantMarkdown(
            text: longText,
            textColor: const Color(0xFF171819),
          ),
        ),
      ),
    );

    expect(find.byKey(assistantMarkdownLongViewportKey), findsOneWidget);
    expect(find.byType(Markdown), findsOneWidget);
  });
}
