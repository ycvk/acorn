import 'package:acorn_mobile/src/ui/widgets/message_widgets.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('thinking section is collapsed by default and expands on tap', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: ThemeData(useMaterial3: true),
        home: const Scaffold(
          body: Center(
            child: AcornThinkingSection(
              reasoning: 'Inspected the retrieved evidence.',
            ),
          ),
        ),
      ),
    );

    expect(find.text('Thinking'), findsOneWidget);
    expect(find.text('Inspected the retrieved evidence.'), findsNothing);

    await tester.tap(find.text('Thinking'));
    await tester.pumpAndSettle();

    expect(find.text('Inspected the retrieved evidence.'), findsOneWidget);
  });
}
