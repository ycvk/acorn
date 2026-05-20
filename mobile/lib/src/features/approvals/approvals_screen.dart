import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../api/acorn_api.dart';
import '../../core/providers.dart';
import '../../ui/theme/acorn_theme.dart';
import '../../ui/widgets/acorn_formatters.dart';
import '../../ui/widgets/acorn_surfaces.dart';
import '../../ui/widgets/empty_state.dart';
import '../../ui/widgets/list_rows.dart';
import '../../ui/widgets/section_header.dart';

class ApprovalsScreen extends ConsumerWidget {
  const ApprovalsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final controller = ref.watch(acornControllerProvider);
    final pending = controller.inbox?.pendingActions ?? const [];

    return Scaffold(
      appBar: AppBar(
        title: const Text('Approvals'),
        actions: [
          IconButton.filledTonal(
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh),
            onPressed: controller.busy ? null : controller.refreshAll,
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: controller.refreshAll,
        child: pending.isEmpty
            ? ListView(
                children: [
                  SizedBox(height: MediaQuery.of(context).size.height * 0.18),
                  const AcornEmptyState(
                    icon: Icons.rule_folder_outlined,
                    title: 'No pending approvals',
                    body:
                        'Actions that need a device decision will appear here.',
                  ),
                ],
              )
            : ListView(
                children: [
                  AcornPageIntro(
                    icon: Icons.rule_folder_outlined,
                    title: '${pending.length} pending',
                    body: 'Review device-gated actions before they continue.',
                    tone: AcornStatusTone.warning,
                  ),
                  const SectionHeader(title: 'Waiting for decision'),
                  for (final action in pending)
                    AcornListRow(
                      icon: Icons.rule_folder_outlined,
                      title: action.title,
                      subtitle:
                          '${action.kind} · ${formatTimestamp(action.createdAt)} · ${shortId(action.runId)}',
                      tone: AcornStatusTone.warning,
                      trailing: const Icon(Icons.chevron_right),
                      onTap: () => _openApproval(context, ref, action),
                    ),
                ],
              ),
      ),
    );
  }

  Future<void> _openApproval(
    BuildContext context,
    WidgetRef ref,
    PendingActionSummary action,
  ) async {
    final controller = ref.read(acornControllerProvider);
    await controller.loadPendingAction(action.actionId);
    if (!context.mounted) {
      return;
    }
    final detail = controller.pendingActionDetail;
    if (detail == null) {
      return;
    }
    await showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      isScrollControlled: true,
      useSafeArea: true,
      builder: (sheetContext) => _ApprovalSheet(detail: detail),
    );
  }
}

class _ApprovalSheet extends ConsumerStatefulWidget {
  const _ApprovalSheet({required this.detail});

  final PendingActionDetail detail;

  @override
  ConsumerState<_ApprovalSheet> createState() => _ApprovalSheetState();
}

class _ApprovalSheetState extends ConsumerState<_ApprovalSheet> {
  final TextEditingController _answerController = TextEditingController();

  @override
  void dispose() {
    _answerController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final controller = ref.watch(acornControllerProvider);
    final detail = widget.detail;
    final isOperatorQuestion = detail.kind == 'operator_question';
    final allowFreeform = detail.payload['allow_freeform'] == true;
    final answerText = _answerController.text.trim();
    return SafeArea(
      child: SingleChildScrollView(
        padding: EdgeInsets.only(
          left: 20,
          right: 20,
          bottom: MediaQuery.of(context).viewInsets.bottom + 20,
        ),
        child: SafeArea(
          top: false,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text(
                detail.title,
                style: Theme.of(
                  context,
                ).textTheme.titleLarge?.copyWith(fontWeight: FontWeight.w800),
              ),
              const SizedBox(height: 8),
              Text(detail.body ?? detail.kind),
              if (detail.reason != null) ...[
                const SizedBox(height: 10),
                Text(
                  detail.reason!,
                  style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
              const SizedBox(height: 18),
              for (final option in detail.options) ...[
                FilledButton.tonal(
                  onPressed: controller.busy
                      ? null
                      : () async {
                          await controller.decidePendingAction(
                            detail.actionId,
                            decision: isOperatorQuestion ? 'answer' : option.id,
                            selectedOptionId: isOperatorQuestion
                                ? option.id
                                : null,
                          );
                          if (context.mounted) {
                            Navigator.of(context).pop();
                          }
                        },
                  child: Text(option.label),
                ),
                const SizedBox(height: 8),
              ],
              if (allowFreeform) ...[
                const SizedBox(height: 6),
                TextField(
                  controller: _answerController,
                  minLines: 2,
                  maxLines: 5,
                  textInputAction: TextInputAction.newline,
                  decoration: const InputDecoration(labelText: 'Answer'),
                  onChanged: (_) => setState(() {}),
                ),
                const SizedBox(height: 12),
                FilledButton(
                  onPressed: controller.busy || answerText.isEmpty
                      ? null
                      : () async {
                          await controller.decidePendingAction(
                            detail.actionId,
                            decision: 'answer',
                            answer: answerText,
                          );
                          if (context.mounted) {
                            Navigator.of(context).pop();
                          }
                        },
                  child: const Text('Submit answer'),
                ),
              ],
              if (isOperatorQuestion) ...[
                const SizedBox(height: 8),
                TextButton(
                  onPressed: controller.busy
                      ? null
                      : () async {
                          await controller.decidePendingAction(
                            detail.actionId,
                            decision: 'decline',
                          );
                          if (context.mounted) {
                            Navigator.of(context).pop();
                          }
                        },
                  child: const Text('Decline'),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}
