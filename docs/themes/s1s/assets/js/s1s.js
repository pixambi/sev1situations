(function () {
  var root = document.documentElement;
  var store = function (k, v) { try { v === undefined ? localStorage.getItem(k) : localStorage.setItem(k, v); } catch (e) {} };

  document.getElementById("theme").addEventListener("click", function () {
    var dark = root.dataset.theme ? root.dataset.theme === "dark" : matchMedia("(prefers-color-scheme: dark)").matches;
    root.dataset.theme = dark ? "light" : "dark";
    store("s1s-theme", root.dataset.theme);
  });

  document.querySelectorAll(".cmd .copy").forEach(function (b) {
    b.addEventListener("click", function () {
      var text = b.parentElement.querySelector("code").textContent;
      var done = function () { b.textContent = "Copied"; setTimeout(function () { b.textContent = "Copy"; }, 1500); };
      if (navigator.clipboard) navigator.clipboard.writeText(text).then(done, function () {});
    });
  });

  // Search runs in the page over an index built with the site, so it needs no network service.
  var box = document.getElementById("search"), list = document.getElementById("results"), index = null;
  var url = document.currentScript.dataset.index;
  function load() {
    if (index) return Promise.resolve(index);
    return fetch(url).then(function (r) { return r.json(); }).then(function (d) { index = d; return d; });
  }
  function hit(page, terms) {
    var t = page.title.toLowerCase(), x = page.text.toLowerCase(), score = 0;
    for (var i = 0; i < terms.length; i++) {
      var inTitle = t.indexOf(terms[i]) >= 0, inText = x.indexOf(terms[i]) >= 0;
      if (!inTitle && !inText) return 0;
      score += inTitle ? 10 : 1;
    }
    return score;
  }
  function snippet(page, term) {
    var i = page.text.toLowerCase().indexOf(term);
    return i < 0 ? "" : page.text.slice(Math.max(0, i - 40), i + 80).replace(/\s+/g, " ");
  }
  function show(items) {
    list.textContent = "";
    items.forEach(function (it) {
      var li = document.createElement("li"), a = document.createElement("a"), s = document.createElement("small");
      a.href = it.page.url; a.textContent = it.page.title; s.textContent = it.snip; a.appendChild(s); li.appendChild(a); list.appendChild(li);
    });
    if (!items.length) { var li = document.createElement("li"); li.style.padding = "0.5rem 1rem"; li.textContent = "No pages match."; list.appendChild(li); }
    list.hidden = false;
  }
  box.addEventListener("input", function () {
    var terms = box.value.toLowerCase().split(/\s+/).filter(Boolean);
    if (!terms.length) { list.hidden = true; return; }
    load().then(function (pages) {
      var found = pages.map(function (p) { return { page: p, score: hit(p, terms), snip: snippet(p, terms[0]) }; })
        .filter(function (r) { return r.score > 0; }).sort(function (a, b) { return b.score - a.score; }).slice(0, 8);
      show(found);
    });
  });
  document.addEventListener("keydown", function (e) { if (e.key === "Escape") { list.hidden = true; } });
  document.addEventListener("click", function (e) { if (!list.contains(e.target) && e.target !== box) list.hidden = true; });
})();
