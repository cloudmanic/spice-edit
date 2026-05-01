// =============================================================================
// File: assets/js/copy-button.js
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
//
// Wires every [data-copy] button on the page to copy the value of
// data-copy (or the textContent of the element referenced by
// data-copy-target) to the clipboard. Shows a "Copied!" toast for
// 1.5s and swaps the button's icon to a checkmark. Falls back to
// document.execCommand("copy") for older browsers.
// =============================================================================

(function () {
  var TOAST_DURATION = 1800;
  var ICON_DURATION = 1500;

  // toast renders the global toast pill with a message. It reuses a
  // single #site-toast node so concurrent copies don't pile up.
  function toast(message, isError) {
    var el = document.getElementById("site-toast");
    if (!el) {
      el = document.createElement("div");
      el.id = "site-toast";
      el.className = "toast";
      el.setAttribute("role", "status");
      el.setAttribute("aria-live", "polite");
      document.body.appendChild(el);
    }
    el.textContent = message;
    el.classList.toggle("is-error", !!isError);
    el.classList.add("is-visible");
    clearTimeout(el._t);
    el._t = setTimeout(function () { el.classList.remove("is-visible"); }, TOAST_DURATION);
  }

  // copyText writes a string to the system clipboard, preferring the
  // async Clipboard API and falling back to a hidden textarea + execCommand
  // for browsers (or insecure contexts) where navigator.clipboard is missing.
  function copyText(text) {
    if (navigator.clipboard && window.isSecureContext) {
      return navigator.clipboard.writeText(text);
    }
    return new Promise(function (resolve, reject) {
      try {
        var ta = document.createElement("textarea");
        ta.value = text;
        ta.setAttribute("readonly", "");
        ta.style.position = "fixed";
        ta.style.left = "-9999px";
        document.body.appendChild(ta);
        ta.select();
        var ok = document.execCommand("copy");
        document.body.removeChild(ta);
        ok ? resolve() : reject(new Error("execCommand failed"));
      } catch (err) { reject(err); }
    });
  }

  // resolveText finds the literal text a button intends to copy. Buttons
  // can either embed the command via data-copy or point at a sibling
  // element with data-copy-target (a CSS selector relative to document).
  function resolveText(btn) {
    var direct = btn.getAttribute("data-copy");
    if (direct) return direct;
    var sel = btn.getAttribute("data-copy-target");
    if (sel) {
      var node = document.querySelector(sel);
      if (node) return node.textContent.trim();
    }
    return "";
  }

  // markCopied flashes the button into a "copied" state for ~1.5s then
  // restores the original icon HTML so the same button stays usable.
  function markCopied(btn) {
    var original = btn.innerHTML;
    btn.classList.add("is-copied");
    btn.innerHTML = checkmarkSVG();
    btn.setAttribute("aria-label", "Copied");
    setTimeout(function () {
      btn.classList.remove("is-copied");
      btn.innerHTML = original;
      btn.setAttribute("aria-label", btn.getAttribute("data-copy-label") || "Copy");
    }, ICON_DURATION);
  }

  // checkmarkSVG returns the inline check icon used after a successful copy.
  function checkmarkSVG() {
    return '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="3 8 6.5 11.5 13 5"/></svg>';
  }

  // onClick handles the click event on any [data-copy] button.
  function onClick(e) {
    var btn = e.currentTarget;
    var text = resolveText(btn);
    if (!text) return;
    copyText(text).then(function () {
      markCopied(btn);
      var msg = btn.getAttribute("data-copy-toast") || "Copied to clipboard";
      toast(msg, false);
    }).catch(function () {
      toast("Couldn't copy. Highlight the command and copy manually.", true);
    });
  }

  // init wires every [data-copy] button in the document. Idempotent so
  // it can be safely called more than once if the page injects content.
  function init() {
    var buttons = document.querySelectorAll("[data-copy], [data-copy-target]");
    buttons.forEach(function (btn) {
      if (btn._copyWired) return;
      btn._copyWired = true;
      btn.addEventListener("click", onClick);
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
