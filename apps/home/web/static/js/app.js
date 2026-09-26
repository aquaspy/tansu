// TansuHome app bundle: vanilla JS behaviors for the data-controller /
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

  // Templates use dashed target names (data-dialog-target);
  // controller names in actions are camelCase (dialog#openSite).
  function kebab(name) {
    return name.replace(/[A-Z]/g, (c) => `-${c.toLowerCase()}`);
  }

  function target(scope, ctrl, name) {
    return scope.querySelector(`[data-${kebab(ctrl)}-target~="${name}"]`);
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
        impl({ event, element: el, scope });
      }
      el = el.parentElement?.closest?.("[data-action]");
    }
  }

  for (const type of ["click", "submit", "input", "change", "focusin", "focusout", "keydown", "paste", "pointerdown", "dragstart", "dragover", "drop", "dragend"]) {
    document.addEventListener(type, dispatch);
  }
  // load/error don't bubble; capture them for the favicon controller.
  for (const type of ["load", "error"]) {
    document.addEventListener(type, dispatch, true);
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

  // ---- logout -------------------------------------------------------------
  document.addEventListener("htmx:afterRequest", async (event) => {
    const form = event.target?.closest?.('[data-controller~="logout"]');
    if (!form || !event.detail?.successful) return;
    try {
      await caches.delete("kurahome-v1");
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

  // ---- clock --------------------------------------------------------------
  controllers.clock = {
    connect(scope) {
      const tick = () => {
        const now = new Date();
        const hour = now.getHours();
        const key = hour < 12 ? "morning" : hour < 18 ? "afternoon" : "evening";
        const greeting = target(scope, "clock", "greeting");
        if (greeting) greeting.textContent = t(key);
        const clock = target(scope, "clock", "clock");
        if (clock) {
          const locale = document.documentElement.lang || "en";
          clock.dateTime = now.toISOString();
          const date = now.toLocaleDateString(locale, { weekday: "long", month: "long", day: "numeric" });
          const time = now.toLocaleTimeString(locale, { hour: "numeric", minute: "2-digit" });
          clock.textContent = `${date} · ${time}`;
        }
      };
      tick();
      state(scope).clockTimer = setInterval(tick, 30_000);
    },
  };

  // ---- edit ---------------------------------------------------------------
  function paintEdit(scope) {
    const on = scope.classList.contains("is-editing");
    scope.querySelectorAll('[data-edit-target~="label"]').forEach((el) => {
      el.textContent = on ? t("done") : t("edit");
      el.closest("button")?.setAttribute("aria-pressed", String(on));
    });
  }

  controllers.edit = {
    toggle({ event, scope }) {
      event?.preventDefault();
      scope.classList.toggle("is-editing");
      paintEdit(scope);
    },
  };

  // ---- favicon ------------------------------------------------------------
  controllers.favicon = {
    connect(el) {
      if (el.complete && el.naturalWidth > 0) controllers.favicon.show({ element: el });
    },
    show({ element }) {
      element.closest(".mark")?.classList.add("has-icon");
    },
    hide({ element }) {
      element.remove();
    },
  };

  // ---- search -------------------------------------------------------------
  const ENGINES = [
    { id: "duckduckgo", action: "https://duckduckgo.com/" },
    { id: "startpage", action: "https://www.startpage.com/sp/search", query: "query" },
    { id: "kagi", action: "https://kagi.com/search" },
    { id: "google", action: "https://www.google.com/search" },
    { id: "brave", action: "https://search.brave.com/search" },
  ];

  function applyEngine(form, id) {
    const engine = ENGINES.find((item) => item.id === id) || ENGINES[0];
    form.action = engine.action;
    target(form, "search", "query")?.setAttribute("name", engine.query || "q");
    localStorage.setItem("kura.search", engine.id);
    const label = target(form, "search", "engine");
    if (label) label.textContent = t(`search_${engine.id === "duckduckgo" ? "ddg" : engine.id}`);
  }

  controllers.search = {
    connect(form) {
      applyEngine(form, localStorage.getItem("kura.search") || "duckduckgo");
      if (controllers.search.bound) return;
      controllers.search.bound = true;
      window.addEventListener("keydown", (event) => {
        if (event.key !== "/" || event.metaKey || event.ctrlKey || event.altKey) return;
        const tag = event.target.tagName;
        if (tag === "INPUT" || tag === "TEXTAREA" || event.target.isContentEditable) return;
        event.preventDefault();
        document.querySelector('[data-search-target~="query"]')?.focus();
      });
    },
    cycle({ event, scope }) {
      event.preventDefault();
      const current = localStorage.getItem("kura.search") || "duckduckgo";
      const index = ENGINES.findIndex((engine) => engine.id === current);
      applyEngine(scope, ENGINES[(index + 1) % ENGINES.length].id);
    },
  };

  // ---- dialog -------------------------------------------------------------
  function openDialog(scope, kind, trigger) {
    const idKey = kind === "site" ? "siteId" : kind === "stack" ? "itemId" : "profileId";
    const id = trigger.dataset[idKey];
    const form = target(scope, "dialog", `${kind}Form`);
    const base = kind === "stack" ? "/stack_items" : `/${kind}s`;
    form.action = id ? `${base}/${id}` : base;
    const method = target(scope, "dialog", `${kind}Method`);
    if (method) method.value = id ? "patch" : "post";
    const fields = {
      site: ["profile_id", "title", "url", "hint", "icon_url"],
      stack: ["profile_id", "category", "choice", "origin", "note", "url", "icon_url"],
      profile: ["name"],
    }[kind];
    const prefix = kind === "stack" ? "stack_item" : kind;
    for (const field of fields) {
      const camel = field.replace(/_([a-z])/g, (_, c) => c.toUpperCase());
      const input = form.querySelector(`[name='${prefix}[${field}]']`);
      if (input) input.value = trigger.dataset[camel] ?? "";
    }
    const heading = target(scope, "dialog", `${kind}Heading`);
    if (heading) heading.textContent = trigger.dataset.heading || "";
    const del = target(scope, "dialog", `${kind}Delete`);
    if (del) {
      let show = !!id;
      if (kind === "profile") show = show && trigger.dataset.destroyable === "true";
      del.hidden = !show;
      const delForm = del.querySelector("form");
      if (delForm && id) delForm.action = `${base}/${id}`;
    }
    const first = form.querySelector("input[type='text']");
    target(scope, "dialog", `${kind}Box`)?.showModal();
    first?.focus();
  }

  controllers.dialog = {
    openSite({ scope, element }) {
      openDialog(scope, "site", element);
    },
    openStack({ scope, element }) {
      openDialog(scope, "stack", element);
    },
    openProfile({ scope, element }) {
      openDialog(scope, "profile", element);
    },
    closeSite({ scope }) {
      target(scope, "dialog", "siteBox")?.close();
    },
    closeStack({ scope }) {
      target(scope, "dialog", "stackBox")?.close();
    },
    closeProfile({ scope }) {
      target(scope, "dialog", "profileBox")?.close();
    },
    backdrop({ event, scope }) {
      if (event.target === target(scope, "dialog", "siteBox")) controllers.dialog.closeSite({ scope });
      if (event.target === target(scope, "dialog", "profileBox")) controllers.dialog.closeProfile({ scope });
      const stackBox = target(scope, "dialog", "stackBox");
      if (stackBox && event.target === stackBox) controllers.dialog.closeStack({ scope });
    },
  };

  // ---- sort ---------------------------------------------------------------
  let dragging = null;

  controllers.sort = {
    start({ event, element }) {
      if (!element.closest(".is-editing")) {
        event.preventDefault();
        return;
      }
      dragging = element;
      event.dataTransfer.effectAllowed = "move";
      try {
        event.dataTransfer.setData("text/plain", element.dataset.id);
      } catch {}
      element.classList.add("is-dragging");
    },
    over({ event, element }) {
      if (!dragging) return;
      event.preventDefault();
      const row = element;
      if (row === dragging || !row.dataset.id) return;
      const rect = row.getBoundingClientRect();
      const vertical = row.tagName === "TR";
      const before = vertical
        ? event.clientY < rect.top + rect.height / 2
        : event.clientX < rect.left + rect.width / 2;
      row.parentNode.insertBefore(dragging, before ? row : row.nextSibling);
    },
    drop({ event, scope }) {
      event.preventDefault();
      const url = scope.dataset.sortUrlValue;
      if (!url) return;
      const ids = [...scope.querySelectorAll("[data-id]")].map((el) => el.dataset.id);
      const token = document.querySelector("meta[name='csrf-token']")?.content;
      const body = new URLSearchParams();
      ids.forEach((id) => body.append("ids[]", id));
      fetch(url, {
        method: "PATCH",
        headers: { "X-CSRF-Token": token || "", Accept: "application/json" },
        body,
      }).catch(() => {});
    },
    end() {
      dragging?.classList.remove("is-dragging");
      dragging = null;
    },
  };

  // ---- share (stack image export) -----------------------------------------
  controllers.share = {
    async download({ event, scope }) {
      event.preventDefault();
      const payload = target(scope, "share", "payload");
      if (!payload) return;
      const rows = JSON.parse(payload.textContent);
      if (!rows.length) return;
      await loadIcons(rows);
      const canvas = drawCard(rows, {
        title: scope.dataset.shareTitleValue,
        profile: scope.dataset.shareProfileValue,
        footer: scope.dataset.shareFooterValue,
      });
      const link = document.createElement("a");
      const slug = (scope.dataset.shareProfileValue || "stack").toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
      link.download = `kura-stack-${slug || "home"}.png`;
      link.href = canvas.toDataURL("image/png");
      link.click();
    },
  };

  function loadIcons(rows) {
    return Promise.all(rows.map((row) => new Promise((resolve) => {
      if (!row.icon) return resolve();
      const img = new Image();
      img.onload = () => { row.image = img; resolve(); };
      img.onerror = () => resolve();
      img.src = row.icon;
    })));
  }

  function drawCard(rows, copy) {
    const dpr = 2;
    const width = 1080;
    const pad = 72;
    const rowH = 78;
    const height = pad + 120 + rows.length * rowH + 80;
    const canvas = document.createElement("canvas");
    canvas.width = width * dpr;
    canvas.height = height * dpr;
    const ctx = canvas.getContext("2d");
    ctx.scale(dpr, dpr);

    ctx.fillStyle = "#f7f3eb";
    ctx.fillRect(0, 0, width, height);
    const glow = ctx.createRadialGradient(width / 2, 0, 40, width / 2, 0, 520);
    glow.addColorStop(0, "rgba(232, 212, 184, 0.85)");
    glow.addColorStop(1, "rgba(247, 243, 235, 0)");
    ctx.fillStyle = glow;
    ctx.fillRect(0, 0, width, 360);

    ctx.fillStyle = "#1c1916";
    ctx.font = "500 54px ui-serif, Palatino, Georgia, serif";
    ctx.fillText(copy.title, pad, pad + 48);
    ctx.fillStyle = "#8a8378";
    ctx.font = "400 22px ui-sans-serif, system-ui, sans-serif";
    ctx.fillText(copy.profile, pad, pad + 82);

    let y = pad + 130;
    rows.forEach((row) => {
      round(ctx, pad, y, 36, 36, 10);
      if (row.image) {
        ctx.save();
        ctx.clip();
        ctx.fillStyle = "#fffdf8";
        ctx.fill();
        ctx.drawImage(row.image, pad + 6, y + 6, 24, 24);
        ctx.restore();
      } else {
        ctx.fillStyle = row.color || "#b55220";
        ctx.fill();
        ctx.fillStyle = "#fff8f2";
        ctx.font = "500 18px ui-serif, Palatino, Georgia, serif";
        ctx.textAlign = "center";
        ctx.fillText(row.letter || "?", pad + 18, y + 24);
        ctx.textAlign = "left";
      }

      ctx.fillStyle = "#8a8378";
      ctx.font = "500 13px ui-sans-serif, system-ui, sans-serif";
      ctx.fillText((row.category || "").toUpperCase(), pad + 56, y + 14);
      ctx.fillStyle = "#1c1916";
      ctx.font = "550 22px ui-sans-serif, system-ui, sans-serif";
      ctx.fillText(row.choice || "", pad + 56, y + 38);
      ctx.fillStyle = "#8a8378";
      ctx.font = "400 16px ui-sans-serif, system-ui, sans-serif";
      const meta = [row.origin, row.note].filter(Boolean).join("  ·  ");
      ctx.fillText(meta, pad + 56, y + 60);
      y += rowH;
    });

    ctx.fillStyle = "#b55220";
    ctx.font = "500 16px ui-serif, Palatino, Georgia, serif";
    ctx.fillText(copy.footer || "TansuHome", pad, height - 36);
    return canvas;
  }

  function round(ctx, x, y, w, h, r) {
    ctx.beginPath();
    ctx.moveTo(x + r, y);
    ctx.arcTo(x + w, y, x + w, y + h, r);
    ctx.arcTo(x + w, y + h, x, y + h, r);
    ctx.arcTo(x, y + h, x, y, r);
    ctx.arcTo(x, y, x + w, y, r);
    ctx.closePath();
  }

  // ---- confirm ------------------------------------------------------------
  // Native confirm for plain delete forms (replaces turbo_confirm).
  controllers.confirm = {
    ask({ event, element, scope }) {
      const message = scope.dataset?.confirmMessage || element.dataset?.confirmMessage || "";
      if (!window.confirm(message)) event.preventDefault();
    },
  };

  connectAll(document);
})();
