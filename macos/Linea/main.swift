import Cocoa
import WebKit

final class WindowDragRegionView: NSView {
  override var mouseDownCanMoveWindow: Bool {
    true
  }

  override func acceptsFirstMouse(for event: NSEvent?) -> Bool {
    true
  }

  override func mouseDown(with event: NSEvent) {
    window?.performDrag(with: event)
  }
}

final class AppDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate, WKNavigationDelegate, WKUIDelegate, WKScriptMessageHandler {
  private var process: Process?
  private var window: NSWindow?
  private var webView: WKWebView?
  private var baseURL: URL?
  private var logHandle: FileHandle?

  func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage) {
    guard message.name == "smoke",
          let body = message.body as? [String: Any],
          let type = body["type"] as? String,
          let msg = body["message"] as? String else {
      return
    }
    print("[WKWebView Smoke] \(type.uppercased()): \(msg)")
  }

  func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
    let isSmoke = ProcessInfo.processInfo.environment["LINEA_MACOS_UI_SMOKE"] == "1"
    if isSmoke {
      DispatchQueue.main.asyncAfter(deadline: .now() + 2.0) {
        webView.evaluateJavaScript("document.body.innerText") { (result, error) in
          if let error = error {
            print("[WKWebView Smoke] FAIL: evaluateJavaScript error: \(error.localizedDescription)")
            return
          }
          let text = (result as? String) ?? ""
          if text.contains("Linea") {
            print("[WKWebView Smoke] PASS: UI rendered successfully inside WKWebView.")
          } else {
            print("[WKWebView Smoke] FAIL: Expected 'Linea' text not found in WKWebView.")
          }
        }
      }
    }
  }

  func applicationDidFinishLaunching(_ notification: Notification) {
    NSApp.setActivationPolicy(.regular)
    NSWindow.allowsAutomaticWindowTabbing = false
    installMenu()

    do {
      let server = try startServer()
      process = server.process
      baseURL = server.url

      DispatchQueue.global(qos: .userInitiated).async {
        let ready = self.waitForServer(server.url)
        DispatchQueue.main.async {
          if ready {
            self.openWindow(server.url)
          } else {
            self.showStartupError(server.url)
          }
        }
      }
    } catch {
      showError(error.localizedDescription)
    }
  }

  func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
    true
  }

  func applicationWillTerminate(_ notification: Notification) {
    process?.terminate()
    DispatchQueue.global().asyncAfter(deadline: .now() + 0.3) {
      if self.process?.isRunning == true {
        self.process?.interrupt()
      }
    }
    try? logHandle?.close()
  }

  func webView(
    _ webView: WKWebView,
    decidePolicyFor navigationAction: WKNavigationAction,
    decisionHandler: @escaping (WKNavigationActionPolicy) -> Void
  ) {
    guard let url = navigationAction.request.url else {
      decisionHandler(.cancel)
      return
    }

    if url.scheme == baseURL?.scheme, url.host == baseURL?.host, url.port == baseURL?.port {
      decisionHandler(.allow)
      return
    }

    NSWorkspace.shared.open(url)
    decisionHandler(.cancel)
  }

  func webView(
    _ webView: WKWebView,
    createWebViewWith configuration: WKWebViewConfiguration,
    for navigationAction: WKNavigationAction,
    windowFeatures: WKWindowFeatures
  ) -> WKWebView? {
    guard navigationAction.targetFrame == nil, let url = navigationAction.request.url else {
      return nil
    }

    if url.scheme == baseURL?.scheme, url.host == baseURL?.host, url.port == baseURL?.port {
      webView.load(URLRequest(url: url))
    } else {
      NSWorkspace.shared.open(url)
    }
    return nil
  }

  private func startServer() throws -> (process: Process, url: URL) {
    guard let serverPath = Bundle.main.path(forResource: "linea", ofType: nil) else {
      throw LineaError.message("Bundled server was not found.")
    }

    let apiAddr = ProcessInfo.processInfo.environment["API_ADDR"] ?? "127.0.0.1:18080"
    guard let url = URL(string: urlString(for: apiAddr)) else {
      throw LineaError.message("API_ADDR is invalid: \(apiAddr)")
    }

    let log = try openLog()
    logHandle = log

    let process = Process()
    process.executableURL = URL(fileURLWithPath: serverPath)
    process.environment = ProcessInfo.processInfo.environment.merging(["API_ADDR": apiAddr]) { _, new in new }
    process.standardOutput = log
    process.standardError = log
    try process.run()

    return (process, url)
  }

  private func openWindow(_ url: URL) {
    let config = WKWebViewConfiguration()
    config.preferences.setValue(true, forKey: "developerExtrasEnabled")

    let isSmoke = ProcessInfo.processInfo.environment["LINEA_MACOS_UI_SMOKE"] == "1"
    if isSmoke {
      config.userContentController.add(self, name: "smoke")
      let errorCaptureScript = WKUserScript(
        source: """
        window.addEventListener('error', (event) => {
          window.webkit.messageHandlers.smoke.postMessage({ type: 'error', message: event.message || String(event) });
        });
        window.addEventListener('unhandledrejection', (event) => {
          window.webkit.messageHandlers.smoke.postMessage({ type: 'error', message: 'Unhandled promise rejection: ' + String(event.reason) });
        });
        """,
        injectionTime: .atDocumentStart,
        forMainFrameOnly: true
      )
      config.userContentController.addUserScript(errorCaptureScript)
    }

    config.userContentController.addUserScript(
      WKUserScript(
        source: "document.documentElement.classList.add('linea-macos-shell')",
        injectionTime: .atDocumentStart,
        forMainFrameOnly: true
      )
    )
    config.userContentController.addUserScript(
      WKUserScript(
        source: previewReturnScript(homeURL: url.appendingPathComponent("").absoluteString),
        injectionTime: .atDocumentEnd,
        forMainFrameOnly: true
      )
    )
    let webView = WKWebView(frame: .zero, configuration: config)
    if #available(macOS 13.3, iOS 16.4, *) {
      webView.isInspectable = true
    }
    webView.navigationDelegate = self
    webView.uiDelegate = self
    webView.allowsBackForwardNavigationGestures = true
    webView.wantsLayer = true
    webView.layer?.backgroundColor = NSColor.windowBackgroundColor.cgColor
    webView.load(URLRequest(url: url.appendingPathComponent("")))

    let window = NSWindow(
      contentRect: NSRect(x: 0, y: 0, width: 1180, height: 820),
      styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
      backing: .buffered,
      defer: false
    )
    window.title = "Linea"
    window.titleVisibility = .hidden
    window.minSize = NSSize(width: 780, height: 560)
    window.delegate = self
    window.isReleasedWhenClosed = false
    window.setFrameAutosaveName("main-window")
    window.titlebarAppearsTransparent = true
    let contentView = NSView()
    contentView.addSubview(webView)
    webView.translatesAutoresizingMaskIntoConstraints = false
    NSLayoutConstraint.activate([
      webView.leadingAnchor.constraint(equalTo: contentView.leadingAnchor),
      webView.trailingAnchor.constraint(equalTo: contentView.trailingAnchor),
      webView.topAnchor.constraint(equalTo: contentView.topAnchor),
      webView.bottomAnchor.constraint(equalTo: contentView.bottomAnchor),
    ])

    let dragRegion = WindowDragRegionView()
    dragRegion.translatesAutoresizingMaskIntoConstraints = false
    contentView.addSubview(dragRegion)
    NSLayoutConstraint.activate([
      dragRegion.leadingAnchor.constraint(equalTo: contentView.leadingAnchor),
      dragRegion.trailingAnchor.constraint(equalTo: contentView.trailingAnchor),
      dragRegion.topAnchor.constraint(equalTo: contentView.topAnchor),
      dragRegion.heightAnchor.constraint(equalToConstant: 8),
    ])

    window.contentView = contentView
    window.center()
    window.makeKeyAndOrderFront(nil)
    self.webView = webView
    self.window = window
    NSApp.activate(ignoringOtherApps: true)
  }

  private func previewReturnScript(homeURL: String) -> String {
    let home = javascriptStringLiteral(homeURL)
    return """
    (() => {
      if (!location.pathname.startsWith('/api/agent/previews/')) return;
      if (document.getElementById('linea-preview-back')) return;

      const style = document.createElement('style');
      style.textContent = `
        #linea-preview-back {
          position: fixed;
          top: 12px;
          right: 12px;
          z-index: 2147483647;
          height: 28px;
          padding: 0 10px;
          border: 1px solid rgba(0, 0, 0, 0.12);
          border-radius: 999px;
          background: rgba(248, 248, 245, 0.82);
          color: #252522;
          box-shadow: 0 4px 18px rgba(0, 0, 0, 0.08);
          font: 13px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
          opacity: 0.72;
          cursor: pointer;
          -webkit-backdrop-filter: saturate(1.2) blur(14px);
          backdrop-filter: saturate(1.2) blur(14px);
        }
        #linea-preview-back:hover,
        #linea-preview-back:focus-visible {
          opacity: 1;
        }
        @media (prefers-color-scheme: dark) {
          #linea-preview-back {
            border-color: rgba(255, 255, 255, 0.14);
            background: rgba(20, 20, 20, 0.78);
            color: #eeeeee;
            box-shadow: 0 4px 18px rgba(0, 0, 0, 0.28);
          }
        }
      `;

      const button = document.createElement('button');
      button.id = 'linea-preview-back';
      button.type = 'button';
      button.textContent = 'Back';
      button.setAttribute('aria-label', 'Back to Linea');
      button.addEventListener('click', () => {
        if (history.length > 1) {
          history.back();
        } else {
          location.href = \(home);
        }
      });

      document.head.appendChild(style);
      document.body.appendChild(button);
    })();
    """
  }

  private func javascriptStringLiteral(_ value: String) -> String {
    guard
      let data = try? JSONSerialization.data(withJSONObject: [value], options: []),
      let encoded = String(data: data, encoding: .utf8)
    else {
      return "\"\""
    }
    return String(encoded.dropFirst().dropLast())
  }

  func webView(
    _ webView: WKWebView,
    runOpenPanelWith parameters: WKOpenPanelParameters,
    initiatedByFrame frame: WKFrameInfo,
    completionHandler: @escaping ([URL]?) -> Void
  ) {
    let panel = NSOpenPanel()
    panel.canChooseDirectories = false
    panel.canChooseFiles = true
    panel.allowsMultipleSelection = parameters.allowsMultipleSelection

    if let window {
      panel.beginSheetModal(for: window) { response in
        completionHandler(response == .OK ? panel.urls : nil)
      }
    } else {
      let response = panel.runModal()
      completionHandler(response == .OK ? panel.urls : nil)
    }
  }

  private func waitForServer(_ url: URL) -> Bool {
    let healthURL = url.appendingPathComponent("healthz")
    let deadline = Date().addingTimeInterval(20)

    while Date() < deadline {
      let semaphore = DispatchSemaphore(value: 0)
      var healthy = false

      URLSession.shared.dataTask(with: healthURL) { _, response, _ in
        if let http = response as? HTTPURLResponse, http.statusCode == 200 {
          healthy = true
        }
        semaphore.signal()
      }.resume()

      _ = semaphore.wait(timeout: .now() + 1)
      if healthy {
        return true
      }
      Thread.sleep(forTimeInterval: 0.25)
    }

    return false
  }

  private func showStartupError(_ url: URL) {
    showError("Linea did not start at \(url.absoluteString).")
  }

  private func showError(_ message: String) {
    let alert = NSAlert()
    alert.messageText = "Linea could not start"
    alert.informativeText = message
    alert.addButton(withTitle: "Quit")
    alert.runModal()
    NSApp.terminate(nil)
  }

  private func installMenu() {
    let menu = NSMenu()
    let appItem = NSMenuItem()
    menu.addItem(appItem)

    let appMenu = NSMenu()
    appMenu.addItem(
      NSMenuItem(
        title: "About Linea",
        action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)),
        keyEquivalent: ""
      )
    )
    appMenu.addItem(.separator())
    appMenu.addItem(
      NSMenuItem(
        title: "Hide Linea",
        action: #selector(NSApplication.hide(_:)),
        keyEquivalent: "h"
      )
    )
    let hideOthers = NSMenuItem(
      title: "Hide Others",
      action: #selector(NSApplication.hideOtherApplications(_:)),
      keyEquivalent: "h"
    )
    hideOthers.keyEquivalentModifierMask = [.command, .option]
    appMenu.addItem(hideOthers)
    appMenu.addItem(
      NSMenuItem(
        title: "Show All",
        action: #selector(NSApplication.unhideAllApplications(_:)),
        keyEquivalent: ""
      )
    )
    appMenu.addItem(.separator())
    appMenu.addItem(
      NSMenuItem(
        title: "Quit Linea",
        action: #selector(NSApplication.terminate(_:)),
        keyEquivalent: "q"
      )
    )
    appItem.submenu = appMenu

    let editItem = NSMenuItem(title: "Edit", action: nil, keyEquivalent: "")
    menu.addItem(editItem)
    let editMenu = NSMenu(title: "Edit")
    editMenu.addItem(
      NSMenuItem(
        title: "Undo",
        action: Selector(("undo:")),
        keyEquivalent: "z"
      )
    )
    editMenu.addItem(
      NSMenuItem(
        title: "Redo",
        action: Selector(("redo:")),
        keyEquivalent: "Z"
      )
    )
    editMenu.addItem(.separator())
    editMenu.addItem(
      NSMenuItem(
        title: "Cut",
        action: #selector(NSText.cut(_:)),
        keyEquivalent: "x"
      )
    )
    editMenu.addItem(
      NSMenuItem(
        title: "Copy",
        action: #selector(NSText.copy(_:)),
        keyEquivalent: "c"
      )
    )
    editMenu.addItem(
      NSMenuItem(
        title: "Paste",
        action: #selector(NSText.paste(_:)),
        keyEquivalent: "v"
      )
    )
    editMenu.addItem(
      NSMenuItem(
        title: "Select All",
        action: #selector(NSText.selectAll(_:)),
        keyEquivalent: "a"
      )
    )
    editItem.submenu = editMenu

    let viewItem = NSMenuItem(title: "View", action: nil, keyEquivalent: "")
    menu.addItem(viewItem)
    let viewMenu = NSMenu(title: "View")
    viewMenu.addItem(
      NSMenuItem(
        title: "Back",
        action: #selector(goBack(_:)),
        keyEquivalent: "["
      )
    )
    viewMenu.addItem(
      NSMenuItem(
        title: "Forward",
        action: #selector(goForward(_:)),
        keyEquivalent: "]"
      )
    )
    viewMenu.addItem(.separator())
    viewMenu.addItem(
      NSMenuItem(
        title: "Reload",
        action: #selector(reloadPage(_:)),
        keyEquivalent: "r"
      )
    )
    viewItem.submenu = viewMenu

    let windowItem = NSMenuItem(title: "Window", action: nil, keyEquivalent: "")
    menu.addItem(windowItem)
    let windowMenu = NSMenu(title: "Window")
    windowMenu.addItem(
      NSMenuItem(
        title: "Minimize",
        action: #selector(NSWindow.miniaturize(_:)),
        keyEquivalent: "m"
      )
    )
    windowMenu.addItem(
      NSMenuItem(
        title: "Zoom",
        action: #selector(NSWindow.zoom(_:)),
        keyEquivalent: ""
      )
    )
    windowMenu.addItem(.separator())
    windowMenu.addItem(
      NSMenuItem(
        title: "Bring All to Front",
        action: #selector(NSApplication.arrangeInFront(_:)),
        keyEquivalent: ""
      )
    )
    windowItem.submenu = windowMenu
    NSApp.windowsMenu = windowMenu
    NSApp.mainMenu = menu
  }

  @objc private func reloadPage(_ sender: Any?) {
    webView?.reload()
  }

  @objc private func goBack(_ sender: Any?) {
    guard let webView, webView.canGoBack else {
      return
    }
    webView.goBack()
  }

  @objc private func goForward(_ sender: Any?) {
    guard let webView, webView.canGoForward else {
      return
    }
    webView.goForward()
  }

  private func openLog() throws -> FileHandle {
    let logs = FileManager.default.homeDirectoryForCurrentUser
      .appendingPathComponent("Library")
      .appendingPathComponent("Logs")
      .appendingPathComponent("Linea")
    try FileManager.default.createDirectory(at: logs, withIntermediateDirectories: true)

    let path = logs.appendingPathComponent("linea-macos.log")
    if !FileManager.default.fileExists(atPath: path.path) {
      FileManager.default.createFile(atPath: path.path, contents: nil)
    }
    let handle = try FileHandle(forWritingTo: path)
    try handle.seekToEnd()
    return handle
  }

  private func urlString(for addr: String) -> String {
    if addr.hasPrefix(":") {
      return "http://127.0.0.1\(addr)"
    }
    if addr.contains(":") {
      return "http://\(addr)"
    }
    return "http://127.0.0.1:\(addr)"
  }
}

enum LineaError: LocalizedError {
  case message(String)

  var errorDescription: String? {
    switch self {
    case .message(let message):
      return message
    }
  }
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.run()
