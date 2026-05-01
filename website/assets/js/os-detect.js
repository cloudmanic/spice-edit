// =============================================================================
// File: assets/js/os-detect.js
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
//
// Detects the user's OS via navigator.userAgentData (modern) or
// navigator.userAgent (fallback). Sets data-os on <html> so CSS can show
// the matching install panel. Dispatches an 'osdetected' CustomEvent on
// document so other scripts can react.
// =============================================================================

(function () {
  // detectOS returns one of "mac" | "linux" | "windows" | "unknown".
  // It prefers the User-Agent Client Hints API when available because
  // that's what Chrome/Edge expose now that platform sniffing is locked
  // down; older Safari/Firefox still rely on parsing navigator.userAgent.
  function detectOS() {
    var uaData = navigator.userAgentData;
    if (uaData && typeof uaData.platform === "string") {
      var p = uaData.platform.toLowerCase();
      if (p.indexOf("mac") !== -1) return "mac";
      if (p.indexOf("linux") !== -1) return "linux";
      if (p.indexOf("win") !== -1) return "windows";
    }

    var ua = (navigator.userAgent || "").toLowerCase();
    if (ua.indexOf("mac os x") !== -1 || ua.indexOf("macintosh") !== -1) return "mac";
    if (ua.indexOf("iphone") !== -1 || ua.indexOf("ipad") !== -1) return "mac";
    if (ua.indexOf("android") !== -1) return "linux";
    if (ua.indexOf("linux") !== -1) return "linux";
    if (ua.indexOf("windows") !== -1 || ua.indexOf("win64") !== -1) return "windows";
    return "unknown";
  }

  // applyOS attaches the detected OS to <html data-os="..."> and emits
  // a CustomEvent so install-block listeners can sync their tab UI.
  // Honors a sticky user override stored in localStorage so manual
  // tab clicks survive a page reload.
  function applyOS() {
    var detected = detectOS();
    var stored = null;
    try { stored = localStorage.getItem("spiceedit:os"); } catch (e) { /* ignore */ }
    var os = stored || detected;
    document.documentElement.setAttribute("data-os", os);
    document.documentElement.setAttribute("data-os-detected", detected);
    document.dispatchEvent(new CustomEvent("osdetected", {
      detail: { os: os, detected: detected }
    }));
  }

  // setOS is exposed on the global so the install-block tabs can
  // override the OS and persist the choice.
  window.setSpiceEditOS = function (os) {
    if (["mac", "linux", "windows"].indexOf(os) === -1) return;
    try { localStorage.setItem("spiceedit:os", os); } catch (e) { /* ignore */ }
    document.documentElement.setAttribute("data-os", os);
    document.dispatchEvent(new CustomEvent("osdetected", {
      detail: { os: os, detected: document.documentElement.getAttribute("data-os-detected") }
    }));
  };

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", applyOS);
  } else {
    applyOS();
  }
})();
