/* Cookbook client: theme, timers, wake lock, recipe + cook mode helpers. */
(function () {
  'use strict';
  const $ = (s, r = document) => r.querySelector(s);
  const $$ = (s, r = document) => Array.from(r.querySelectorAll(s));
  const store = {
    get(k, d) { try { const v = localStorage.getItem(k); return v === null ? d : JSON.parse(v); } catch (e) { return d; } },
    set(k, v) { try { localStorage.setItem(k, JSON.stringify(v)); } catch (e) {} },
  };

  /* ---------- theme ---------- */
  function applyTheme(t) {
    const dark = t === 'dark' || (t === 'auto' && matchMedia('(prefers-color-scheme: dark)').matches);
    document.documentElement.setAttribute('data-bs-theme', dark ? 'dark' : 'light');
  }
  document.addEventListener('click', (e) => {
    const b = e.target.closest('[data-theme]');
    if (!b) return;
    try { localStorage.setItem('theme', b.dataset.theme); } catch (err) {}
    applyTheme(b.dataset.theme);
  });
  matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
    if ((localStorage.getItem('theme') || 'auto') === 'auto') applyTheme('auto');
  });

  /* ---------- wake lock ---------- */
  let lock = null;
  const wakeSupported = 'wakeLock' in navigator;
  async function acquire() {
    if (!wakeSupported || lock) return;
    try {
      lock = await navigator.wakeLock.request('screen');
      lock.addEventListener('release', () => { lock = null; syncWake(); });
    } catch (e) { lock = null; }
    syncWake();
  }
  function release() { if (lock) { lock.release(); lock = null; } syncWake(); }
  function wakeWanted() { return store.get('wake', document.body.dataset.cook ? true : false); }
  function syncWake() {
    $$('#toggle-wake').forEach((b) => {
      const on = !!lock;
      b.classList.toggle('active', on);
      b.setAttribute('aria-pressed', on);
      if (!wakeSupported) { b.disabled = true; b.title = 'Screen wake lock needs HTTPS and a supported browser'; }
    });
  }
  document.addEventListener('click', (e) => {
    if (!e.target.closest('#toggle-wake')) return;
    const want = !lock;
    store.set('wake', want);
    want ? acquire() : release();
  });
  document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'visible' && wakeWanted()) acquire(); });
  if (wakeWanted()) acquire(); else syncWake();

  /* ---------- timers ---------- */
  const TKEY = 'timers';
  let timers = store.get(TKEY, {});
  let audio = null;
  const save = () => store.set(TKEY, timers);
  const fmt = (s) => {
    s = Math.max(0, s);
    const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = s % 60;
    const p = (n) => String(n).padStart(2, '0');
    return h ? `${h}:${p(m)}:${p(sec)}` : `${m}:${p(sec)}`;
  };
  const remaining = (t) => t.state === 'running' ? Math.ceil((t.end - Date.now()) / 1000) : t.rem;

  function beep() {
    try {
      audio = audio || new (window.AudioContext || window.webkitAudioContext)();
      for (let i = 0; i < 4; i++) {
        const o = audio.createOscillator(), g = audio.createGain();
        o.type = 'sine'; o.frequency.value = 880;
        o.connect(g); g.connect(audio.destination);
        const t0 = audio.currentTime + i * 0.35;
        g.gain.setValueAtTime(0.0001, t0);
        g.gain.exponentialRampToValueAtTime(0.4, t0 + 0.02);
        g.gain.exponentialRampToValueAtTime(0.0001, t0 + 0.25);
        o.start(t0); o.stop(t0 + 0.3);
      }
    } catch (e) {}
    if (navigator.vibrate) navigator.vibrate([300, 150, 300, 150, 300]);
  }
  function notify(t) {
    try {
      if ('Notification' in window && Notification.permission === 'granted') new Notification('Timer done', { body: t.label, tag: t.id });
    } catch (e) {}
  }
  function pillsFor(id) { return $$('.timer').filter((p) => p.dataset.tid === id); }

  function startTimer(id, label, secs) {
    timers[id] = { id, label, total: secs, state: 'running', end: Date.now() + secs * 1000, rem: secs };
    save();
    try { audio = audio || new (window.AudioContext || window.webkitAudioContext)(); audio.resume && audio.resume(); } catch (e) {}
    if ('Notification' in window && Notification.permission === 'default') Notification.requestPermission();
    render();
  }
  function toggleTimer(id, label, secs) {
    const t = timers[id];
    if (!t) return startTimer(id, label, secs);
    if (t.state === 'running') { t.rem = Math.max(0, Math.ceil((t.end - Date.now()) / 1000)); t.state = 'paused'; }
    else if (t.state === 'paused') { t.end = Date.now() + t.rem * 1000; t.state = 'running'; }
    else { delete timers[id]; }
    save(); render();
  }
  function resetTimer(id) { delete timers[id]; save(); render(); }

  function assignIds() {
    const root = $('#recipe-content') || $('#cook');
    if (!root) return;
    const slug = root.dataset.slug;
    $$('.timer', root).forEach((p, i) => {
      p.dataset.tid = slug + ':' + i;
      if (!p.dataset.orig) p.dataset.orig = p.querySelector('.timer-text').textContent;
    });
  }

  document.addEventListener('click', (e) => {
    const p = e.target.closest('.timer');
    if (p) {
      e.stopPropagation();
      if (!p.dataset.tid) assignIds();
      const secs = parseInt(p.dataset.secs, 10) || 0;
      if (secs > 0) toggleTimer(p.dataset.tid, p.dataset.label || 'Timer', secs);
      return;
    }
    const tb = e.target.closest('[data-tray]');
    if (tb) {
      const id = tb.dataset.id;
      if (tb.dataset.tray === 'toggle') { const t = timers[id]; if (t) toggleTimer(id, t.label, t.total); }
      else resetTimer(id);
    }
  });
  document.addEventListener('contextmenu', (e) => {
    const p = e.target.closest('.timer');
    if (p && timers[p.dataset.tid]) { e.preventDefault(); resetTimer(p.dataset.tid); }
  });

  function render() {
    // pills
    $$('.timer').forEach((p) => {
      const t = timers[p.dataset.tid];
      const txt = p.querySelector('.timer-text');
      p.classList.remove('running', 'paused', 'done');
      if (!t) { if (p.dataset.orig) txt.textContent = p.dataset.orig; return; }
      p.classList.add(t.state);
      txt.textContent = t.state === 'done' ? "Time's up" : fmt(remaining(t));
    });
    // tray
    const tray = $('#timer-tray');
    if (!tray) return;
    const ids = Object.keys(timers);
    const sig = ids.join('|');
    if (tray.dataset.sig !== sig) {
      tray.dataset.sig = sig;
      tray.innerHTML = ids.map((id) => `<div class="tray-item" data-id="${id}"><span class="lbl"></span><span class="time"></span>
        <button class="btn btn-sm btn-light rounded-circle" data-tray="toggle" data-id="${id}" aria-label="Pause or resume"><i class="bi bi-pause-fill"></i></button>
        <button class="btn btn-sm btn-light rounded-circle" data-tray="reset" data-id="${id}" aria-label="Dismiss"><i class="bi bi-x-lg"></i></button></div>`).join('');
      ids.forEach((id) => { tray.querySelector(`.tray-item[data-id="${CSS.escape(id)}"] .lbl`).textContent = timers[id].label; });
    }
    ids.forEach((id) => {
      const t = timers[id], el = tray.querySelector(`.tray-item[data-id="${CSS.escape(id)}"]`);
      if (!el) return;
      el.classList.toggle('done', t.state === 'done');
      el.querySelector('.time').textContent = t.state === 'done' ? 'Done!' : fmt(remaining(t));
      el.querySelector('[data-tray=toggle] i').className = 'bi ' + (t.state === 'running' ? 'bi-pause-fill' : t.state === 'paused' ? 'bi-play-fill' : 'bi-arrow-counterclockwise');
    });
  }
  function tick() {
    let changed = false;
    Object.values(timers).forEach((t) => {
      if (t.state === 'running' && t.end - Date.now() <= 0) { t.state = 'done'; t.rem = 0; changed = true; beep(); notify(t); }
    });
    if (changed) save();
    render();
  }
  window.addEventListener('storage', (e) => { if (e.key === TKEY) { timers = store.get(TKEY, {}); render(); } });
  setInterval(tick, 250);

  /* ---------- recipe page ---------- */
  function initRecipe() {
    const root = $('#recipe-content');
    if (!root) return;
    assignIds();
    const slug = root.dataset.slug;
    document.body.classList.toggle('show-qty', !!store.get('inlineQty', false));
    const q = $('#toggle-qty'); if (q) { q.classList.toggle('active', document.body.classList.contains('show-qty')); }
    const split = $('.recipe-split', root);
    const lay = store.get('layout', 'both');
    if (split) split.dataset.layout = lay;
    const radio = $(`input[name=layout][value=${lay}]`, root); if (radio) radio.checked = true;
    const checked = store.get('chk:' + slug, []);
    $$('.ing-check', root).forEach((c, i) => { c.checked = checked.includes(i); });
    const done = store.get('done:' + slug, []);
    $$('.step', root).forEach((s) => s.classList.toggle('done', done.includes(parseInt(s.dataset.step, 10))));
    syncWake();
    render();
  }
  document.addEventListener('change', (e) => {
    const root = $('#recipe-content'); if (!root) return;
    if (e.target.matches('input[name=layout]')) {
      store.set('layout', e.target.value);
      $('.recipe-split', root).dataset.layout = e.target.value;
    }
    if (e.target.matches('.ing-check')) {
      const idx = $$('.ing-check', root).map((c, i) => c.checked ? i : -1).filter((i) => i >= 0);
      store.set('chk:' + root.dataset.slug, idx);
    }
  });
  document.addEventListener('click', (e) => {
    const root = $('#recipe-content'); if (!root) return;
    if (e.target.closest('#toggle-qty')) {
      const on = !document.body.classList.contains('show-qty');
      document.body.classList.toggle('show-qty', on);
      e.target.closest('#toggle-qty').classList.toggle('active', on);
      store.set('inlineQty', on);
      return;
    }
    const step = e.target.closest('.step');
    if (step && !e.target.closest('a,button,.timer')) {
      step.classList.toggle('done');
      const done = $$('.step.done', root).map((s) => parseInt(s.dataset.step, 10));
      store.set('done:' + root.dataset.slug, done);
    }
  });
  document.body.addEventListener('htmx:afterSettle', initRecipe);

  /* ---------- cook mode ---------- */
  function initCook() {
    const root = $('#cook'); if (!root) return;
    assignIds();
    const slides = $$('[data-slide]', root), key = 'cookstep:' + root.dataset.slug;
    let i = 0;
    try { i = Math.min(parseInt(sessionStorage.getItem(key) || '0', 10) || 0, slides.length - 1); } catch (e) {}
    const show = (n) => {
      i = Math.max(0, Math.min(slides.length - 1, n));
      slides.forEach((s, k) => s.classList.toggle('active', k === i));
      $('#cook-bar').style.width = ((i + 1) / slides.length * 100) + '%';
      $('#prev').disabled = i === 0;
      $('#next').disabled = i === slides.length - 1;
      try { sessionStorage.setItem(key, i); } catch (e) {}
      window.scrollTo(0, 0);
    };
    $('#prev').addEventListener('click', () => show(i - 1));
    $('#next').addEventListener('click', () => show(i + 1));
    document.addEventListener('keydown', (e) => {
      if (e.target.matches('input,textarea,select')) return;
      if (e.key === 'ArrowRight' || e.key === ' ' || e.key === 'PageDown') { e.preventDefault(); show(i + 1); }
      if (e.key === 'ArrowLeft' || e.key === 'PageUp') { e.preventDefault(); show(i - 1); }
    });
    let x0 = null, y0 = null;
    root.addEventListener('touchstart', (e) => { x0 = e.touches[0].clientX; y0 = e.touches[0].clientY; }, { passive: true });
    root.addEventListener('touchend', (e) => {
      if (x0 === null) return;
      const dx = e.changedTouches[0].clientX - x0, dy = e.changedTouches[0].clientY - y0;
      x0 = null;
      if (Math.abs(dx) > 70 && Math.abs(dx) > Math.abs(dy) * 1.5) show(i + (dx < 0 ? 1 : -1));
    }, { passive: true });
    show(i);
  }

  document.addEventListener('DOMContentLoaded', () => { initRecipe(); initCook(); render(); });
})();
