import '../../api/acorn_api.dart';

String threadDisplayTitle(Thread? thread) {
  final title = thread?.title.trim();
  if (title == null || title.isEmpty) {
    return 'New thread';
  }
  return title;
}
