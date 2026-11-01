import Cocoa
import FlutterMacOS

class MainFlutterWindow: NSWindow {
  override func awakeFromNib() {
    let flutterViewController = FlutterViewController()
    let windowFrame = self.frame
    self.contentViewController = flutterViewController
    self.setFrame(windowFrame, display: true)

    RegisterGeneratedPlugins(registry: flutterViewController)

    // meept:// deep-link bridge (hitl-notifications leaf 03, T4): the
    // Info.plist CFBundleURLTypes entry routes `open meept://session/<id>`
    // here; DeepLinkChannel forwards it into the Dart listener.
    _ = DeepLinkChannel(messenger: flutterViewController.engine.binaryMessenger)

    super.awakeFromNib()
  }
}
