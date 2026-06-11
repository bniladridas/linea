package com.bniladridas.linea

import android.annotation.SuppressLint
import android.os.Bundle
import android.view.KeyEvent
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.appcompat.app.AppCompatActivity
import java.net.HttpURLConnection
import java.net.URL

/**
 * Linea Android app.
 *
 * The bundled Go server binary (jniLibs/arm64-v8a/liblinea.so) is extracted
 * by the Android package manager at install time. This activity spawns it as
 * a subprocess, waits for it to become healthy, then loads the React UI in a
 * WebView — mirroring the macOS Swift wrapper.
 *
 * Build with `make android-check` or manually:
 *   cd android && GOOS=android GOARCH=arm64 CGO_ENABLED=0 \
 *     go build -o app/src/main/jniLibs/arm64-v8a/liblinea.so ../backend/cmd/server
 */
class MainActivity : AppCompatActivity() {

    private lateinit var webView: WebView

    private val serverUrl = "http://10.0.2.2:8080"

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
                    return host != "10.0.2.2"
                }
            }
        }
        setContentView(webView)

        // Connect to the host Go server (same instance as iOS).
        Thread {
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
        super.onDestroy()
    }

    // ── private helpers ────────────────────────────────────────────────────

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
