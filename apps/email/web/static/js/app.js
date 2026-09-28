// Tansu Email: theme, lock, CSRF, and a small confirm prompt.
(function () {
  "use strict";

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

  document.addEventListener("htmx:configRequest", (event) => {
    const token = document.querySelector('meta[name="csrf-token"]')?.content;
    if (token) event.detail.headers["X-CSRF-Token"] = token;
  });

  if ("serviceWorker" in navigator) {
    navigator.serviceWorker.register("/service-worker");
  }

  const controllers = {};
  const DEFAULT_EVENT = { A: "click", BUTTON: "click", FORM: "submit", INPUT: "input", SELECT: "change" };

  function state(el) {
    return (el._kura = el._kura || {});
  }

  function controllerEl(el, name) {
    return el.closest(`[data-controller~="${name}"]`);
  }

  function dispatch(event) {
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
        impl({ event, element: el, scope });
      }
      el = el.parentElement?.closest?.("[data-action]");
    }
  }

  for (const type of ["click", "submit", "input", "change", "keydown", "pointerdown"]) {
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
    document.querySelector("meta[name='theme-color']")?.setAttribute("content", dark ? "#0a0d12" : "#f4f6f5");
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

  const IDLE_MS = 15 * 60 * 1000;
  const BACKGROUND_MS = 2 * 60 * 1000;
  const LOCK_KEY = "kura_auto_lock";
  let lockTimer = null;
  let lockHiddenAt = null;

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
    if (document.hidden) lockHiddenAt = Date.now();
    else if (lockHiddenAt && Date.now() - lockHiddenAt > BACKGROUND_MS) submitLock();
  });

  function syncViewport() {
    const vv = window.visualViewport;
    const height = vv?.height ?? window.innerHeight;
    const top = vv?.offsetTop ?? 0;
    document.documentElement.style.setProperty("--vvh", `${Math.round(height)}px`);
    document.documentElement.style.setProperty("--vvt", `${Math.round(top)}px`);
  }

  controllers.layout = {
    connect() {
      syncViewport();
      if (controllers.layout.bound) return;
      controllers.layout.bound = true;
      window.visualViewport?.addEventListener("resize", syncViewport);
      window.visualViewport?.addEventListener("scroll", syncViewport);
      window.addEventListener("resize", syncViewport);
      document.addEventListener("pointerdown", (event) => {
        const menu = document.querySelector("[data-layout-target~='menu']");
        if (menu && !menu.hidden && !event.target.closest(".mobile-menu, [data-layout-target~='menuButton']")) {
          menu.hidden = true;
        }
      });
    },
    toggleMenu() {
      const menu = document.querySelector("[data-layout-target~='menu']");
      if (!menu) return;
      menu.hidden = !menu.hidden;
    },
  };

  controllers.confirm = {
    ask({ event, element }) {
      const message = element.dataset.confirm || element.closest("[data-confirm]")?.dataset.confirm;
      if (message && !window.confirm(message)) event.preventDefault();
    },
  };

  controllers.offline = {
    connect() {
      const paint = () => {
        document.querySelectorAll("[data-offline-target~='banner']").forEach((b) => {
          b.hidden = navigator.onLine;
        });
      };
      paint();
      if (controllers.offline.bound) return;
      controllers.offline.bound = true;
      window.addEventListener("online", paint);
      window.addEventListener("offline", paint);
    },
  };

  document.addEventListener("htmx:afterRequest", async (event) => {
    const form = event.target?.closest?.('[data-controller~="logout"]');
    if (!form || !event.detail?.successful) return;
    try {
      const reg = await navigator.serviceWorker.getRegistration();
      reg?.active?.postMessage("logout");
    } catch {}
    window.location.href = "/login";
  });
})();
