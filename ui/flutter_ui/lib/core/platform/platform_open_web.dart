// Web half of the external-URL opener (deep links, leaf 03).
//
// Uses the browser's window.open via dart:js_interop (dart:html is
// deprecated). Web builds never import the native half.
import 'package:web/web.dart' as web;

/// Open [url] in a new browser tab. window.open returning null (popup
/// blocked) degrades to false; the deep-link path never throws.
Future<bool> openExternalWeb(String url) async {
  final web.Window? win = web.window.open(url, '_blank');
  return win != null;
}
