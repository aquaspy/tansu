// Tansu Assistant app bundle: vanilla JS behaviors for the data-controller /
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

  function targets(scope, ctrl, name) {
    return [...scope.querySelectorAll(`[data-${ctrl}-target~="${name}"]`)];
  }

  function target(scope, ctrl, name) {
    return scope.querySelector(`[data-${ctrl}-target~="${name}"]`);
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

  for (const type of ["click", "submit", "input", "change", "focusout", "keydown", "paste"]) {
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
      localStorage.setItem("kura.theme", next);
      document.documentElement.dataset.theme = next;
      paintTheme();
    },
    set({ event, element }) {
      event?.preventDefault();
      const theme = element?.dataset.themeChoice;
      if (!THEME_ORDER.includes(theme)) return;
      localStorage.setItem("kura.theme", theme);
      document.documentElement.dataset.theme = theme;
      paintTheme();
    },
  };

  // ---- lock ---------------------------------------------------------------
  const IDLE_MS = 15 * 60 * 1000;
  const BACKGROUND_MS = 2 * 60 * 1000;
  const LOCK_KEY = "kura_auto_lock";
  let lockTimer = null;
  let lockHiddenAt = null;

  function lockEnabled() {
    return (localStorage.getItem(LOCK_KEY) || document.cookie.match(/kura_auto_lock=(\d)/)?.[1] || "0") === "1";
  }

  function persistLock(enabled) {
    const value = enabled ? "1" : "0";
    localStorage.setItem(LOCK_KEY, value);
    const secure = window.location.protocol === "https:" ? "; Secure" : "";
    document.cookie = `${LOCK_KEY}=${value}; Path=/; Max-Age=31536000; SameSite=Lax${secure}`;
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
          document.querySelector("[data-layout-target~='menuButton']")?.setAttribute("aria-expanded", "false");
        }
      });
    },
    toggleMenu() {
      const menu = document.querySelector("[data-layout-target~='menu']");
      if (!menu) return;
      menu.hidden = !menu.hidden;
      document.querySelector("[data-layout-target~='menuButton']")?.setAttribute("aria-expanded", String(!menu.hidden));
    },
  };

  // ---- logout -------------------------------------------------------------
  document.addEventListener("htmx:afterRequest", async (event) => {
    const form = event.target?.closest?.('[data-controller~="logout"]');
    if (!form || !event.detail?.successful) return;
    for (const name of ["kurachat-v1", "kurachat-v2", "kurachat-v3"]) {
      try {
        await caches.delete(name);
      } catch {}
    }
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

  // ---- transcript ---------------------------------------------------------
  controllers.transcript = {
    connect(el) {
      const st = state(el);
      st.stick = true;
      el.addEventListener("scroll", () => {
        st.stick = el.scrollHeight - el.scrollTop - el.clientHeight < 64;
      });
      const onViewport = () => {
        if (st.stick) el.scrollTop = el.scrollHeight;
      };
      window.visualViewport?.addEventListener("resize", onViewport);
      el.scrollTop = el.scrollHeight;
      new MutationObserver(() => {
        // Swap in the real echoed pair; drop the optimistic preview.
        if (el.querySelector("article.msg")) document.getElementById("msg-echo")?.remove();
        if (st.stick) el.scrollTop = el.scrollHeight;
      }).observe(el, { childList: true, subtree: true });
    },
  };

  // ---- composer -----------------------------------------------------------
  function composerState(form) {
    const st = state(form);
    if (!st.composer) st.composer = { sending: false, chosen: [], previewUrls: [], draft: null };
    return st.composer;
  }

  function composerStreaming(form) {
    return form.dataset.composerStreamingValue === "true";
  }

  function composerMaxFiles(form) {
    const n = Number(form.dataset.composerMaxFilesValue || 4);
    return n > 0 ? n : 4;
  }

  function composerIsDoc(file) {
    return !file.type.startsWith("image/");
  }

  // Identity for deduping picks: a re-fired change event must not append
  // the same file twice.
  function pickedKey(file) {
    return [file.name, file.size, file.lastModified].join("|");
  }

  const docIconSVG = '<svg viewBox="0 0 16 16" aria-hidden="true"><path d="M4.5 1.8h4L12.5 6v8.2h-8Z" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linejoin="round"></path><path d="M8.5 1.8V6h4" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linejoin="round"></path></svg>';

  function composerEmpty(form) {
    const input = target(form, "composer", "input");
    const text = input ? input.value.trim() : "";
    return !text && composerState(form).chosen.length === 0;
  }

  function composerBlocked(form) {
    return composerState(form).sending || composerStreaming(form) || !navigator.onLine || composerEmpty(form);
  }

  function composerSync(form) {
    const input = target(form, "composer", "input");
    const file = target(form, "composer", "file");
    const submit = target(form, "composer", "submit");
    if (input) input.disabled = composerStreaming(form);
    if (file) file.disabled = composerStreaming(form);
    if (submit) submit.disabled = composerBlocked(form);
    voiceMicUI(form);
  }

  function composerResize(form) {
    const input = target(form, "composer", "input");
    if (!input) return;
    // Cap comes from CSS so min()/dvh stay the single source of truth.
    const cap = Number.parseFloat(getComputedStyle(input).maxHeight) || 192;
    input.style.height = "auto";
    input.style.height = `${Math.min(input.scrollHeight, cap)}px`;
  }

  function composerNarrow() {
    return window.matchMedia("(max-width: 860px)").matches;
  }

  function composerWriteFiles(form) {
    const file = target(form, "composer", "file");
    if (!file) return;
    const transfer = new DataTransfer();
    composerState(form).chosen.forEach((f) => transfer.items.add(f));
    file.files = transfer.files;
  }

  function composerRevokePreviews(form) {
    const st = composerState(form);
    st.previewUrls.forEach((url) => URL.revokeObjectURL(url));
    st.previewUrls = [];
  }

  function composerRemoveButton(form, index) {
    const remove = document.createElement("button");
    remove.type = "button";
    remove.className = "composer-chip-x";
    remove.textContent = "×";
    remove.dataset.action = "composer#removeFile";
    remove.dataset.composerIndexParam = String(index);
    const label = form.dataset.composerRemoveLabelValue;
    if (label) remove.setAttribute("aria-label", label);
    return remove;
  }

  function composerRenderChips(form) {
    const chips = target(form, "composer", "chips");
    if (!chips) return;
    composerRevokePreviews(form);
    chips.replaceChildren();
    composerState(form).chosen.forEach((file, index) => {
      const remove = composerRemoveButton(form, index);
      if (composerIsDoc(file)) {
        const chip = document.createElement("div");
        chip.className = "composer-chip-doc";
        chip.title = file.name;
        const icon = document.createElement("span");
        icon.innerHTML = docIconSVG;
        icon.firstChild.style.display = "block";
        const name = document.createElement("span");
        name.className = "composer-chip-name";
        name.textContent = file.name;
        chip.append(icon.firstChild, name, remove);
        chips.appendChild(chip);
        return;
      }
      const url = URL.createObjectURL(file);
      composerState(form).previewUrls.push(url);
      const chip = document.createElement("div");
      chip.className = "composer-chip";
      const img = document.createElement("img");
      img.alt = "";
      img.src = url;
      chip.append(img, remove);
      chips.appendChild(chip);
    });
    chips.hidden = composerState(form).chosen.length === 0;
  }

  function composerEcho(text, attachments) {
    const transcript = document.getElementById("transcript");
    const value = text == null ? "" : text.toString();
    const files = (attachments || []).filter(Boolean);
    if (!transcript || (!value.trim() && files.length === 0)) return;
    let node = document.getElementById("msg-echo");
    if (!node) {
      node = document.createElement("article");
      node.id = "msg-echo";
      node.className = "msg msg-user is-echo";
      transcript.appendChild(node);
    }
    node.replaceChildren();
    files.forEach((file) => {
      if (file.doc) {
        const row = document.createElement("div");
        row.className = "msg-doc";
        const icon = document.createElement("span");
        icon.innerHTML = docIconSVG;
        const name = document.createElement("span");
        name.className = "msg-doc-name";
        name.textContent = file.name;
        row.append(icon.firstChild, name);
        node.appendChild(row);
        return;
      }
      const wrap = document.createElement("div");
      wrap.className = "msg-image";
      const img = document.createElement("img");
      img.src = file.url;
      img.alt = "";
      wrap.appendChild(img);
      node.appendChild(wrap);
    });
    if (value.trim()) {
      const body = document.createElement("div");
      body.className = "msg-body";
      body.textContent = value;
      node.appendChild(body);
    }
    transcript.scrollTop = transcript.scrollHeight;
  }

  controllers.composer = {
    connect(form) {
      const st = composerState(form);
      st.sending = false;
      st.chosen = [];
      st.previewUrls = [];
      composerSync(form);
      composerResize(form);
      const input = target(form, "composer", "input");
      if (input?.hasAttribute("autofocus")) {
        input.focus({ preventScroll: true });
        requestAnimationFrame(() => {
          if (!st.sending && !composerStreaming(form)) input.focus({ preventScroll: true });
        });
      }
    },
    key({ event, scope }) {
      if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
        event.preventDefault();
        if (!composerBlocked(scope)) scope.requestSubmit();
      }
    },
    paste({ event, scope }) {
      const file = target(scope, "composer", "file");
      if (!file) return;
      const files = [...(event.clipboardData?.items || [])]
        .filter((entry) => entry.type.startsWith("image/") || entry.type === "application/pdf")
        .map((entry) => entry.getAsFile())
        .filter(Boolean);
      if (files.length === 0) return;
      event.preventDefault();
      const st = composerState(scope);
      st.chosen = [...st.chosen, ...files].slice(0, composerMaxFiles(scope));
      composerWriteFiles(scope);
      composerRenderChips(scope);
      composerSync(scope);
    },
    picked({ scope }) {
      const file = target(scope, "composer", "file");
      if (!file) return;
      const fresh = [...file.files];
      file.value = "";
      const st = composerState(scope);
      const seen = new Set(st.chosen.map((f) => pickedKey(f)));
      const deduped = fresh.filter((f) => {
        const key = pickedKey(f);
        if (seen.has(key)) return false;
        seen.add(key);
        return true;
      });
      st.chosen = [...st.chosen, ...deduped].slice(0, composerMaxFiles(scope));
      composerWriteFiles(scope);
      composerRenderChips(scope);
      composerSync(scope);
    },
    removeFile({ scope, params }) {
      const index = Number(params?.index);
      if (!Number.isInteger(index)) return;
      composerState(scope).chosen.splice(index, 1);
      composerWriteFiles(scope);
      composerRenderChips(scope);
      composerSync(scope);
    },
    resize({ scope }) {
      composerResize(scope);
    },
    sync({ scope }) {
      composerSync(scope);
    },
    // Persist the sticky web/deep toggles without a send (the send
    // persists them too). Deep implies web: checking deep checks the
    // globe, unchecking the globe unchecks deep — no dead states.
    // Ignores change events from other inputs.
    searchToggle({ event, scope }) {
      const box = event.target?.closest?.('input[name="web"], input[name="deep"]');
      if (!box || !scope.action) return;
      const web = scope.querySelector('input[name="web"]');
      const deep = scope.querySelector('input[name="deep"]');
      if (box === deep && box.checked && web) web.checked = true;
      if (box === web && !box.checked && deep) deep.checked = false;
      const url = scope.action.replace(/\/messages$/, "/settings");
      const token = document.querySelector('meta[name="csrf-token"]')?.content || "";
      fetch(url, {
        method: "PATCH",
        headers: {
          "Content-Type": "application/x-www-form-urlencoded",
          "X-CSRF-Token": token,
        },
        body: "web_search=" + (web && web.checked ? "1" : "0") +
          "&deep_search=" + (deep && deep.checked ? "1" : "0"),
      }).catch(() => {});
    },
  };

  // Optimistic echo on send; the input clears only once the POST succeeds
  // (clearing earlier could wipe the not-yet-serialized draft).
  document.addEventListener("htmx:beforeRequest", (event) => {
    const form = event.target?.closest?.("form.composer");
    if (!form) return;
    const st = composerState(form);
    const input = target(form, "composer", "input");
    st.draft = input?.value ?? "";
    st.sending = true;
    composerSync(form);
    composerEcho(
      st.draft,
      st.chosen.map((file) => ({
        url: URL.createObjectURL(file),
        name: file.name,
        doc: composerIsDoc(file),
      }))
    );
  });

  document.addEventListener("htmx:afterRequest", (event) => {
    const form = event.target?.closest?.("form.composer");
    if (!form) return;
    const st = composerState(form);
    const input = target(form, "composer", "input");
    st.sending = false;
    composerSync(form);
    if (event.detail?.successful) {
      if (input) {
        input.value = "";
        input.style.height = "";
      }
      st.chosen = [];
      composerWriteFiles(form);
      composerRenderChips(form);
      composerSync(form);
      if (input) {
        if (composerNarrow()) input.blur();
        else input.focus();
      }
    } else {
      document.getElementById("msg-echo")?.remove();
      if (input && st.draft != null && !input.value) {
        input.value = st.draft;
        composerResize(form);
      }
    }
  });

  // ---- voice --------------------------------------------------------------
  // Tap mic to dictate (auto-sends; the reply auto-plays), or tap Listen
  // on any finished reply. One shared player: a new request stops the old.
  const VOICE_CHUNK = 450;

  // Screen wake lock: held while recording or playing so a long reply
  // never pauses mid-sentence on mobile. Progressive enhancement:
  // unsupported browsers simply skip it.
  const voiceWake = { lock: null };

  function voiceWakeWant() {
    const form = document.querySelector("form.composer");
    return (form && voiceState(form).recording) || voicePlayer.busy;
  }

  async function voiceWakeSync() {
    if (!("wakeLock" in navigator)) return;
    if (voiceWakeWant() && !voiceWake.lock) {
      try {
        voiceWake.lock = await navigator.wakeLock.request("screen");
        voiceWake.lock.addEventListener("release", () => {
          voiceWake.lock = null;
        });
      } catch {
        voiceWake.lock = null;
      }
    } else if (!voiceWakeWant() && voiceWake.lock) {
      const lock = voiceWake.lock;
      voiceWake.lock = null;
      try {
        await lock.release();
      } catch {
        /* already gone */
      }
    }
  }

  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible") voiceWakeSync();
  });

  function voiceState(scope) {
    const st = state(scope);
    st.voice = st.voice || { recorder: null, chunks: [], recording: false, stream: null, staged: false };
    return st.voice;
  }

  function voiceStatus(form, key) {
    const el = form ? target(form, "voice", "status") : null;
    if (!el) return;
    if (!key) {
      el.hidden = true;
      el.textContent = "";
      return;
    }
    el.hidden = false;
    el.textContent = t(key);
  }

  function voiceMicUI(form) {
    const st = voiceState(form);
    const mic = target(form, "voice", "mic");
    if (!mic) return;
    mic.classList.toggle("recording", st.recording);
    mic.title = st.recording ? form.dataset.voiceStopValue : form.dataset.voiceMicValue;
    mic.disabled = composerStreaming(form);
  }

  function voiceMime() {
    if (typeof MediaRecorder === "undefined") return "";
    for (const mt of ["audio/webm;codecs=opus", "audio/webm", "audio/mp4"]) {
      if (MediaRecorder.isTypeSupported(mt)) return mt;
    }
    return "";
  }

  function voiceExt(mime) {
    if (mime.includes("mp4")) return "m4a";
    return "webm";
  }

  async function voiceStopTracks(form) {
    const st = voiceState(form);
    st.stream?.getTracks().forEach((tr) => tr.stop());
    st.stream = null;
  }

  function voiceFillInput(form, text) {
    const input = target(form, "composer", "input");
    if (!input) return;
    input.value = text;
    input.dispatchEvent(new Event("input", { bubbles: true }));
  }

  // voiceTranscribe uploads one recording; null on any failure (status set).
  async function voiceTranscribe(form, blob) {
    const url = form.dataset.voiceTranscribeValue;
    if (!url) return null;
    voiceStatus(form, "voice_transcribing");
    const data = new FormData();
    data.append("audio", blob, `rec.${voiceExt(blob.type || "")}`);
    try {
      const res = await fetch(url, {
        method: "POST",
        headers: { "X-CSRF-Token": document.querySelector('meta[name="csrf-token"]')?.content || "" },
        body: data,
      });
      const out = await res.json().catch(() => ({}));
      if (!res.ok || out.error) throw new Error(out.error || `http_${res.status}`);
      const text = (out.text || "").trim();
      if (!text) {
        voiceStatus(form, "voice_empty");
        return null;
      }
      return { text, cost: out.cost_usd, seconds: out.seconds };
    } catch {
      voiceStatus(form, "voice_failed");
      return null;
    }
  }

  // voiceFinishUpload transcribes, always polishes, then follows the
  // auto-send setting: submit right away or stage for review.
  async function voiceFinishUpload(form, blob) {
    const out = await voiceTranscribe(form, blob);
    if (!out) return;
    const entries = [
      ["stt_cost_usd", out.cost],
      ["stt_seconds", out.seconds],
    ];
    let text = out.text;
    const url = form.dataset.voicePolishValue;
    if (url) {
      voiceStatus(form, "voice_polishing");
      try {
        const res = await fetch(url, {
          method: "POST",
          headers: {
            "Content-Type": "application/x-www-form-urlencoded",
            "X-CSRF-Token": document.querySelector('meta[name="csrf-token"]')?.content || "",
          },
          body: `text=${encodeURIComponent(out.text)}`,
        });
        const polished = await res.json().catch(() => ({}));
        if (!res.ok || polished.error || !polished.text) throw new Error("polish");
        text = polished.text.trim();
        entries.push(["polish_cost_usd", polished.cost_usd]);
      } catch {
        // Keep the raw transcript rather than losing the dictation.
      }
    }
    voiceStatus(form, null);
    voiceFillInput(form, text);
    voiceStampMetering(form, entries);
    voiceState(form).staged = true;
    if (form.dataset.voiceAutosendValue === "1") form.requestSubmit();
    else target(form, "composer", "input")?.focus();
  }

  function voiceStampMetering(form, entries) {
    for (const old of form.querySelectorAll('input[data-voice-metering]')) old.remove();
    for (const [name, value] of entries) {
      if (value == null || Number.isNaN(Number(value))) continue;
      const hidden = document.createElement("input");
      hidden.type = "hidden";
      hidden.name = name;
      hidden.value = String(value);
      hidden.dataset.voiceMetering = "1";
      form.appendChild(hidden);
    }
  }

  // Player: sequential chunk queue over one Audio element.
  const voicePlayer = { audio: null, queue: [], busy: false, current: null };

  function voiceStopPlayback() {
    const p = voicePlayer;
    p.queue = [];
    p.busy = false;
    if (p.audio) {
      p.audio.pause();
      p.audio = null;
    }
    if (p.current) {
      voiceSpeakUI(p.current.scope, false);
      p.current = null;
    }
    voiceWakeSync();
  }

  function voiceSpeakUI(scope, playing) {
    const btn = scope ? target(scope, "voice", "speak") : null;
    if (!btn) return;
    btn.classList.toggle("playing", playing);
    const label = target(scope, "voice", "speak-label");
    const idle = scope.dataset.voiceIdleValue;
    const stop = scope.dataset.voiceStopValue;
    if (label) label.textContent = playing ? stop : idle;
    btn.title = playing ? stop : idle;
  }

  function voiceCleanText(raw) {
    return (raw || "")
      // Emojis (pictographs, flags, skin tones) become a space so speech
      // skips them without gluing words. Leftover ZWJ/variation selectors
      // are zero-width and TTS-silent, so need no rule of their own.
      .replace(/[\p{Extended_Pictographic}\p{Regional_Indicator}\p{Emoji_Modifier}]/gu, " ")
      .replace(/```[\s\S]*?```/g, " ")
      .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
      .replace(/[#>*_`]/g, "")
      .replace(/\s+/g, " ")
      .trim();
  }

  function voiceChunks(text) {
    const parts = text.match(/[^.!?…]+[.!?…]+["”)]?|\S[^.!?…]*$/g) || [];
    const out = [];
    let cur = "";
    for (const s of parts.map((x) => x.trim()).filter(Boolean)) {
      if ((cur + " " + s).trim().length > VOICE_CHUNK && cur) {
        out.push(cur.trim());
        cur = s;
      } else {
        cur = `${cur} ${s}`;
      }
    }
    if (cur.trim()) out.push(cur.trim());
    return out;
  }

  async function voicePlayQueue() {
    const p = voicePlayer;
    if (p.busy) return;
    p.busy = true;
    voiceWakeSync();
    while (p.queue.length > 0) {
      const job = p.queue[0];
      try {
        const res = await fetch(job.url, {
          method: "POST",
          headers: {
            "Content-Type": "application/x-www-form-urlencoded",
            "X-CSRF-Token": document.querySelector('meta[name="csrf-token"]')?.content || "",
          },
          body: `text=${encodeURIComponent(job.text)}&message_id=${encodeURIComponent(job.mid)}`,
        });
        if (!res.ok) throw new Error(`http_${res.status}`);
        const blob = await res.blob();
        await new Promise((resolve, reject) => {
          const audio = new Audio(URL.createObjectURL(blob));
          p.audio = audio;
          audio.onended = () => resolve();
          audio.onerror = () => reject(new Error("audio"));
          audio.play().catch(reject);
        });
      } catch {
        break;
      } finally {
        if (p.queue[0] === job) p.queue.shift();
      }
    }
    p.busy = false;
    if (p.current) {
      voiceSpeakUI(p.current.scope, false);
      p.current = null;
    }
    voiceWakeSync();
  }

  function voiceSpeakArticle(article) {
    if (!article || !article.isConnected) return;
    const nav = article.querySelector(".msg-toolbar");
    const source = article.querySelector('[data-copy-target="source"]');
    const text = voiceCleanText(source ? source.value : article.querySelector(".msg-body")?.textContent);
    if (!text || !nav) return;
    const btn = target(nav, "voice", "speak");
    const mid = btn?.dataset.voiceIdParam;
    const url = nav.dataset.voiceSpeakValue;
    if (!mid || !url) return;
    if (voicePlayer.current?.mid === mid) {
      voiceStopPlayback();
      return;
    }
    voiceStopPlayback();
    voiceSpeakUI(nav, true);
    voicePlayer.current = { mid, scope: nav };
    voicePlayer.queue = voiceChunks(text).map((chunk) => ({ url, mid, text: chunk }));
    voicePlayQueue();
  }

  // Auto-play every completed reply while read-aloud is on. Replies
  // arrive inflight and are swapped for a terminal node with the same id,
  // so track both phases; failures play nothing.
  function voiceWatchTranscript() {
    const root = document.getElementById("transcript");
    if (!root || root._voiceWatched) return;
    root._voiceWatched = true;
    const seen = new Set();
    for (const a of root.querySelectorAll("article")) {
      if (!a.id) continue;
      seen.add(a.id);
      if (a.querySelector(".msg-toolbar, .msg-error")) seen.add(`${a.id}#t`);
    }
    new MutationObserver((records) => {
      for (const rec of records) {
        for (const node of rec.addedNodes) {
          if (!(node instanceof Element)) continue;
          const articles = node.matches("article") ? [node] : [...node.querySelectorAll("article")];
          for (const a of articles) {
            if (!a.id) continue;
            const tools = a.querySelector(".msg-toolbar");
            const terminal = tools || a.querySelector(".msg-error");
            const key = terminal ? `${a.id}#t` : a.id;
            if (seen.has(key)) continue;
            seen.add(key);
            if (!tools) continue;
            const form = document.querySelector("form.composer");
            if (form && form.dataset.voiceAutospeakValue === "1") voiceSpeakArticle(a);
          }
        }
      }
    }).observe(root, { childList: true, subtree: true });
  }

  // Mirror the sticky voice toggles into the composer dataset so flips
  // take effect without a reload (the settings form PATCHes itself).
  document.addEventListener("change", (event) => {
    const box = event.target?.closest?.(".voice-setting");
    if (!box) return;
    const form = document.querySelector("form.composer");
    if (!form) return;
    const read = box.querySelector('input[name="voice_read_aloud"]');
    const send = box.querySelector('input[name="voice_auto_send"]');
    form.dataset.voiceAutospeakValue = read && read.checked ? "1" : "0";
    form.dataset.voiceAutosendValue = send && send.checked ? "1" : "0";
  });

  async function voiceRecordStart(form) {
    const st = voiceState(form);
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      st.stream = stream;
      const mime = voiceMime();
      const rec = mime ? new MediaRecorder(stream, { mimeType: mime }) : new MediaRecorder(stream);
      st.chunks = [];
      rec.ondataavailable = (ev) => {
        if (ev.data && ev.data.size > 0) st.chunks.push(ev.data);
      };
      rec.start();
      st.recorder = rec;
      st.recording = true;
      voiceStatus(form, null);
      voiceMicUI(form);
      voiceWakeSync();
    } catch {
      voiceStopTracks(form);
      voiceStatus(form, "voice_denied");
    }
  }

  async function voiceRecordStop(form) {
    const st = voiceState(form);
    st.recording = false;
    voiceMicUI(form);
    voiceWakeSync();
    try {
      await new Promise((resolve) => {
        st.recorder.onstop = resolve;
        st.recorder.stop();
      });
    } catch {
      voiceStatus(form, "voice_failed");
      return null;
    }
    voiceStopTracks(form);
    const blob = new Blob(st.chunks, { type: st.recorder?.mimeType || "audio/webm" });
    st.chunks = [];
    if (blob.size === 0) {
      voiceStatus(form, "voice_empty");
      return null;
    }
    return blob;
  }

  controllers.voice = {
    connect(scope) {
      if (scope.matches("form.composer")) {
        voiceMicUI(scope);
        voiceWatchTranscript();
      }
    },
    async toggle({ scope }) {
      const form = scope.matches("form.composer") ? scope : scope.closest("form.composer");
      if (!form || composerStreaming(form)) return;
      if (voiceState(form).recording) {
        const blob = await voiceRecordStop(form);
        if (blob) voiceFinishUpload(form, blob);
        return;
      }
      if (typeof MediaRecorder === "undefined" || !navigator.mediaDevices) {
        voiceStatus(form, "voice_denied");
        return;
      }
      voiceRecordStart(form);
    },
    speak({ scope, params }) {
      const nav = scope.matches(".msg-toolbar") ? scope : controllerEl(scope, "voice");
      const article = nav?.closest("article") || document.getElementById(`message_${params.id}`);
      voiceSpeakArticle(article);
    },
  };

  document.addEventListener("htmx:beforeRequest", (event) => {
    const form = event.target?.closest?.("form.composer");
    if (!form) return;
    const vst = voiceState(form);
    // Keep metering for the send it belongs to; drop it for later turns.
    if (vst.staged) {
      vst.staged = false;
      return;
    }
    for (const old of form.querySelectorAll("input[data-voice-metering]")) old.remove();
  });

  // ---- title --------------------------------------------------------------
  controllers.title = {
    connect(form) {
      const input = target(form, "title", "input");
      state(form).title = { saved: input?.value ?? "", submitting: false, dirty: false, timer: null };
      form.addEventListener("htmx:beforeRequest", () => {
        state(form).title.submitting = true;
      });
      form.addEventListener("htmx:afterRequest", () => {
        const st = state(form).title;
        const current = target(form, "title", "input");
        st.submitting = false;
        if (st.dirty && current && current.value !== st.saved) {
          st.dirty = false;
          controllers.title.save({ scope: form });
        } else {
          st.dirty = false;
        }
      });
    },
    schedule({ scope }) {
      const st = state(scope).title;
      clearTimeout(st.timer);
      st.timer = setTimeout(() => controllers.title.save({ scope }), 800);
    },
    save({ scope }) {
      const st = state(scope).title;
      const input = target(scope, "title", "input");
      if (!input) return;
      clearTimeout(st.timer);
      if (input.value === st.saved) return;
      if (st.submitting) {
        st.dirty = true;
        return;
      }
      st.saved = input.value;
      scope.requestSubmit();
    },
    sync({ scope }) {
      const input = target(scope, "title", "input");
      state(scope).title.saved = input?.value ?? "";
      state(scope).title.dirty = false;
    },
  };

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

  // ---- copy ---------------------------------------------------------------
  controllers.copy = {
    async copy({ scope }) {
      const source = target(scope, "copy", "source");
      const text = source ? source.value : "";
      if (!text) return;
      try {
        await navigator.clipboard.writeText(text);
      } catch {
        source.focus();
        source.select();
        document.execCommand("copy");
      }
      const label = target(scope, "copy", "label");
      if (!label) return;
      label.textContent = scope.dataset.copyCopiedValue || "";
      clearTimeout(state(scope).copyTimer);
      state(scope).copyTimer = setTimeout(() => {
        label.textContent = scope.dataset.copyIdleValue || "";
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
  };

  // ---- effort -------------------------------------------------------------
  // Keeps the selected pill visible when the segmented row overflows
  // (narrow screens scroll instead of wrapping). No-op when visible.
  controllers.effort = {
    connect(scope) {
      scope.querySelector("input:checked")?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
    },
  };

  // ---- modelmenu ----------------------------------------------------------
  // Custom model dropdown. Choosing an option sets the hidden input and
  // fires change so the settings form auto-saves through its hx-patch.
  function modelmenuClose(scope) {
    state(scope).menuOpen = false;
    const menu = target(scope, "modelmenu", "menu");
    const button = target(scope, "modelmenu", "button");
    if (menu) menu.hidden = true;
    button?.setAttribute("aria-expanded", "false");
  }

  controllers.modelmenu = {
    connect(scope) {
      state(scope).menuOpen = false;
      if (controllers.modelmenu.bound) return;
      controllers.modelmenu.bound = true;
      document.addEventListener("click", (event) => {
        document.querySelectorAll('[data-controller~="modelmenu"]').forEach((item) => {
          if (state(item).menuOpen && !item.contains(event.target)) modelmenuClose(item);
        });
      });
      document.addEventListener("keydown", (event) => {
        if (event.key !== "Escape") return;
        document.querySelectorAll('[data-controller~="modelmenu"]').forEach((item) => {
          if (!state(item).menuOpen) return;
          modelmenuClose(item);
          if (item.contains(event.target)) target(item, "modelmenu", "button")?.focus();
        });
      });
    },
    toggle({ scope }) {
      if (state(scope).menuOpen) {
        modelmenuClose(scope);
        return;
      }
      state(scope).menuOpen = true;
      const menu = target(scope, "modelmenu", "menu");
      const button = target(scope, "modelmenu", "button");
      if (menu) menu.hidden = false;
      button?.setAttribute("aria-expanded", "true");
    },
    key({ event, scope }) {
      if ((event.key === "ArrowDown" || event.key === "ArrowUp") && !state(scope).menuOpen) {
        event.preventDefault();
        controllers.modelmenu.toggle({ scope });
        const current = scope.querySelector('.model-option[aria-selected="true"]');
        (current || scope.querySelector(".model-option"))?.focus();
      }
    },
    choose({ scope, element, params }) {
      const value = params?.value || "";
      if (!value) return;
      const input = target(scope, "modelmenu", "value");
      if (input && input.value !== value) {
        input.value = value;
        input.dispatchEvent(new Event("change", { bubbles: true }));
      }
      scope.querySelectorAll(".model-option").forEach((opt) => {
        opt.setAttribute("aria-selected", String(opt === element));
      });
      const label = target(scope, "modelmenu", "label");
      const button = target(scope, "modelmenu", "button");
      if (label && params?.short) label.textContent = params.short;
      const dot = target(scope, "modelmenu", "tier");
      if (dot) {
        const tier = params?.tier || "";
        dot.className = tier ? `tier-dot tier-${tier}` : "tier-dot";
        dot.hidden = !tier;
      }
      button?.setAttribute("title", params?.title || value);
      modelmenuClose(scope);
      button?.focus();
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
        headers: { "X-CSRF-Token": token },
      });
      if (!resp.ok) return;
    } catch {
      return;
    }
    window.location.href = "/conversations/";
  });

  connectAll(document);
})();
