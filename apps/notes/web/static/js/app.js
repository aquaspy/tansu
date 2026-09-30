// TansuNotes app bundle: vanilla JS behaviors for the data-controller /
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

  // Templates use dashed target names (data-folder-picker-target);
  // controller names in actions are camelCase (folderPicker#pick).
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
    document.querySelector("meta[name='theme-color']")?.setAttribute("content", dark ? "#0a0d12" : "#efeae2");
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

  // ---- layout -------------------------------------------------------------
  function syncViewport() {
    const vv = window.visualViewport;
    const height = vv?.height ?? window.innerHeight;
    const top = vv?.offsetTop ?? 0;
    document.documentElement.style.setProperty("--vvh", `${Math.round(height)}px`);
    document.documentElement.style.setProperty("--vvt", `${Math.round(top)}px`);
  }

  function loadUi() {
    try {
      const saved = JSON.parse(localStorage.getItem("kura.ui") || "{}");
      return {
        folders: saved.folders !== false,
        list: saved.list !== false,
        prevFolders: saved.prevFolders,
        prevList: saved.prevList,
      };
    } catch {
      return { folders: true, list: true };
    }
  }

  function applyUi(shell) {
    const ui = state(shell).ui || loadUi();
    state(shell).ui = ui;
    shell.classList.toggle("hide-folders", !ui.folders);
    shell.classList.toggle("hide-list", !ui.list);
    shell.classList.toggle("is-focus", !ui.folders && !ui.list);
  }

  function persistUi(shell) {
    localStorage.setItem("kura.ui", JSON.stringify(state(shell).ui));
    applyUi(shell);
  }

  controllers.layout = {
    connect(el) {
      syncViewport();
      applyUi(el);
      if (controllers.layout.bound) return;
      controllers.layout.bound = true;
      window.visualViewport?.addEventListener("resize", syncViewport);
      window.visualViewport?.addEventListener("scroll", syncViewport);
      window.addEventListener("resize", syncViewport);
      document.addEventListener("pointerdown", (event) => {
        const menu = document.querySelector("[data-layout-target~='menu']");
        if (menu && !menu.hidden && !event.target.closest(".mobile-menu, [data-layout-target~='menuButton']")) {
          menu.hidden = true;
          document.querySelector("[data-layout-target~='menuButton']")?.setAttribute("aria-expanded", "false");
        }
      });
      window.addEventListener("keydown", (event) => {
        if ((event.metaKey || event.ctrlKey) && event.key === "\\") {
          event.preventDefault();
          const shell = document.querySelector('[data-controller~="layout"]');
          if (shell) controllers.layout.toggleFocus({ scope: shell });
        }
      });
    },
    toggleMenu() {
      const menu = document.querySelector("[data-layout-target~='menu']");
      if (!menu) return;
      menu.hidden = !menu.hidden;
      document.querySelector("[data-layout-target~='menuButton']")?.setAttribute("aria-expanded", String(!menu.hidden));
    },
    toggleFolders({ scope }) {
      const ui = state(scope).ui || loadUi();
      ui.folders = !ui.folders;
      state(scope).ui = ui;
      persistUi(scope);
    },
    toggleList({ scope }) {
      const ui = state(scope).ui || loadUi();
      ui.list = !ui.list;
      state(scope).ui = ui;
      persistUi(scope);
    },
    toggleFocus({ scope }) {
      const ui = state(scope).ui || loadUi();
      const focused = !ui.folders && !ui.list;
      if (focused) {
        ui.folders = ui.prevFolders ?? true;
        ui.list = ui.prevList ?? true;
      } else {
        ui.prevFolders = ui.folders;
        ui.prevList = ui.list;
        ui.folders = false;
        ui.list = false;
      }
      state(scope).ui = ui;
      persistUi(scope);
    },
  };

  // ---- logout -------------------------------------------------------------
  document.addEventListener("htmx:afterRequest", async (event) => {
    const form = event.target?.closest?.('[data-controller~="logout"]');
    if (!form || !event.detail?.successful) return;
    for (const name of ["kuranotes-v7", "kuranotes-v8"]) {
      try {
        await caches.delete(name);
      } catch {}
    }
    try {
      // Unsaved offline drafts (prefix must match autosave below).
      const doomed = [];
      for (let i = 0; i < window.localStorage.length; i++) {
        const key = window.localStorage.key(i);
        if (key?.startsWith("kura_draft_")) doomed.push(key);
      }
      doomed.forEach((key) => window.localStorage.removeItem(key));
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

  // ---- autosave -----------------------------------------------------------
  const DRAFT_PREFIX = "kura_draft_";

  function autosaveState(form) {
    const st = state(form);
    if (!st.autosave) st.autosave = { dirty: false, timer: null, clearTimer: null };
    return st.autosave;
  }

  function draftKey(form) {
    const match = form.action.match(/\/notes\/(\d+)/);
    return match ? `${DRAFT_PREFIX}${match[1]}` : null;
  }

  function autosaveFields(form) {
    return {
      body: form.querySelector('[name="body"]'),
      folder: form.querySelector('[name="folder"]'),
    };
  }

  function storeDraft(form) {
    const key = draftKey(form);
    if (!key) return;
    const { body, folder } = autosaveFields(form);
    try {
      window.localStorage.setItem(key, JSON.stringify({
        body: body?.value ?? "",
        folder: folder?.value ?? "",
      }));
    } catch {}
  }

  function clearDraft(form) {
    const key = draftKey(form);
    if (!key) return;
    try {
      window.localStorage.removeItem(key);
    } catch {}
  }

  function restoreDraft(form) {
    const key = draftKey(form);
    if (!key) return;
    let draft;
    try {
      draft = JSON.parse(window.localStorage.getItem(key) || "null");
    } catch {
      return;
    }
    if (!draft || typeof draft !== "object") return;
    const { body, folder } = autosaveFields(form);
    const bodyChanged = typeof draft.body === "string" && body && draft.body !== body.value;
    const folderChanged = typeof draft.folder === "string" && folder && draft.folder !== folder.value;
    if (!bodyChanged && !folderChanged) return;
    if (bodyChanged) body.value = draft.body;
    if (folderChanged) folder.value = draft.folder;
    controllers.autosave.queue({ scope: form });
  }

  function autosaveStatus(form, text) {
    const status = target(form, "autosave", "status");
    if (status) status.textContent = text;
  }

  controllers.autosave = {
    connect(form) {
      restoreDraft(form);
      if (controllers.autosave.bound) return;
      controllers.autosave.bound = true;
      window.addEventListener("online", () => {
        document.querySelectorAll("form.editor-form").forEach((item) => {
          if (navigator.onLine && autosaveState(item).dirty) item.requestSubmit();
        });
      });
    },
    queue({ scope }) {
      const st = autosaveState(scope);
      st.dirty = true;
      storeDraft(scope);
      clearTimeout(st.timer);
      clearTimeout(st.clearTimer);
      if (!navigator.onLine) {
        autosaveStatus(scope, scope.dataset.autosaveOfflineValue || "");
        return;
      }
      autosaveStatus(scope, scope.dataset.autosaveSavingValue || "");
      st.timer = setTimeout(() => scope.requestSubmit(), 400);
    },
  };

  document.addEventListener("htmx:beforeRequest", (event) => {
    const form = event.target?.closest?.("form.editor-form");
    if (!form) return;
    autosaveStatus(form, form.dataset.autosaveSavingValue || "");
  });

  document.addEventListener("htmx:afterRequest", (event) => {
    const form = event.target?.closest?.("form.editor-form");
    if (!form) return;
    const st = autosaveState(form);
    const ok = event.detail?.successful && !event.detail?.failed;
    if (ok) {
      st.dirty = false;
      clearDraft(form);
    }
    autosaveStatus(form, ok ? form.dataset.autosaveSavedValue || "" : "");
    clearTimeout(st.clearTimer);
    if (ok) {
      st.clearTimer = setTimeout(() => {
        const status = target(form, "autosave", "status");
        if (status) status.textContent = "";
      }, 1600);
    }
  });

  // ---- share --------------------------------------------------------------
  controllers.share = {
    open({ scope }) {
      target(scope, "share", "box")?.showModal();
      const url = target(scope, "share", "url");
      url?.focus();
      url?.select();
    },
    close({ scope }) {
      target(scope, "share", "box")?.close();
    },
    backdrop({ event, scope }) {
      const box = target(scope, "share", "box");
      if (event.target === box) box?.close();
    },
    selectUrl({ scope }) {
      const url = target(scope, "share", "url");
      url?.focus();
      url?.select();
    },
    async copy({ scope }) {
      const url = target(scope, "share", "url");
      if (!url) return;
      try {
        await navigator.clipboard.writeText(url.value);
      } catch {
        url.focus();
        url.select();
        document.execCommand("copy");
      }
      const status = target(scope, "share", "status");
      if (!status) return;
      status.hidden = false;
      status.textContent = scope.dataset.shareCopiedValue || "";
      clearTimeout(state(scope).shareTimer);
      state(scope).shareTimer = setTimeout(() => {
        status.hidden = true;
      }, 1400);
    },
  };

  // ---- confirm ------------------------------------------------------------
  controllers.confirm = {
    open({ element, scope }) {
      const trigger = element;
      const destroy = target(scope, "confirm", "destroy");
      const message = target(scope, "confirm", "message");
      if (trigger.dataset.url && destroy) destroy.dataset.url = trigger.dataset.url;
      if (trigger.dataset.redirect && destroy) destroy.dataset.redirect = trigger.dataset.redirect;
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
    // Native confirm for plain forms (token revoke), like turbo_confirm.
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
    window.location.href = destroy.dataset.redirect || "/notes/";
  });

  // ---- folders ------------------------------------------------------------
  // Centers the active folder pill when the strip overflows (mobile).
  controllers.folders = {
    connect(scope) {
      const current = scope.querySelector(".folder-item.is-on");
      if (!current) return;
      const left = current.offsetLeft - (scope.clientWidth - current.offsetWidth) / 2;
      scope.scrollLeft = Math.max(0, left);
    },
  };

  // ---- folderPicker -------------------------------------------------------
  function pickerOptions(scope) {
    return targets(scope, "folderPicker", "option");
  }

  function pickerVisible(scope) {
    return pickerOptions(scope).filter((el) => !el.hidden);
  }

  function pickerApplyFilter(scope) {
    const input = target(scope, "folderPicker", "input");
    const q = (input?.value || "").trim().toLowerCase();
    pickerOptions(scope).forEach((el) => {
      const label = (el.dataset.label || el.textContent).trim().toLowerCase();
      el.hidden = q !== "" && !label.includes(q);
      el.classList.remove("is-active");
    });
    state(scope).pickerIndex = -1;
  }

  function pickerPlace(scope) {
    const input = target(scope, "folderPicker", "input");
    const list = target(scope, "folderPicker", "list");
    if (!input || !list) return;
    const rect = scope.getBoundingClientRect();
    const mobile = window.matchMedia("(max-width: 860px)").matches;
    const gap = 4;
    list.style.position = "fixed";
    if (mobile) {
      list.style.left = "0.5rem";
      list.style.right = "0.5rem";
      list.style.width = "auto";
    } else {
      list.style.left = `${Math.max(8, rect.left)}px`;
      list.style.right = "auto";
      list.style.width = `${Math.max(rect.width, 12 * 16)}px`;
    }
    const spaceBelow = window.innerHeight - rect.bottom;
    if (spaceBelow < 180 && rect.top > spaceBelow) {
      list.style.top = "auto";
      list.style.bottom = `${window.innerHeight - rect.top + gap}px`;
    } else {
      list.style.top = `${rect.bottom + gap}px`;
      list.style.bottom = "auto";
    }
  }

  function pickerClose(scope) {
    const list = target(scope, "folderPicker", "list");
    const input = target(scope, "folderPicker", "input");
    if (list) list.hidden = true;
    input?.setAttribute("aria-expanded", "false");
    state(scope).pickerIndex = -1;
    pickerOptions(scope).forEach((el) => el.classList.remove("is-active"));
  }

  function pickerChoose(scope, value) {
    const input = target(scope, "folderPicker", "input");
    if (!input) return;
    input.value = value ?? "";
    input.dispatchEvent(new Event("input", { bubbles: true }));
    pickerClose(scope);
  }

  controllers.folderPicker = {
    connect(scope) {
      state(scope).pickerIndex = -1;
      if (controllers.folderPicker.bound) return;
      controllers.folderPicker.bound = true;
      document.addEventListener("pointerdown", (event) => {
        document.querySelectorAll('[data-controller~="folderPicker"]').forEach((item) => {
          if (!item.contains(event.target)) pickerClose(item);
        });
      });
      const replace = () => {
        document.querySelectorAll('[data-controller~="folderPicker"]').forEach((item) => {
          const list = target(item, "folderPicker", "list");
          if (list && !list.hidden) pickerPlace(item);
        });
      };
      window.addEventListener("resize", replace);
      window.addEventListener("scroll", replace, true);
    },
    open({ scope }) {
      const list = target(scope, "folderPicker", "list");
      const input = target(scope, "folderPicker", "input");
      pickerApplyFilter(scope);
      if (list) list.hidden = false;
      input?.setAttribute("aria-expanded", "true");
      pickerPlace(scope);
    },
    toggle({ event, scope }) {
      event.preventDefault();
      const list = target(scope, "folderPicker", "list");
      if (!list) return;
      if (list.hidden) {
        controllers.folderPicker.open({ scope });
        target(scope, "folderPicker", "input")?.focus();
      } else {
        pickerClose(scope);
      }
    },
    filter({ scope }) {
      const list = target(scope, "folderPicker", "list");
      if (!list) return;
      if (list.hidden) controllers.folderPicker.open({ scope });
      else {
        pickerApplyFilter(scope);
        pickerPlace(scope);
      }
    },
    pick({ event, scope, element }) {
      event.preventDefault();
      pickerChoose(scope, element.dataset.value ?? "");
    },
    keydown({ event, scope }) {
      const list = target(scope, "folderPicker", "list");
      if (event.key === "Escape") {
        pickerClose(scope);
        return;
      }
      if (event.key !== "ArrowDown" && event.key !== "ArrowUp" && event.key !== "Enter") return;
      event.preventDefault();
      if (!list) return;
      if (list.hidden) controllers.folderPicker.open({ scope });
      const options = pickerVisible(scope);
      if (event.key === "Enter") {
        const index = state(scope).pickerIndex ?? -1;
        if (index >= 0 && options[index]) pickerChoose(scope, options[index].dataset.value ?? "");
        return;
      }
      if (options.length === 0) return;
      const delta = event.key === "ArrowDown" ? 1 : -1;
      const index = (((state(scope).pickerIndex ?? -1) + delta) % options.length + options.length) % options.length;
      state(scope).pickerIndex = index;
      options.forEach((el, i) => el.classList.toggle("is-active", i === index));
      options[index]?.scrollIntoView({ block: "nearest" });
    },
  };

  // ---- rename -------------------------------------------------------------
  controllers.rename = {
    connect(scope) {
      const box = target(scope, "rename", "box");
      box?.addEventListener("close", () => {
        const input = target(scope, "rename", "input");
        if (input) input.value = "";
      });
    },
    open({ element, scope }) {
      const from = element.dataset.from || "";
      const fromInput = target(scope, "rename", "from");
      const input = target(scope, "rename", "input");
      if (fromInput) fromInput.value = from;
      if (input) input.value = from;
      target(scope, "rename", "box")?.showModal();
      input?.focus();
      input?.select();
    },
    close({ scope }) {
      target(scope, "rename", "box")?.close();
    },
    backdrop({ event, scope }) {
      const box = target(scope, "rename", "box");
      if (event.target === box) box?.close();
    },
  };

  connectAll(document);
})();
