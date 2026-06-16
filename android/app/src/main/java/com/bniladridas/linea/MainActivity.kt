package com.bniladridas.linea

import android.annotation.SuppressLint
import android.content.Context
import android.os.Bundle
import android.view.KeyEvent
import android.view.ViewGroup
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.FrameLayout
import androidx.appcompat.app.AppCompatActivity
import java.io.File
import java.net.HttpURLConnection
import java.net.URL

/**
 * Linea Android app.
 *
 * Bundles the Go server binary as an asset (assets/linea-android-arm64).
 * On launch, extracts it to internal storage, spawns it as a subprocess,
 * waits for it to become healthy, then loads the React UI in a WebView.
 *
 * Build with `make android-check` or manually:
 *   cd android && GOOS=android GOARCH=arm64 CGO_ENABLED=0 \
 *     go build -o app/src/main/assets/linea-android-arm64 ../backend/cmd/server
 */
class MainActivity : AppCompatActivity() {

    private lateinit var webView: WebView
    private var serverProcess: java.lang.Process? = null

    private val serverPort = 8080
    private val serverUrl get() = "http://127.0.0.1:$serverPort"

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        webView = WebView(this).apply {
            settings.javaScriptEnabled = true
            settings.domStorageEnabled = true
            settings.allowFileAccess = false
            webViewClient = object : WebViewClient() {
                override fun shouldOverrideUrlLoading(
                    view: WebView,
                    request: WebResourceRequest
                ): Boolean {
                    val host = request.url.host ?: return true
                    return host != "127.0.0.1"
                }
                override fun onPageFinished(view: WebView, url: String) {
                    view.evaluateJavascript(
                        "document.documentElement.classList.add('linea-android-shell')",
                        null
                    )
                }
            }
        }
        val root = FrameLayout(this).apply {
            fitsSystemWindows = true
            addView(webView, ViewGroup.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.MATCH_PARENT
            ))
        }
        setContentView(root)

        Thread {
            val binary = extractBinary()
            if (binary != null) {
                startServer(binary)
            }
            val ready = waitForServer()
            runOnUiThread {
                if (ready) {
                    if (savedInstanceState != null) webView.restoreState(savedInstanceState)
                    else webView.loadUrl(serverUrl)
                } else {
                    webView.loadData(
                        "<h2 style='font-family:sans-serif;padding:24px'>Linea could not start.</h2>",
                        "text/html", "utf-8"
                    )
                }
            }
        }.start()
    }

    override fun onSaveInstanceState(outState: Bundle) {
        super.onSaveInstanceState(outState)
        webView.saveState(outState)
    }

    override fun onKeyDown(keyCode: Int, event: KeyEvent?): Boolean {
        if (keyCode == KeyEvent.KEYCODE_BACK && webView.canGoBack()) {
            webView.goBack()
            return true
        }
        return super.onKeyDown(keyCode, event)
    }

    override fun onDestroy() {
        serverProcess?.destroy()
        webView.destroy()
        super.onDestroy()
    }

    // ── private helpers ────────────────────────────────────────────────────

    /** Extract the bundled Go binary from assets to internal storage. */
    private fun extractBinary(): File? {
        val assetName = "linea-android-arm64"
        val binary = File(filesDir, assetName)
        if (binary.exists()) {
            return binary
        }
        return try {
            assets.open(assetName).use { input ->
                binary.outputStream().use { output ->
                    input.copyTo(output)
                }
            }
            binary.setExecutable(true)
            binary
        } catch (e: Exception) {
            android.util.Log.e("Linea", "Failed to extract binary", e)
            null
        }
    }

    /** Start the bundled Linea server as a subprocess. */
    private fun startServer(binary: File) {
        try {
            val pb = java.lang.ProcessBuilder(
                binary.absolutePath,
                "server",
                "--addr", "127.0.0.1:$serverPort"
            )
            pb.directory(filesDir)
            pb.environment()["LINEA_ENV_FILE"] = "/dev/null"
            pb.environment()["LINEA_AGENT_DEVELOPER_MODE"] = "0"
            serverProcess = pb.start()
        } catch (e: Exception) {
            android.util.Log.e("Linea", "Failed to start server", e)
        }
    }

    /** Poll /healthz for up to 20 seconds, matching the macOS wrapper. */
    private fun waitForServer(): Boolean {
        val deadline = System.currentTimeMillis() + 20_000
        while (System.currentTimeMillis() < deadline) {
            try {
                val conn = URL("$serverUrl/healthz").openConnection() as HttpURLConnection
                conn.connectTimeout = 500
                conn.readTimeout = 500
                try {
                    if (conn.responseCode == 200) return true
                } finally {
                    conn.disconnect()
                }
            } catch (_: Exception) {}
            Thread.sleep(250)
        }
        return false
    }
}
