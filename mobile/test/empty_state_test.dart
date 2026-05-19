import 'package:acorn_mobile/src/ui/widgets/empty_state.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('empty state scrolls instead of overflowing in short viewport', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: ThemeData(useMaterial3: true),
        home: Scaffold(
          body: Center(
            child: SizedBox(
              width: 360,
              height: 180,
              child: AcornEmptyState(
                icon: Icons.chat_bubble_outline,
                title: 'Start with a real backend run',
                body:
                    'Messages are stored in Acorn. Assistant output streams from persisted RunEvents.',
                action: FilledButton.icon(
                  onPressed: () {},
                  icon: const Icon(Icons.add),
                  label: const Text('New thread'),
                ),
              ),
            ),
          ),
        ),
      ),
    );

    expect(tester.takeException(), isNull);
    expect(find.text('Start with a real backend run'), findsOneWidget);

    await tester.drag(find.byType(SingleChildScrollView), const Offset(0, -80));
    await tester.pump();

    expect(tester.takeException(), isNull);
  });
}
