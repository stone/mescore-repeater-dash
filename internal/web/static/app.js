'use strict';
// meshtui web dashboard: state from /api/events (SSE), charts on <canvas>.
(() => {
  const $ = (id) => document.getElementById(id);
  const NS = 'http://www.w3.org/2000/svg';
  const THEMES = ['synthwave', 'phosphor', 'monokai', 'nord'];
  const MAX = 900; // 30 min at 2 s, same as the server rings
  const DUTY_LIMIT = 10; // % — EU 869.4–869.65 MHz
  const ICON = { rx: '⇣', tx: '⇡', error: '⚠', reboot: '⟳', neighbor: '★', signal: 'ϟ', cmd: '⌘' };

  let S = null;
  let connected = false;
  const hover = { sig: null, snr: null, pkt: null };
  const css = {};
  const store = {
    get(k) { try { return localStorage.getItem(k); } catch { return null; } },
    set(k, v) { try { localStorage.setItem(k, v); } catch { /* private mode */ } },
  };

  // ---------- formatting ----------
  const pad2 = (n) => String(n).padStart(2, '0');
  function fmtDur(s) {
    s = Math.max(0, Math.floor(s));
    if (s >= 86400) return `${Math.floor(s / 86400)}d${pad2(Math.floor(s % 86400 / 3600))}h`;
    if (s >= 3600) return `${Math.floor(s / 3600)}h${pad2(Math.floor(s % 3600 / 60))}m`;
    if (s >= 60) return `${Math.floor(s / 60)}m${pad2(s % 60)}s`;
    return `${s}s`;
  }
  const hms = (t) => { const d = new Date(t * 1000); return `${pad2(d.getHours())}:${pad2(d.getMinutes())}:${pad2(d.getSeconds())}`; };
  const fixed = (v, n = 1) => (v === undefined || v === null || Number.isNaN(v)) ? '–' : Number(v).toFixed(n);

  // ---------- theme ----------
  function readColors() {
    const cs = getComputedStyle(document.documentElement);
    for (const k of ['fg', 'dim', 'muted', 'border', 'c-rx', 'c-tx', 'panel', 'bg']) css[k] = cs.getPropertyValue('--' + k).trim();
  }
  function setTheme(t) {
    if (!THEMES.includes(t)) t = THEMES[0];
    document.documentElement.dataset.theme = t;
    $('themeName').textContent = t[0].toUpperCase() + t.slice(1);
    store.set('meshtui-theme', t);
    readColors();
    drawCharts();
  }

  // ---------- canvas helpers ----------
  function prep(c) {
    const r = c.getBoundingClientRect();
    const d = window.devicePixelRatio || 1;
    const w = Math.max(1, Math.round(r.width * d)), h = Math.max(1, Math.round(r.height * d));
    if (c.width !== w || c.height !== h) { c.width = w; c.height = h; }
    const x = c.getContext('2d');
    x.setTransform(d, 0, 0, d, 0, 0);
    x.clearRect(0, 0, r.width, r.height);
    x.font = '11px system-ui, sans-serif';
    return { x, w: r.width, h: r.height };
  }
  function empty(x, w, h, msg) {
    x.fillStyle = css.muted; x.textAlign = 'center'; x.textBaseline = 'middle';
    x.font = 'italic 13px system-ui, sans-serif';
    x.fillText(msg, w / 2, h / 2);
  }
  function niceStep(span, n) {
    const raw = span / n, p = Math.pow(10, Math.floor(Math.log10(raw)));
    for (const m of [1, 2, 5, 10]) if (m * p >= raw) return m * p;
    return 10 * p;
  }
  function hline(x, y, x0, x1, color, dash) {
    x.strokeStyle = color; x.lineWidth = 1; x.setLineDash(dash || []);
    x.beginPath(); x.moveTo(x0, Math.round(y) + .5); x.lineTo(x1, Math.round(y) + .5); x.stroke(); x.setLineDash([]);
  }
  function bar(x, cx, base, top, bw) { // rounded at the data end, square at the baseline
    const y0 = Math.min(base, top), hh = Math.abs(top - base);
    if (hh < .5) return;
    const r = Math.min(2, bw / 2, hh);
    x.beginPath();
    if (top < base) x.roundRect(cx, y0, bw, hh, [r, r, 0, 0]);
    else x.roundRect(cx, y0, bw, hh, [0, 0, r, r]);
    x.fill();
  }

  // ---------- charts ----------
  function drawSignal() {
    const c = $('sigChart'); const { x, w, h } = prep(c);
    const H = S && S.history; const n = H ? H.t.length : 0;
    if (n < 2) return empty(x, w, h, 'waiting for data…');
    const P = { l: 38, r: 8, t: 6, b: 20 };
    let lo = Math.min(...H.noise, ...H.rssi), hi = Math.max(...H.noise, ...H.rssi);
    lo = Math.floor(lo - 3); hi = Math.ceil(hi + 3);
    const t0 = H.t[0], t1 = H.t[n - 1];
    const X = (t) => P.l + (t - t0) / ((t1 - t0) || 1) * (w - P.l - P.r);
    const Y = (v) => P.t + (hi - v) / (hi - lo) * (h - P.t - P.b);
    const step = niceStep(hi - lo, 4);
    x.textAlign = 'right'; x.textBaseline = 'middle'; x.fillStyle = css.dim;
    for (let v = Math.ceil(lo / step) * step; v <= hi; v += step) {
      hline(x, Y(v), P.l, w - P.r, css.border); x.fillText(v, P.l - 6, Y(v));
    }
    x.textBaseline = 'top';
    for (let i = 0; i <= 3; i++) {
      const t = t0 + (t1 - t0) * i / 3;
      x.textAlign = i === 0 ? 'left' : i === 3 ? 'right' : 'center';
      x.fillText(hms(t), X(t), h - P.b + 6);
    }
    // RSSI: area + 2px line
    const g = x.createLinearGradient(0, P.t, 0, h - P.b);
    g.addColorStop(0, css['c-rx'] + '55'); g.addColorStop(1, css['c-rx'] + '00');
    x.beginPath(); x.moveTo(X(H.t[0]), h - P.b);
    for (let i = 0; i < n; i++) x.lineTo(X(H.t[i]), Y(H.rssi[i]));
    x.lineTo(X(H.t[n - 1]), h - P.b); x.closePath(); x.fillStyle = g; x.fill();
    const line = (arr, color, dash, lw) => {
      x.strokeStyle = color; x.lineWidth = lw; x.lineJoin = 'round'; x.setLineDash(dash);
      x.beginPath(); for (let i = 0; i < n; i++) i ? x.lineTo(X(H.t[i]), Y(arr[i])) : x.moveTo(X(H.t[i]), Y(arr[i]));
      x.stroke(); x.setLineDash([]);
    };
    line(H.noise, css.dim, [5, 4], 1.5);
    line(H.rssi, css['c-rx'], [], 2);
    const i = hover.sig;
    if (i !== null && i < n) {
      x.strokeStyle = css.dim; x.lineWidth = 1; x.beginPath(); x.moveTo(Math.round(X(H.t[i])) + .5, P.t); x.lineTo(Math.round(X(H.t[i])) + .5, h - P.b); x.stroke();
      for (const [arr, col] of [[H.noise, css.dim], [H.rssi, css['c-rx']]]) {
        x.beginPath(); x.arc(X(H.t[i]), Y(arr[i]), 4.5, 0, 7); x.fillStyle = col; x.fill();
        x.lineWidth = 2; x.strokeStyle = css.panel; x.stroke();
      }
    }
    c._map = (mx) => { let best = 0, bd = 1e9; for (let k = 0; k < n; k++) { const d = Math.abs(X(H.t[k]) - mx); if (d < bd) { bd = d; best = k; } } return best; };
  }

  function drawSNR() {
    const c = $('snrChart'); const { x, w, h } = prep(c);
    const H = S && S.history; const all = H ? H.snr : [];
    if (!all.length) return empty(x, w, h, 'waiting for data…');
    const P = { l: 30, r: 4, t: 6, b: 8 }, bw = 4, gap = 2;
    const fit = Math.max(1, Math.floor((w - P.l - P.r) / (bw + gap)));
    const vals = all.slice(-fit), off = all.length - vals.length;
    const lo = -20, hi = 15;
    const Y = (v) => P.t + (hi - Math.max(lo, Math.min(hi, v))) / (hi - lo) * (h - P.t - P.b);
    x.fillStyle = css.dim; x.textAlign = 'right'; x.textBaseline = 'middle';
    for (const v of [10, 0, -10, -20]) { hline(x, Y(v), P.l, w - P.r, css.border, v === -10 ? [3, 3] : []); x.fillText(v, P.l - 6, Y(v)); }
    x.textAlign = 'left'; x.fillText('decode limit', P.l + 4, Y(-10) - 8);
    const x0 = w - P.r - vals.length * (bw + gap);
    x.fillStyle = css['c-rx'];
    vals.forEach((v, k) => { x.globalAlpha = hover.snr === off + k ? 1 : .85; bar(x, x0 + k * (bw + gap), Y(0), Y(v), bw); });
    x.globalAlpha = 1;
    c._map = (mx) => { const k = Math.floor((mx - x0) / (bw + gap)); return k >= 0 && k < vals.length ? off + k : null; };
  }

  function drawPackets() {
    const c = $('pktChart'); const { x, w, h } = prep(c);
    const H = S && S.history; const rx = H ? H.rx_per_min : [], tx = H ? H.tx_per_min : [];
    if (!rx.length) return empty(x, w, h, 'waiting for data…');
    const P = { l: 38, r: 4, t: 6, b: 6 }, bw = 4, gap = 2;
    const fit = Math.max(1, Math.floor((w - P.l - P.r) / (bw + gap)));
    const r = rx.slice(-fit), t = tx.slice(-fit), off = rx.length - r.length;
    const top = Math.max(1, ...r, ...t);
    const mid = P.t + (h - P.t - P.b) / 2, half = (h - P.t - P.b) / 2;
    x.fillStyle = css.dim; x.textAlign = 'right'; x.textBaseline = 'middle';
    x.fillText(Math.round(top), P.l - 6, P.t + 6); x.fillText('0', P.l - 6, mid); x.fillText(Math.round(top), P.l - 6, h - P.b - 6);
    hline(x, mid, P.l, w - P.r, css.border);
    const x0 = w - P.r - r.length * (bw + gap);
    r.forEach((v, k) => {
      const cx = x0 + k * (bw + gap);
      x.globalAlpha = hover.pkt === null || hover.pkt === off + k ? 1 : .8;
      x.fillStyle = css['c-rx']; bar(x, cx, mid - 1, mid - 1 - v / top * (half - 1), bw);
      x.fillStyle = css['c-tx']; bar(x, cx, mid + 1, mid + 1 + t[k] / top * (half - 1), bw);
    });
    x.globalAlpha = 1;
    c._map = (mx) => { const k = Math.floor((mx - x0) / (bw + gap)); return k >= 0 && k < r.length ? off + k : null; };
  }

  function drawSpark() {
    const c = $('battSpark'); const { x, w, h } = prep(c);
    const v = S ? S.history.battery.slice(-200) : [];
    if (v.length < 2) return;
    let lo = Math.min(...v), hi = Math.max(...v);
    if (hi - lo < .05) { lo -= .025; hi += .025; }
    x.strokeStyle = css['c-rx']; x.lineWidth = 1.5; x.beginPath();
    v.forEach((b, i) => { const px = i / (v.length - 1) * (w - 2) + 1, py = 2 + (hi - b) / (hi - lo) * (h - 4); i ? x.lineTo(px, py) : x.moveTo(px, py); });
    x.stroke();
  }

  function drawCharts() { if (!S) return; drawSignal(); drawSNR(); drawPackets(); drawSpark(); }

  // ---------- tooltips ----------
  const tip = $('tip-box');
  function showTip(e, rows) {
    tip.replaceChildren();
    for (const [k, v] of rows) {
      const d = document.createElement('div');
      if (k) { const s = document.createElement('span'); s.className = 'k'; s.textContent = k + ' '; d.append(s); }
      d.append(v); tip.append(d);
    }
    tip.hidden = false;
    const r = tip.getBoundingClientRect();
    tip.style.left = Math.min(e.clientX + 14, innerWidth - r.width - 8) + 'px';
    tip.style.top = Math.max(8, e.clientY - r.height - 10) + 'px';
  }
  function bindHover(id, key, rows) {
    const c = $(id);
    c.addEventListener('pointermove', (e) => {
      if (!c._map || !S) return;
      const i = c._map(e.clientX - c.getBoundingClientRect().left);
      if (hover[key] !== i) { hover[key] = i; drawCharts(); }
      if (i === null) { tip.hidden = true; return; }
      showTip(e, rows(i));
    });
    c.addEventListener('pointerleave', () => { hover[key] = null; tip.hidden = true; drawCharts(); });
  }
  bindHover('sigChart', 'sig', (i) => [[null, hms(S.history.t[i])], ['RSSI', `${S.history.rssi[i]} dBm`], ['Noise', `${S.history.noise[i]} dBm`]]);
  bindHover('snrChart', 'snr', (i) => [[null, hms(S.history.t[i])], ['SNR', `${fixed(S.history.snr[i], 2)} dB`]]);
  bindHover('pktChart', 'pkt', (i) => [['rx', `${Math.round(S.history.rx_per_min[i])} /min`], ['tx', `${Math.round(S.history.tx_per_min[i])} /min`]]);

  // ---------- antenna ----------
  let tipTimer = 0;
  function ring(kind, delay) {
    setTimeout(() => {
      const c = document.createElementNS(NS, 'circle');
      c.setAttribute('cx', '100'); c.setAttribute('cy', '34'); c.setAttribute('r', '92');
      c.setAttribute('class', 'ring ' + kind);
      c.addEventListener('animationend', () => c.remove());
      $('rings').append(c);
      $('tip').classList.add('active');
      clearTimeout(tipTimer);
      tipTimer = setTimeout(() => $('tip').classList.remove('active'), 1300);
    }, delay);
  }

  // ---------- counters ----------
  const shown = { rxTotal: 0, txTotal: 0 };
  function countTo(id, to) {
    const from = shown[id]; shown[id] = to;
    if (from === to) { $(id).textContent = to; return; }
    const start = performance.now(), dur = from === 0 ? 0 : 700;
    const stepFn = (now) => {
      const f = dur ? Math.min(1, (now - start) / dur) : 1;
      $(id).textContent = Math.round(from + (to - from) * (1 - Math.pow(1 - f, 3)));
      if (f < 1) requestAnimationFrame(stepFn);
    };
    requestAnimationFrame(stepFn);
  }

  // ---------- render ----------
  function renderStatus() {
    const pill = $('statusPill'), st = $('status');
    let cls = 'wait', txt = 'CONNECTING';
    if (S && connected) {
      if (S.up) { cls = 'live'; txt = 'LIVE'; }
      else if (S.failures >= 3) { cls = 'down'; txt = 'NO SIGNAL'; }
      else { cls = 'wait'; txt = 'WAITING'; }
    } else if (S) { txt = 'RECONNECTING'; }
    pill.className = 'status ' + cls; st.textContent = txt;
    const down = S && connected && !S.up && S.failures >= 3;
    $('banner').hidden = !down;
    if (down) $('bannerText').textContent = `${S.failures} failed polls, retrying… last seen ${S.last_ok ? fmtDur((Date.now() - S.last_ok) / 1000) + ' ago' : 'never'}`;
    $('tip').classList.toggle('dead', !!down);
  }

  function renderTick() {
    const d = new Date();
    $('clock').textContent = `${pad2(d.getHours())}:${pad2(d.getMinutes())}:${pad2(d.getSeconds())}`;
    if (!S) return;
    if (S.last_ok) {
      const since = (Date.now() - S.last_ok) / 1000;
      $('updated').textContent = `updated ${fmtDur(since)} ago`;
      $('uptime').textContent = fmtDur(S.latest.core.uptime_secs + (S.up ? since : 0));
    }
    renderStatus();
  }

  function renderInfo() {
    const i = S.info;
    $('name').textContent = i.name || 'meshtui';
    document.title = (i.name ? i.name + ' · ' : '') + 'meshtui';
    $('fw').hidden = !i.firmware; $('fw').textContent = i.firmware;
    $('radio').hidden = !i.freq_mhz;
    $('radio').textContent = `${fixed(i.freq_mhz, 3)} MHz · SF${i.sf} · BW${i.bw_khz} · CR${i.cr}`;
  }

  function renderStats() {
    const L = S.latest, c = L.core, r = L.radio, p = L.packets;
    const v = c.battery_mv / 1000;
    $('battV').textContent = c.battery_mv ? v.toFixed(2) + 'V' : '–';
    $('battV2').textContent = c.battery_mv ? v.toFixed(2) + ' V' : '–';
    const f = Math.max(0, Math.min(1, (c.battery_mv - 3300) / 900));
    $('battFill').style.width = (f * 100) + '%';
    $('battFill').style.background = f > .5 ? 'var(--good)' : f > .2 ? 'var(--warn)' : 'var(--err)';
    countTo('rxTotal', p.recv); countTo('txTotal', p.sent);
    $('lastPkt').textContent = S.last_ok ? `last packet ${r.last_rssi} dBm · SNR ${fixed(r.last_snr, 2)} dB` : 'waiting for data…';
    $('sigNow').textContent = S.last_ok ? `noise ${r.noise_floor} · rssi ${r.last_rssi} dBm` : '';
    $('snrNow').textContent = S.last_ok ? `${fixed(r.last_snr, 2)} dB` : '';
    const H = S.history, n = H.rx_per_min.length;
    $('pktNow').textContent = n ? `rx ${Math.round(H.rx_per_min[n - 1])} · tx ${Math.round(H.tx_per_min[n - 1])}` : '';
    const d = H.duty.length ? H.duty[H.duty.length - 1] : null;
    $('duty').textContent = d === null ? 'warming up…' : d.toFixed(2) + '%';
    $('dutyMask').style.width = d === null ? '100%' : (100 - Math.min(100, d / DUTY_LIMIT * 100)) + '%';
    $('airTx').textContent = fmtDur(r.tx_air_secs); $('airRx').textContent = fmtDur(r.rx_air_secs);
    $('floodRx').textContent = p.flood_rx; $('floodTx').textContent = p.flood_tx;
    $('directRx').textContent = p.direct_rx; $('directTx').textContent = p.direct_tx;
    $('errs').textContent = p.recv_errors; $('errs').className = p.recv_errors ? 'warn' : '';
    $('queue').textContent = c.queue_len;
  }

  function renderNeighbors() {
    const nb = S.neighbors || [], body = $('nbBody');
    $('nbCount').textContent = `(${nb.length})`;
    body.replaceChildren();
    if (!nb.length) {
      const tr = body.insertRow(), td = tr.insertCell(); td.colSpan = 3; td.className = 'muted';
      td.textContent = 'none heard yet — keep listening'; return;
    }
    for (const n of nb) {
      const tr = body.insertRow();
      const id = tr.insertCell(); id.className = 'id'; id.textContent = n.id;
      const s = tr.insertCell(); const m = document.createElement('span'); m.className = 'meter';
      const fill = document.createElement('i'); fill.style.width = Math.max(4, Math.min(100, (n.snr + 10) / 20 * 100)) + '%';
      m.append(fill); s.append(m, `${n.snr >= 0 ? '+' : ''}${fixed(n.snr)} dB`);
      tr.insertCell().textContent = fmtDur(n.heard_ago_s) + ' ago';
    }
  }

  function addEvents(evs, fresh) {
    const feed = $('feed');
    if (feed.firstElementChild && feed.firstElementChild.classList.contains('muted')) feed.replaceChildren();
    for (const e of evs) {
      const li = document.createElement('li');
      li.className = 'k-' + e.kind + (fresh ? ' new' : '') + (e.kind === 'signal' && e.text.includes('lost') ? ' lost' : '');
      const t = document.createElement('time'); const d = new Date(e.t);
      t.textContent = `${pad2(d.getHours())}:${pad2(d.getMinutes())}:${pad2(d.getSeconds())}`;
      const ic = document.createElement('span'); ic.className = 'ic'; ic.textContent = ICON[e.kind] || '·';
      const tx = document.createElement('span'); tx.textContent = e.text;
      li.append(t, ic, tx); feed.prepend(li);
    }
    while (feed.children.length > 100) feed.lastElementChild.remove();
  }

  function renderHelp() {
    const byKey = Object.fromEntries((S.help || []).map((h) => [h.key, h]));
    document.querySelectorAll('.card[data-help]').forEach((card) => {
      const h = byKey[card.dataset.help]; const p = card.querySelector('.help');
      if (h && p) p.textContent = h.text;
    });
    const dl = document.createElement('dl');
    for (const h of S.help || []) {
      const dt = document.createElement('dt'); dt.textContent = h.title;
      const dd = document.createElement('dd'); dd.textContent = h.text; dl.append(dt, dd);
    }
    const dt = document.createElement('dt'); dt.textContent = 'Keys';
    const dd = document.createElement('dd');
    dd.textContent = 'h / ? help · t theme' + (S.console ? ' · : console (↑↓ history). Read-only commands run at once; anything that could change the repeater asks first.' : '. The console is off — start meshtui with -web-console to enable it.');
    dl.append(dt, dd);
    $('helpBody').replaceChildren(dl);
  }

  function renderConsole() {
    $('consoleCard').hidden = !S.console; $('consoleKey').hidden = !S.console;
    const dl = $('cmds');
    if (S.console && !dl.children.length) for (const c of S.commands || []) { const o = document.createElement('option'); o.value = c; dl.append(o); }
  }

  function renderAll() { renderInfo(); renderStats(); renderNeighbors(); renderTick(); drawCharts(); }

  // ---------- live data ----------
  const push = (arr, v) => { arr.push(v); if (arr.length > MAX) arr.shift(); };
  function applyUpdate(u) {
    if (!S) return;
    S.up = u.up; S.failures = u.failures; S.last_ok = u.last_ok;
    if (u.info) S.info = u.info;
    if (u.latest) S.latest = u.latest;
    const p = u.point, H = S.history;
    if (p) {
      push(H.t, p.t); push(H.noise, p.noise); push(H.rssi, p.rssi); push(H.snr, p.snr); push(H.battery, p.battery);
      if (p.rx_per_min !== undefined) { push(H.rx_per_min, p.rx_per_min); push(H.tx_per_min, p.tx_per_min); }
      if (p.duty !== undefined) push(H.duty, p.duty);
    }
    if (u.neighbors) S.neighbors = u.neighbors;
    if (u.events && u.events.length) addEvents(u.events, true);
    for (let i = 0; i < Math.min(u.new_rx || 0, 3); i++) ring('rx', i * 220);
    for (let i = 0; i < Math.min(u.new_tx || 0, 3); i++) ring('tx', 500 + i * 220);
    renderAll();
  }

  function connect() {
    const es = new EventSource('api/events');
    es.addEventListener('state', (e) => {
      S = JSON.parse(e.data); connected = true;
      $('feed').replaceChildren(); addEvents(S.events || [], false);
      if (!S.events || !S.events.length) { const li = document.createElement('li'); li.className = 'muted'; li.textContent = 'listening for packets…'; $('feed').append(li); }
      renderHelp(); renderConsole(); renderAll();
    });
    es.addEventListener('update', (e) => applyUpdate(JSON.parse(e.data)));
    es.addEventListener('open', () => { connected = true; renderStatus(); });
    es.addEventListener('error', () => { connected = false; renderStatus(); });
  }

  // ---------- console ----------
  const history = []; let hIdx = 0;
  function consoleEntry(cmd) {
    const out = $('consoleOut');
    const c = document.createElement('div'); c.className = 'c'; c.textContent = '❯ ' + cmd;
    const pre = document.createElement('pre'); pre.className = 'wait'; pre.textContent = 'waiting for reply…';
    out.append(c, pre); out.scrollTop = out.scrollHeight;
    return pre;
  }
  function setOut(pre, text, cls) { pre.className = cls; pre.textContent = text; $('consoleOut').scrollTop = $('consoleOut').scrollHeight; }
  function confirmSend(cmd) {
    return new Promise((resolve) => {
      const dlg = $('confirmDlg');
      $('confirmCmd').textContent = `'${cmd}'`;
      dlg.returnValue = '';
      dlg.addEventListener('close', () => resolve(dlg.returnValue === 'yes'), { once: true });
      dlg.showModal();
    });
  }
  async function run(cmd, confirm, pre) {
    pre = pre || consoleEntry(cmd);
    let r, j = {};
    try {
      r = await fetch('api/exec', {
        method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'meshtui' },
        body: JSON.stringify({ cmd, confirm }),
      });
      j = await r.json().catch(() => ({}));
    } catch (err) { return setOut(pre, 'network error: ' + err.message, 'err'); }
    if (r.status === 409 && j.needs_confirm) {
      setOut(pre, 'needs confirmation…', 'wait');
      if (await confirmSend(cmd)) return run(cmd, true, pre);
      return setOut(pre, 'cancelled — nothing sent', 'wait');
    }
    if (!r.ok) return setOut(pre, j.error || `HTTP ${r.status}`, 'err');
    setOut(pre, j.out || '(no reply)', '');
  }
  // One command at a time: a second pending confirmation could otherwise be
  // approved by the same click on the shared dialog.
  let busy = false;
  const lock = (on) => { busy = on; $('cmd').disabled = on; $('consoleForm').querySelector('button').disabled = on; };
  $('consoleForm').addEventListener('submit', async (e) => {
    e.preventDefault();
    if (busy) return;
    const cmd = $('cmd').value.trim(); if (!cmd) return;
    history.push(cmd); hIdx = history.length; $('cmd').value = '';
    lock(true);
    try { await run(cmd, false); } finally { lock(false); $('cmd').focus(); }
  });
  $('cmd').addEventListener('keydown', (e) => {
    if (e.key === 'ArrowUp' && hIdx > 0) { hIdx--; $('cmd').value = history[hIdx]; e.preventDefault(); }
    else if (e.key === 'ArrowDown') { hIdx = Math.min(history.length, hIdx + 1); $('cmd').value = history[hIdx] || ''; e.preventDefault(); }
    else if (e.key === 'Escape') $('cmd').blur();
  });

  // ---------- ui wiring ----------
  document.querySelectorAll('.card .info').forEach((b) => b.addEventListener('click', () => {
    const p = b.closest('.card').querySelector('.help'); p.hidden = !p.hidden;
  }));
  const toggleHelp = () => { const d = $('helpDlg'); d.open ? d.close() : d.showModal(); };
  const nextTheme = () => setTheme(THEMES[(THEMES.indexOf(document.documentElement.dataset.theme) + 1) % THEMES.length]);
  $('helpBtn').addEventListener('click', toggleHelp);
  $('themeBtn').addEventListener('click', nextTheme);
  document.addEventListener('keydown', (e) => {
    if (e.target.closest('input, textarea') || e.ctrlKey || e.metaKey || e.altKey) return;
    if (e.key === 'h' || e.key === '?') { e.preventDefault(); toggleHelp(); }
    else if (e.key === 't') nextTheme();
    else if (e.key === ':' && S && S.console) { e.preventDefault(); $('cmd').focus(); }
  });
  new ResizeObserver(() => drawCharts()).observe(document.querySelector('.grid'));

  setTheme(store.get('meshtui-theme') || 'synthwave');
  setInterval(renderTick, 1000);
  renderTick();
  connect();
})();
