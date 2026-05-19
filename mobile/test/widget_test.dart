import 'package:acorn_mobile/app.dart';
import 'package:acorn_mobile/src/core/connection_store.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('shows pairing screen when no device profile is stored', (
    tester,
  ) async {
    await tester.pumpWidget(AcornApp(connectionStore: MemoryConnectionStore()));
    await tester.pumpAndSettle();

    expect(find.text('Acorn'), findsOneWidget);
    expect(find.text('Server URL'), findsOneWidget);
    expect(find.text('Pairing code'), findsOneWidget);
    expect(find.text('Scan pairing QR'), findsOneWidget);
    expect(find.text('Pair device'), findsOneWidget);
  });
}
