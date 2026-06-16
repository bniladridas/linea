(function () {
  var html = document.documentElement;
  var key = "linea-theme";
  var saved;
  try { saved = localStorage.getItem(key); } catch (e) {}

  function setTheme(t) {
    if (t === "solarized") { html.classList.add("solarized"); }
    else { html.classList.remove("solarized"); }
  }

  function update() {
    var btn = document.getElementById("tbtn");
    if (btn) { btn.textContent = html.classList.contains("solarized") ? "Default" : "Solarized"; }
  }

  function toggle() {
    var on = html.classList.contains("solarized");
    setTheme(on ? "" : "solarized");
    try { localStorage.setItem(key, on ? "" : "solarized"); } catch (e) {}
    update();
  }

  setTheme(saved);
  var btn = document.getElementById("tbtn");
  if (btn) { btn.addEventListener("click", toggle); update(); }
})();
