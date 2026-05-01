// =============================================================================
// File: assets/js/site.js
// Author: Spicer Matthews <spicer@cloudmanic.com>
// Created: 2026-04-30
// Copyright: 2026 Cloudmanic, LLC. All rights reserved.
//
// Glue script for site-wide interactivity: install-block tab switching,
// fade-up scroll reveals, mobile nav toggle, and docs sidebar mobile
// dropdown. Plain ES module style, no framework.
// =============================================================================

(function () {

  // wireInstallTabs binds the .install-block tab buttons. Clicking a tab
  // updates aria-selected on the buttons, toggles .is-active on panels,
  // and (via the global helper from os-detect.js) persists the user's OS
  // override so the install pill in the hero matches.
  function wireInstallTabs() {
    var blocks = document.querySelectorAll("[data-install-block]");
    blocks.forEach(function (block) {
      var tabs = block.querySelectorAll("[data-install-tab]");
      var panels = block.querySelectorAll("[data-install-panel]");
      tabs.forEach(function (tab) {
        tab.addEventListener("click", function () {
          var os = tab.getAttribute("data-install-tab");
          tabs.forEach(function (t) {
            t.setAttribute("aria-selected", t === tab ? "true" : "false");
          });
          panels.forEach(function (p) {
            p.classList.toggle("is-active", p.getAttribute("data-install-panel") === os);
          });
          if (window.setSpiceEditOS && os !== "source") {
            window.setSpiceEditOS(os);
          }
        });
      });

      // sync tabs to detected/stored OS on the osdetected event
      document.addEventListener("osdetected", function (ev) {
        var os = ev.detail && ev.detail.os;
        if (!os || os === "unknown") os = "mac";
        tabs.forEach(function (t) {
          var match = t.getAttribute("data-install-tab") === os;
          t.setAttribute("aria-selected", match ? "true" : "false");
        });
        panels.forEach(function (p) {
          p.classList.toggle("is-active", p.getAttribute("data-install-panel") === os);
        });
      });
    });
  }

  // wireReveal arms .reveal nodes with the .reveal-armed class then
  // observes them. The arming step is what hides them initially; without
  // JS or with reduced motion, .reveal is always fully visible. The
  // observer is single-shot and unobserves each node once seen.
  function wireReveal() {
    var nodes = document.querySelectorAll(".reveal");
    var prefersReduced = window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    if (prefersReduced || !("IntersectionObserver" in window)) {
      // No animation at all — leave nodes fully visible.
      return;
    }
    nodes.forEach(function (el) { el.classList.add("reveal-armed"); });
    var io = new IntersectionObserver(function (entries) {
      entries.forEach(function (entry) {
        if (entry.isIntersecting) {
          entry.target.classList.add("is-visible");
          io.unobserve(entry.target);
        }
      });
    }, { rootMargin: "0px 0px -10% 0px", threshold: 0.05 });
    nodes.forEach(function (el) { io.observe(el); });
  }

  // wireMobileMenu toggles the mobile nav drawer when the menu button
  // is pressed. Closes on link click and on Escape.
  function wireMobileMenu() {
    var btn = document.querySelector("[data-mobile-toggle]");
    var menu = document.getElementById("mobile-menu");
    if (!btn || !menu) return;

    function close() {
      menu.classList.remove("is-open");
      btn.setAttribute("aria-expanded", "false");
      document.body.style.overflow = "";
    }
    function open() {
      menu.classList.add("is-open");
      btn.setAttribute("aria-expanded", "true");
      document.body.style.overflow = "hidden";
    }

    btn.addEventListener("click", function () {
      menu.classList.contains("is-open") ? close() : open();
    });
    menu.querySelectorAll("a").forEach(function (a) {
      a.addEventListener("click", close);
    });
    document.addEventListener("keydown", function (e) {
      if (e.key === "Escape" && menu.classList.contains("is-open")) close();
    });
  }

  // wireDocsMobileNav binds the <select> on the docs index/page so picking
  // a doc navigates to the corresponding URL. Only present on mobile.
  function wireDocsMobileNav() {
    var sel = document.getElementById("docs-mobile-select");
    if (!sel) return;
    sel.addEventListener("change", function () {
      if (sel.value) window.location.href = sel.value;
    });
  }

  // init runs the boot routine once the DOM is ready.
  function init() {
    wireInstallTabs();
    wireReveal();
    wireMobileMenu();
    wireDocsMobileNav();
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
