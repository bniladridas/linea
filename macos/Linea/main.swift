import Cocoa
import WebKit

final class AppDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate, WKNavigationDelegate {
  private var process: Process?
  private var window: NSWindow?
  private var webView: WKWebView?
  private var baseURL: URL?
  private var logHandle: FileHandle?

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
    config.userContentController.addUserScript(
      WKUserScript(
        source: "document.documentElement.classList.add('linea-macos-shell')",
        injectionTime: .atDocumentStart,
        forMainFrameOnly: true
      )
    )
    let webView = WKWebView(frame: .zero, configuration: config)
    webView.navigationDelegate = self
    webView.allowsBackForwardNavigationGestures = false
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
    window.contentView = webView
    window.center()
    window.makeKeyAndOrderFront(nil)
    self.webView = webView
    self.window = window
    NSApp.activate(ignoringOtherApps: true)
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

    let viewItem = NSMenuItem(title: "View", action: nil, keyEquivalent: "")
    menu.addItem(viewItem)
    let viewMenu = NSMenu(title: "View")
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
