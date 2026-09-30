// TansuAccount app bundle: vanilla JS behaviors for the data-controller /
// data-action attributes in the templates. Listeners are delegated from
// the document so htmx swaps never orphan them.

(function () {
  "use strict";

  // ---- i18n ---------------------------------------------------------------
  function t(key) {
    if (!t.cache) {
      t.cache = {};
      try {
        const node = document.getElementById("i18n");
        if (node) t.cache = JSON.parse(node.textContent);
      } catch {
        t.cache = {};
      }
    }
    return t.cache[key] ?? key;
  }

  // ---- htmx CSRF ----------------------------------------------------------
  document.addEventListener("htmx:configRequest", (event) => {
    const token = document.querySelector('meta[name="csrf-token"]')?.content;
    if (token) event.detail.headers["X-CSRF-Token"] = token;
  });

  // ---- service worker -----------------------------------------------------
  if ("serviceWorker" in navigator) {
    navigator.serviceWorker.register("/service-worker");
  }

  // ---- controller plumbing ------------------------------------------------
  const controllers = {};
  const DEFAULT_EVENT = {
    A: "click",
    BUTTON: "click",
    FORM: "submit",
    INPUT: "input",
    TEXTAREA: "input",
    SELECT: "change",
    DIALOG: "click",
  };

  function state(el) {
    return (el._kura = el._kura || {});
  }

  function controllerEl(el, name) {
    return el.closest(`[data-controller~="${name}"]`);
  }

  // Templates use dashed target names (data-composer-target);
  // controller names in actions are camelCase (composer#open).
  function kebab(name) {
    return name.replace(/[A-Z]/g, (c) => `-${c.toLowerCase()}`);
  }

  function targets(scope, ctrl, name) {
    return [...scope.querySelectorAll(`[data-${kebab(ctrl)}-target~="${name}"]`)];
  }

  function target(scope, ctrl, name) {
    return scope.querySelector(`[data-${kebab(ctrl)}-target~="${name}"]`);
  }

  function paramsFor(el, ctrl) {
    const params = {};
    for (const [key, value] of Object.entries(el.dataset)) {
      const low = key.toLowerCase();
      if (low.startsWith(ctrl.toLowerCase()) && low.endsWith("param")) {
        const name = key.slice(ctrl.length, -5);
        params[name.charAt(0).toLowerCase() + name.slice(1)] = value;
      }
    }
    return params;
  }

  function dispatch(event) {
    // Walk up through nested data-action elements so inner actions never
    // shadow outer ones for other event types.
    let el = event.target?.closest?.("[data-action]");
    while (el) {
      for (const part of el.dataset.action.split(/\s+/)) {
        if (!part) continue;
        let name = part;
        let want = DEFAULT_EVENT[el.tagName] || "click";
        const arrow = part.indexOf("->");
        if (arrow >= 0) {
          want = part.slice(0, arrow);
          name = part.slice(arrow + 2);
        }
        if (want !== event.type) continue;
        const hash = name.indexOf("#");
        if (hash < 0) continue;
        const ctrl = name.slice(0, hash);
        const method = name.slice(hash + 1);
        const scope = controllerEl(el, ctrl) || el;
        const impl = controllers[ctrl]?.[method];
        if (typeof impl !== "function") continue;
        impl({ event, element: el, scope, params: paramsFor(el, ctrl) });
      }
      el = el.parentElement?.closest?.("[data-action]");
    }
  }

  for (const type of ["click", "submit", "input", "change", "focusin", "focusout", "keydown", "paste", "pointerdown"]) {
    document.addEventListener(type, dispatch);
  }

  function connectAll(root) {
    const scope = root || document;
    const els = [...scope.querySelectorAll("[data-controller]")];
    if (scope instanceof Element && scope.hasAttribute("data-controller")) els.unshift(scope);
    els.forEach((el) => {
      for (const name of (el.dataset.controller || "").split(/\s+/)) {
        const impl = controllers[name];
        if (!impl || state(el)[`connected:${name}`]) continue;
        state(el)[`connected:${name}`] = true;
        impl.connect?.(el);
      }
    });
  }

  document.addEventListener("DOMContentLoaded", () => connectAll(document));
  document.addEventListener("htmx:afterSwap", (event) => {
    if (event.target instanceof Element) connectAll(event.target);
  });

  // ---- theme --------------------------------------------------------------
  const THEME_ORDER = ["system", "light", "dark"];
  const THEME_LABEL = { system: "theme_system", light: "theme_light", dark: "theme_dark" };
  let themeMedia = null;

  function paintTheme() {
    const theme = document.documentElement.dataset.theme || "system";
    document.querySelectorAll("[data-theme-button]").forEach((el) => {
      el.title = `${t(THEME_LABEL[theme])} — ${t("theme_switch")}`;
      el.dataset.themeState = theme;
    });
    document.querySelectorAll("[data-theme-choice]").forEach((el) => {
      const on = el.dataset.themeChoice === theme;
      el.classList.toggle("is-on", on);
      el.setAttribute("aria-pressed", String(on));
    });
    const dark = theme === "dark" || (theme === "system" && themeMedia?.matches);
    document.querySelector("meta[name='theme-color']")?.setAttribute("content", dark ? "#0a0d12" : "#e9edeb");
  }

  controllers.theme = {
    connect() {
      if (!themeMedia) {
        themeMedia = window.matchMedia("(prefers-color-scheme: dark)");
        themeMedia.addEventListener?.("change", paintTheme);
      }
      paintTheme();
    },
    cycle({ event }) {
      event?.preventDefault();
      const current = document.documentElement.dataset.theme || "system";
      const next = THEME_ORDER[(THEME_ORDER.indexOf(current) + 1) % THEME_ORDER.length];
      rememberTheme(next);
    },
    set({ event, element }) {
      event?.preventDefault();
      const theme = element?.dataset.themeChoice;
      if (!THEME_ORDER.includes(theme)) return;
      rememberTheme(theme);
    },
  };

  // ---- lock ---------------------------------------------------------------
  const IDLE_MS = 15 * 60 * 1000;
  const BACKGROUND_MS = 2 * 60 * 1000;
  const LOCK_KEY = "kura_auto_lock";
  let lockTimer = null;
  let lockHiddenAt = null;

  function sharedCookieSuffix() {
    const host = location.hostname;
    const secure = location.protocol === "https:" ? "; Secure" : "";
    let domain = "";
    if (host && host !== "localhost" && !host.endsWith(".localhost") && !/^\d{1,3}(\.\d{1,3}){3}$/.test(host)) {
      const parts = host.split(".");
      if (parts.length >= 2) domain = "; Domain=" + parts.slice(-2).join(".");
    }
    return "; Path=/; SameSite=Lax" + domain + secure;
  }

  function writeSharedCookie(name, value, maxAge) {
    document.cookie = `${name}=${value}${sharedCookieSuffix()}; Max-Age=${maxAge}`;
  }

  function rememberTheme(theme) {
    localStorage.setItem("kura.theme", theme);
    writeSharedCookie("tansu_theme", theme, 31536000);
    document.documentElement.dataset.theme = theme;
    paintTheme();
  }

  function cookieValue(name) {
    const m = document.cookie.match(new RegExp("(?:^|; )" + name + "=([^;]*)"));
    return m ? decodeURIComponent(m[1]) : "";
  }

  function lockEnabled() {
    const fromCookie = cookieValue(LOCK_KEY);
    if (fromCookie === "1" || fromCookie === "0") return fromCookie === "1";
    return (localStorage.getItem(LOCK_KEY) || "0") === "1";
  }

  function persistLock(enabled) {
    const value = enabled ? "1" : "0";
    localStorage.setItem(LOCK_KEY, value);
    writeSharedCookie(LOCK_KEY, value, 31536000);
    refreshLockLabels();
    armLock();
  }

  function refreshLockLabels() {
    const scope = document.querySelector('[data-controller~="lock"]');
    const on = scope?.dataset.lockOnLabelValue || "";
    const off = scope?.dataset.lockOffLabelValue || "";
    if (!on || !off) return;
    const label = lockEnabled() ? off : on;
    document.querySelectorAll("[data-auto-lock-label]").forEach((el) => {
      el.textContent = label;
    });
  }

  function armLock() {
    clearTimeout(lockTimer);
    lockTimer = null;
    if (!lockEnabled()) return;
    lockTimer = setTimeout(submitLock, IDLE_MS);
  }

  function submitLock() {
    document.getElementById("lock-now-form")?.requestSubmit();
  }

  controllers.lock = {
    connect(el) {
      // Migrate a stored preference onto the cookie the server reads.
      const fromCookie = cookieValue(LOCK_KEY);
      if (fromCookie === "1" || fromCookie === "0") {
        persistLock(fromCookie === "1");
        return;
      }
      const stored = localStorage.getItem(LOCK_KEY);
      const initial = el.dataset.lockEnabledValue === "true";
      if (stored === "1" || stored === "0") persistLock(stored === "1");
      else persistLock(initial);
    },
    togglePreference({ event }) {
      event.preventDefault();
      persistLock(!lockEnabled());
    },
    lock() {
      submitLock();
    },
  };

  document.addEventListener("pointerdown", () => lockEnabled() && armLock());
  document.addEventListener("keydown", () => lockEnabled() && armLock());
  document.addEventListener("visibilitychange", () => {
    if (!lockEnabled()) return;
    if (document.hidden) {
      lockHiddenAt = Date.now();
    } else if (lockHiddenAt && Date.now() - lockHiddenAt > BACKGROUND_MS) {
      submitLock();
    }
  });

  // ---- menu ---------------------------------------------------------------
  controllers.menu = {
    connect() {
      if (controllers.menu.bound) return;
      controllers.menu.bound = true;
      document.addEventListener("pointerdown", (event) => {
        const menu = document.querySelector("[data-menu-target~='menu']");
        if (menu && !menu.hidden && !event.target.closest(".mobile-menu, [data-menu-target~='menuButton']")) {
          controllers.menu.close({});
        }
      });
    },
    toggle() {
      const menu = document.querySelector("[data-menu-target~='menu']");
      if (!menu) return;
      menu.hidden = !menu.hidden;
      document.querySelector("[data-menu-target~='menuButton']")?.setAttribute("aria-expanded", String(!menu.hidden));
    },
    close() {
      const menu = document.querySelector("[data-menu-target~='menu']");
      if (menu) menu.hidden = true;
      document.querySelector("[data-menu-target~='menuButton']")?.setAttribute("aria-expanded", "false");
    },
  };

  // ---- logout -------------------------------------------------------------
  document.addEventListener("htmx:afterRequest", async (event) => {
    const form = event.target?.closest?.('[data-controller~="logout"]');
    if (!form || !event.detail?.successful) return;
    try {
      await caches.delete("kuraaccount-v1");
    } catch {}
    try {
      const reg = await navigator.serviceWorker.getRegistration();
      reg?.active?.postMessage("logout");
    } catch {}
    window.location.href = "/login";
  });

  // ---- offline ------------------------------------------------------------
  controllers.offline = {
    connect(el) {
      const paint = () => {
        target(el, "offline", "banner")?.toggleAttribute("hidden", navigator.onLine);
      };
      paint();
      if (controllers.offline.bound) return;
      controllers.offline.bound = true;
      window.addEventListener("online", () =>
        document.querySelectorAll('[data-offline-target~="banner"]').forEach((b) => (b.hidden = navigator.onLine))
      );
      window.addEventListener("offline", () =>
        document.querySelectorAll('[data-offline-target~="banner"]').forEach((b) => (b.hidden = navigator.onLine))
      );
    },
  };

  // ---- confirm ------------------------------------------------------------
  controllers.confirm = {
    open({ element, scope }) {
      const trigger = element;
      const destroy = target(scope, "confirm", "destroy");
      const message = target(scope, "confirm", "message");
      if (trigger.dataset.url && destroy) destroy.dataset.url = trigger.dataset.url;
      if (destroy) destroy.dataset.redirect = trigger.dataset.redirect || "";
      if (trigger.dataset.message && message) message.textContent = trigger.dataset.message;
      target(scope, "confirm", "box")?.showModal();
    },
    close({ scope }) {
      target(scope, "confirm", "box")?.close();
    },
    backdrop({ event, scope }) {
      const box = target(scope, "confirm", "box");
      if (event.target === box) box?.close();
    },
    // Native confirm for plain forms (token revoke).
    ask({ event, element, scope }) {
      const message = scope.dataset?.confirmMessage || element.dataset?.confirmMessage || "";
      if (!window.confirm(message)) event.preventDefault();
    },
  };

  document.addEventListener("click", async (event) => {
    const destroy = event.target?.closest?.('[data-confirm-target~="destroy"]');
    if (!destroy || !destroy.dataset.url) return;
    event.preventDefault();
    const token = document.querySelector('meta[name="csrf-token"]')?.content || "";
    try {
      const resp = await fetch(destroy.dataset.url, {
        method: "DELETE",
        headers: { "X-CSRF-Token": token, "X-Requested-With": "fetch" },
      });
      if (!resp.ok) return;
    } catch {
      return;
    }
    // Follow an explicit redirect (deleting from the detail page);
    // otherwise reload in place.
    window.location.href = destroy.dataset.redirect || window.location.pathname + window.location.search;
  });

  connectAll(document);
})();
