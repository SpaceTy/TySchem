/* tyschem – vanilla JS SPA */

// ───────────────────────────────────────────────────────────────
//  API client
// ───────────────────────────────────────────────────────────────
const API = {
  async list(params = {}) {
    const q = new URLSearchParams();
    for (const [k, v] of Object.entries(params)) {
      if (v !== '' && v !== null && v !== undefined) q.set(k, v);
    }
    const r = await fetch('/api/schematics?' + q);
    if (!r.ok) throw new Error(await r.text());
    return r.json();
  },
  async get(id) {
    const r = await fetch('/api/schematics/' + id);
    if (!r.ok) throw new Error('not found');
    return r.json();
  },
  async upload(formData, onProgress) {
    return new Promise((resolve, reject) => {
      const xhr = new XMLHttpRequest();
      xhr.open('POST', '/api/schematics');
      xhr.onload = () => {
        if (xhr.status >= 200 && xhr.status < 300) resolve(JSON.parse(xhr.responseText));
        else {
          if (xhr.status === 401 || xhr.status === 403) onUnauthorized();
          let msg = xhr.responseText || 'upload failed';
          try { msg = JSON.parse(xhr.responseText).error || msg; } catch {}
          reject(new Error(msg));
        }
      };
      xhr.onerror = () => reject(new Error('network error'));
      if (onProgress) xhr.upload.onprogress = e => { if (e.lengthComputable) onProgress(e.loaded / e.total); };
      xhr.send(formData);
    });
  },
  async update(id, body) {
    const r = await fetch('/api/schematics/' + id, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (r.status === 401 || r.status === 403) { onUnauthorized(); throw new Error('not allowed'); }
    if (!r.ok) throw new Error(await r.text());
    return r.json();
  },
  async remove(id) {
    const r = await fetch('/api/schematics/' + id, { method: 'DELETE' });
    if (r.status === 401 || r.status === 403) { onUnauthorized(); throw new Error('not allowed'); }
    if (!r.ok) throw new Error('delete failed');
  },
  downloadUrl(id) { return '/api/schematics/' + id + '/file'; },
  // One call for both login and sign-up: the server decides based on whether
  // the username already exists.
  async enter(username, password) {
    const r = await fetch('/api/auth', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    });
    const data = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(data.error || 'authentication failed');
    return data;
  },
  async logout() {
    await fetch('/api/auth/logout', { method: 'POST' });
  },
  async me() {
    const r = await fetch('/api/auth/me');
    if (!r.ok) return null;
    return (await r.json()).user;
  },
};

// ───────────────────────────────────────────────────────────────
//  Auth state
// ───────────────────────────────────────────────────────────────
let currentUser = null;

// Called when an authenticated request is rejected; resets local state.
function onUnauthorized() {
  if (!currentUser) return;
  currentUser = null;
  renderAuthNav();
  toast('Please sign in to continue', 'error');
}

async function refreshAuth() {
  try { currentUser = await API.me(); } catch { currentUser = null; }
  renderAuthNav();
  return currentUser;
}

// Whether the signed-in user may edit/delete a schematic.
// Legacy uploads have no owner and are manageable by any signed-in user.
function canManage(meta) {
  return !!currentUser && (!meta.ownerId || meta.ownerId === currentUser.id);
}

function renderAuthNav() {
  const el = document.getElementById('site-user');
  if (!el) return;
  if (currentUser) {
    const initial = (currentUser.username || '?').charAt(0);
    el.innerHTML = `
      <div class="profile-menu" id="profile-menu">
        <button class="profile-trigger" id="profile-trigger" type="button" aria-haspopup="true" aria-expanded="false">
          <span class="avatar">${esc(initial)}</span>
          <span class="profile-name">${esc(currentUser.username)}</span>
          <svg class="profile-chevron" fill="none" viewBox="0 0 24 24" stroke="currentColor" aria-hidden="true">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M19 9l-7 7-7-7" />
          </svg>
        </button>
        <div class="profile-dropdown" id="profile-dropdown" hidden>
          <div class="profile-dropdown-head">
            <p class="profile-dropdown-label">Signed in as</p>
            <p class="profile-dropdown-user">${esc(currentUser.username)}</p>
          </div>
          <div class="profile-dropdown-body">
            <button class="profile-logout" id="logout-btn" type="button"><span>&#10132;</span> Logout</button>
          </div>
        </div>
      </div>`;
    const trigger = document.getElementById('profile-trigger');
    const dropdown = document.getElementById('profile-dropdown');
    trigger.addEventListener('click', () => {
      const open = !dropdown.hidden;
      dropdown.hidden = open;
      trigger.classList.toggle('open', !open);
      trigger.setAttribute('aria-expanded', String(!open));
    });
    document.getElementById('logout-btn').addEventListener('click', doLogout);
  } else {
    el.innerHTML = `
      <a href="#/login" class="button button-primary button-sm">Sign in</a>`;
  }
}

function closeProfileMenus(e) {
  document.querySelectorAll('.profile-menu').forEach(root => {
    if (e && e.target && root.contains(e.target)) return;
    const dropdown = root.querySelector('.profile-dropdown');
    const trigger = root.querySelector('.profile-trigger');
    if (dropdown) dropdown.hidden = true;
    if (trigger) {
      trigger.classList.remove('open');
      trigger.setAttribute('aria-expanded', 'false');
    }
  });
}
document.addEventListener('click', closeProfileMenus);
document.addEventListener('keydown', e => { if (e.key === 'Escape') closeProfileMenus(); });

async function doLogout() {
  try { await API.logout(); } catch {}
  currentUser = null;
  renderAuthNav();
  toast('Signed out');
  if (location.hash === '#/' || location.hash === '') route();
  else location.hash = '#/';
}

// ───────────────────────────────────────────────────────────────
//  Toast
// ───────────────────────────────────────────────────────────────
function toast(msg, type = 'success') {
  const el = document.createElement('div');
  el.className = 'toast ' + type;
  el.textContent = msg;
  document.getElementById('toast-container').appendChild(el);
  setTimeout(() => { el.classList.add('toast-exit'); el.addEventListener('animationend', () => el.remove()); }, 3500);
}

// ───────────────────────────────────────────────────────────────
//  Anchored popovers (grow out of the button that opened them)
// ───────────────────────────────────────────────────────────────
function positionPopover(pop, anchor) {
  const rail = anchor.closest('.detail-rail');
  const a = anchor.getBoundingClientRect();
  const o = rail ? rail.getBoundingClientRect() : a;
  const pw = pop.offsetWidth;
  const ph = pop.offsetHeight;
  const gap = 12;
  let side = 'right';
  let left = a.left - pw - gap;
  if (left < 8) { left = a.right + gap; side = 'left'; }
  if (left + pw > window.innerWidth - 8) left = window.innerWidth - pw - 8;
  let top = o.top;
  top = Math.max(8, Math.min(top, window.innerHeight - ph - 8));
  const caretY = a.top + a.height / 2 - top;
  pop.style.left = left + 'px';
  pop.style.top = top + 'px';
  pop.style.setProperty('--caret-y', caretY + 'px');
  pop.dataset.side = side;
  pop.style.transformOrigin = `${side === 'right' ? '100%' : '0'} 0`;
}

function createPopover(anchor, contentHtml, onClose, extraClass) {
  if (anchor.__popover) anchor.__popover.close();
  const pop = document.createElement('div');
  pop.className = 'action-popover' + (extraClass ? ' ' + extraClass : '');
  pop.innerHTML = contentHtml;
  document.body.appendChild(pop);
  positionPopover(pop, anchor);

  let closed = false;
  function close() {
    if (closed) return;
    closed = true;
    if (anchor.__popover === api) anchor.__popover = null;
    pop.classList.remove('open');
    document.removeEventListener('pointerdown', onDocDown, true);
    document.removeEventListener('keydown', onKey);
    window.removeEventListener('resize', reposition);
    window.removeEventListener('scroll', reposition, true);
    const done = () => pop.remove();
    pop.addEventListener('transitionend', done, { once: true });
    setTimeout(done, 260);
    onClose?.();
  }
  function onDocDown(e) {
    if (pop.contains(e.target) || anchor.contains(e.target)) return;
    close();
  }
  function onKey(e) { if (e.key === 'Escape') close(); }
  function reposition() { positionPopover(pop, anchor); }

  document.addEventListener('pointerdown', onDocDown, true);
  document.addEventListener('keydown', onKey);
  window.addEventListener('resize', reposition);
  window.addEventListener('scroll', reposition, true);
  requestAnimationFrame(() => pop.classList.add('open'));
  const api = { pop, close };
  anchor.__popover = api;
  return api;
}

// Confirm dialog anchored to a button; returns Promise<boolean>.
function confirm(anchor, title, message, confirmLabel = 'Delete') {
  return new Promise(resolve => {
    let done = false;
    const finish = v => { if (!done) { done = true; resolve(v); } };
    const { pop, close } = createPopover(anchor, `
      <h3 class="dialog-title">${esc(title)}</h3>
      <p class="dialog-message">${esc(message)}</p>
      <div class="dialog-actions">
        <button type="button" class="button button-secondary" data-act="cancel">Cancel</button>
        <button type="button" class="button button-danger" data-act="ok">${esc(confirmLabel)}</button>
      </div>`, () => finish(false));
    pop.querySelector('[data-act="cancel"]').addEventListener('click', () => { close(); finish(false); });
    pop.querySelector('[data-act="ok"]').addEventListener('click', () => { finish(true); close(); });
  });
}

// ───────────────────────────────────────────────────────────────
//  Utilities
// ───────────────────────────────────────────────────────────────
const view = () => document.getElementById('view');
function esc(s) { const d = document.createElement('div'); d.textContent = s; return d.innerHTML; }

function fmtBytes(n) {
  if (n < 1024) return n + ' B';
  if (n < 1048576) return (n / 1024).toFixed(1) + ' KiB';
  return (n / 1048576).toFixed(1) + ' MiB';
}
function fmtDate(s) {
  const d = new Date(s);
  return d.toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' });
}
function fmtDateTime(s) {
  const d = new Date(s);
  return d.toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}

// ───────────────────────────────────────────────────────────────
//  3D preview (Three.js + Lodestone)
// ───────────────────────────────────────────────────────────────
const LODESTONE = window.Lodestone || null;
const THREE_LIB = window.THREE || null;
const PACK_BASE = new URL('vendor/default-pack/', document.baseURI).href;

// Twilight-blue environment tuned to the dark glass theme.
const VIEW_ENV = {
  shadow: { enabled: true, intensity: 0.35 },
  postProcess: { enabled: false },
  sky: {
    zenithColor: [0.03, 0.05, 0.10],
    horizonColor: [0.08, 0.13, 0.235],
    groundColor: [0.015, 0.02, 0.035],
    sunGlowColor: [0.35, 0.5, 0.9],
    sunGlowIntensity: 0.18,
  },
  disc: { coreIntensity: 0, glowIntensity: 0 },
  fog: { density: 0 },
};

let _previewsOk = null;
function previewsSupported() {
  if (_previewsOk !== null) return _previewsOk;
  if (!LODESTONE || !THREE_LIB) return (_previewsOk = false);
  try {
    const c = document.createElement('canvas');
    _previewsOk = !!(c.getContext('webgl2') || c.getContext('webgl') || c.getContext('experimental-webgl'));
  } catch { _previewsOk = false; }
  return _previewsOk;
}

// Load (and memoize) the bundled resource pack.
let _packPromise = null;
function loadPack() {
  if (!_packPromise) {
    _packPromise = LODESTONE.loadDefaultPackResources({ baseUrl: PACK_BASE })
      .then(p => p.resources)
      .catch(err => { _packPromise = null; throw err; });
  }
  return _packPromise;
}

// Parse (and memoize) a schematic into a Lodestone Structure.
const _structureCache = new Map();
function loadStructure(id) {
  if (!_structureCache.has(id)) {
    const p = fetch(API.downloadUrl(id))
      .then(r => { if (!r.ok) throw new Error('failed to fetch schematic'); return r.arrayBuffer(); })
      .then(buf => LODESTONE.LitematicLoader.load(new Uint8Array(buf)))
      .catch(err => { _structureCache.delete(id); throw err; });
    _structureCache.set(id, p);
  }
  return _structureCache.get(id);
}

// Camera placement orbiting a structure's centre.
function orbitCamera(structure, theta, phi, margin) {
  const [sx, sy, sz] = structure.getSize();
  const target = [sx / 2, sy / 2, sz / 2];
  const radius = Math.max(sx, sy, sz, 1) * margin;
  const sinPhi = Math.sin(phi);
  return {
    position: [
      target[0] + radius * Math.sin(theta) * sinPhi,
      target[1] + radius * Math.cos(phi),
      target[2] + radius * Math.cos(theta) * sinPhi,
    ],
    target,
    up: [0, 1, 0],
    radius,
  };
}

// ── Card thumbnails ─────────────────────────────────────────
const _thumbCache = new Map();     // id -> dataURL
const _thumbPending = new Set();   // ids currently queued/rendering
const _thumbQueue = [];
let _thumbRunning = false;

function requestThumbnail(id, imgEl) {
  if (!id || !imgEl) return;
  if (_thumbCache.has(id)) { setThumb(imgEl, _thumbCache.get(id)); return; }
  if (_thumbPending.has(id)) return;
  _thumbPending.add(id);
  _thumbQueue.push({ id, imgEl });
  pumpThumbQueue();
}

async function pumpThumbQueue() {
  if (_thumbRunning) return;
  _thumbRunning = true;
  while (_thumbQueue.length) {
    const job = _thumbQueue.shift();
    try {
      const url = _thumbCache.get(job.id) || await renderThumbnail(job.id);
      _thumbCache.set(job.id, url);
      if (job.imgEl.isConnected) setThumb(job.imgEl, url);
    } catch (err) {
      console.warn('[tyschem] thumbnail failed for', job.id, err);
      job.imgEl.closest('.card-thumb')?.classList.add('thumb-error');
    } finally {
      _thumbPending.delete(job.id);
    }
  }
  _thumbRunning = false;
}

function setThumb(imgEl, url) {
  imgEl.src = url;
  imgEl.closest('.card-thumb')?.classList.add('thumb-ready');
}

async function renderThumbnail(id) {
  const [structure, resources] = await Promise.all([loadStructure(id), loadPack()]);
  const width = 280;
  const height = 280;   // square to match the compact .card-thumb
  const canvas = document.createElement('canvas');
  canvas.width = width;
  canvas.height = height;
  const renderer = new LODESTONE.ThreeStructureRenderer(canvas, structure, resources, {
    antialias: true,
    preserveDrawingBuffer: true,
    sunlight: VIEW_ENV,
  });
  try {
    renderer.setViewport(0, 0, width, height, 1);
    renderer.setCamera(orbitCamera(structure, Math.PI * 0.28, Math.PI * 0.36, 1.8));
    renderer.drawStructure();
    return canvas.toDataURL('image/png');
  } finally {
    renderer.dispose();
  }
}

// Only queue thumbnails once their card scrolls near the viewport.
let _thumbObserver = null;
function thumbObserver() {
  if (!_thumbObserver) {
    _thumbObserver = new IntersectionObserver(entries => {
      for (const e of entries) {
        if (!e.isIntersecting) continue;
        _thumbObserver.unobserve(e.target);
        const img = e.target.querySelector('.card-thumb-img');
        requestThumbnail(e.target.dataset.id, img);
      }
    }, { rootMargin: '250px' });
  }
  return _thumbObserver;
}

// ── Interactive detail viewer ───────────────────────────────
async function mountDetailViewer(container, id) {
  if (!previewsSupported()) {
    container.innerHTML = '<div class="viewer-loading">3D preview not supported in this browser.</div>';
    return;
  }
  let structure, resources;
  try {
    [structure, resources] = await Promise.all([loadStructure(id), loadPack()]);
  } catch (err) {
    console.warn('[tyschem] preview failed', err);
    container.innerHTML = '<div class="viewer-loading">Could not render preview.</div>';
    return;
  }
  if (!container.isConnected) return;

  const canvas = document.createElement('canvas');
  canvas.className = 'viewer-canvas';
  const loading = container.querySelector('.viewer-loading');

  const state = {
    theta: Math.PI * 0.28,
    phi: Math.PI * 0.36,
    radius: Math.max(...structure.getSize(), 1) * 1.9,
    target: null,
    dragging: false,
    autoRotate: true,
    lastX: 0,
    lastY: 0,
    disposed: false,
    raf: 0,
  };
  const [sx, sy, sz] = structure.getSize();
  state.target = [sx / 2, sy / 2, sz / 2];

  let renderer;
  try {
    renderer = new LODESTONE.ThreeStructureRenderer(canvas, structure, resources, {
      antialias: true,
      asyncBuild: true,
      sunlight: VIEW_ENV,
    });
  } catch (err) {
    console.warn('[tyschem] renderer init failed', err);
    container.innerHTML = '<div class="viewer-loading">Could not initialise 3D renderer.</div>';
    return;
  }

  container.insertBefore(canvas, loading);

  function resize() {
    if (state.disposed) return;
    const w = Math.max(container.clientWidth, 1);
    const h = Math.max(container.clientHeight, 180);
    renderer.setViewport(0, 0, w, h, Math.min(window.devicePixelRatio || 1, 2));
  }
  resize();

  function updateCamera() {
    const sinPhi = Math.sin(state.phi);
    renderer.setCamera({
      position: [
        state.target[0] + state.radius * Math.sin(state.theta) * sinPhi,
        state.target[1] + state.radius * Math.cos(state.phi),
        state.target[2] + state.radius * Math.cos(state.theta) * sinPhi,
      ],
      target: state.target,
      up: [0, 1, 0],
    });
  }

  let firstDraw = true;
  function renderFrame() {
    updateCamera();
    renderer.drawStructure();
    if (firstDraw) { firstDraw = false; loading?.remove(); }
  }
  function loop() {
    if (state.disposed) return;
    if (state.autoRotate && !state.dragging) state.theta += 0.0035;
    renderFrame();
    state.raf = requestAnimationFrame(loop);
  }
  renderFrame();               // paint immediately, then animate
  state.raf = requestAnimationFrame(loop);

  const onDown = e => {
    state.dragging = true;
    state.autoRotate = false;
    state.lastX = e.clientX;
    state.lastY = e.clientY;
    try { canvas.setPointerCapture(e.pointerId); } catch {}
    canvas.classList.add('grabbing');
  };
  const onMove = e => {
    if (!state.dragging) return;
    const dx = e.clientX - state.lastX;
    const dy = e.clientY - state.lastY;
    state.lastX = e.clientX;
    state.lastY = e.clientY;
    state.theta -= dx * 0.008;
    state.phi = Math.min(Math.PI - 0.12, Math.max(0.12, state.phi - dy * 0.008));
  };
  const onUp = e => {
    state.dragging = false;
    canvas.classList.remove('grabbing');
    try { canvas.releasePointerCapture(e.pointerId); } catch {}
  };
  const onWheel = e => {
    e.preventDefault();
    state.autoRotate = false;
    state.radius = Math.min(state.radius * 4, Math.max(state.radius * 0.25, state.radius * Math.exp(e.deltaY * 0.0012)));
  };

  canvas.addEventListener('pointerdown', onDown);
  canvas.addEventListener('pointermove', onMove);
  canvas.addEventListener('pointerup', onUp);
  canvas.addEventListener('pointercancel', onUp);
  canvas.addEventListener('wheel', onWheel, { passive: false });

  let resizeObserver = null;
  if (window.ResizeObserver) {
    resizeObserver = new ResizeObserver(resize);
    resizeObserver.observe(container);
  }

  return function cleanup() {
    state.disposed = true;
    cancelAnimationFrame(state.raf);
    resizeObserver?.disconnect();
    canvas.removeEventListener('pointerdown', onDown);
    canvas.removeEventListener('pointermove', onMove);
    canvas.removeEventListener('pointerup', onUp);
    canvas.removeEventListener('pointercancel', onUp);
    canvas.removeEventListener('wheel', onWheel);
    try { renderer.dispose(); } catch {}
  };
}

// ───────────────────────────────────────────────────────────────
//  Router
// ───────────────────────────────────────────────────────────────
const $view = document.getElementById('view');
let _currentAbort = null;          // AbortController to cancel in-flight fetches
let _viewCleanup = null;           // teardown for the active view (e.g. 3D renderer)

function setViewCleanup(fn) {
  if (_viewCleanup && _viewCleanup !== fn) { try { _viewCleanup(); } catch {} }
  _viewCleanup = fn;
}

function route() {
  if (_currentAbort) _currentAbort.abort();
  _currentAbort = new AbortController();
  if (_viewCleanup) { try { _viewCleanup(); } catch {} _viewCleanup = null; }
  $view.classList.remove('view-wide');
  document.body.classList.remove('detail-view');
  const navSub = document.getElementById('nav-sub');
  if (navSub) { navSub.hidden = true; }
  const navSubText = document.getElementById('nav-sub-text');
  if (navSubText) navSubText.textContent = '';

  const hash = location.hash.slice(1) || '/';
  const parts = hash.split('/').filter(Boolean);

  // highlight nav
  document.querySelectorAll('.nav-link').forEach(a => a.classList.remove('active'));
  if (parts[0] === 'upload') document.querySelector('[data-nav="upload"]')?.classList.add('active');
  else if (parts[0] === 'mine') document.querySelector('[data-nav="mine"]')?.classList.add('active');
  else if (parts[0] !== 'login' && parts[0] !== 'register') document.querySelector('[data-nav="schematics"]')?.classList.add('active');

  if (parts.length === 0 || (parts.length === 1 && parts[0] === '')) { renderList({ owner: '' }); return; }
  if (parts[0] === 'login' || parts[0] === 'register') { renderAuth(); return; }
  if (parts[0] === 'mine') { renderList({ owner: 'me', offset: 0 }); return; }
  if (parts[0] === 'upload') { renderUpload(); return; }
  if (parts[0] === 'schematic' && parts[1]) { renderDetail(parts[1]); return; }
  $view.innerHTML = '<div class="panel fade-in"><h2 class="page-title">Not found</h2></div>';
}
window.addEventListener('hashchange', route);

// ───────────────────────────────────────────────────────────────
//  List / browse view
// ───────────────────────────────────────────────────────────────
let _listState = { q: '', sort: 'uploadDate', order: 'desc', limit: 20, offset: 0, owner: '' };

async function renderList(overrides = {}) {
  Object.assign(_listState, overrides);
  const s = _listState;
  const mine = s.owner === 'me';

  if (mine && !currentUser) {
    $view.innerHTML = `
      <div class="fade-in">
        <h2 class="page-title">My Uploads</h2>
        <div class="panel empty-state">
          <div class="empty-icon">&#128274;</div>
          <p class="empty-text">You need an account to see your uploads.</p>
          <p class="empty-hint"><a href="#/login">Sign in</a> to continue.</p>
        </div>
      </div>`;
    return;
  }

  const sortLabel = { uploadDate: 'Upload date', updatedDate: 'Updated date', name: 'Name', size: 'Size' }[s.sort] || s.sort;
  let html = `
    <div class="browse-view">
      <h2 class="page-title">${mine ? 'My Uploads' : 'Schematics'}</h2>
      <div class="browse-layout">
        <aside class="browse-rail">
          <button type="button" class="rail-btn ${s.q ? 'active' : ''}" id="search-btn" title="Search" aria-label="Search">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="11" cy="11" r="7"/><path d="M21 21l-4.3-4.3"/></svg>
          </button>
          <button type="button" class="rail-btn" id="sort-btn" title="Sort: ${sortLabel}" aria-label="Change sort">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 6h18M6 12h12M10 18h4"/></svg>
          </button>
          <button type="button" class="rail-btn ${s.order === 'asc' ? 'asc' : 'desc'}" id="order-btn" title="${s.order === 'desc' ? 'Newest first' : 'Oldest first'}" aria-label="Toggle sort order">
            <svg class="rail-order-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 5v14M6 13l6 6 6-6"/></svg>
          </button>
          <div class="browse-rail-pagination" id="rail-pagination"></div>
        </aside>
        <div class="browse-main fade-in">
          <div id="list-body"><div class="empty-state"><div class="empty-icon">...</div><p class="empty-text">Loading...</p></div></div>
        </div>
      </div>
    </div>`;
  $view.classList.add('view-wide');
  $view.innerHTML = html;

  const withPopover = (id, open) => {
    const btn = document.getElementById(id);
    btn.addEventListener('click', e => {
      const el = e.currentTarget;
      if (el.__popover) { el.__popover.close(); return; }
      open(el);
    });
  };
  withPopover('search-btn', openSearchPopover);
  withPopover('sort-btn', openSortPopover);
  withPopover('order-btn', openOrderPopover);

  await loadList();
}

async function loadList() {
  const s = _listState;
  try {
    const data = await API.list({ q: s.q, sort: s.sort, order: s.order, limit: s.limit, offset: s.offset, owner: s.owner });
    renderListBody(data);
  } catch (err) {
    if (err.name === 'AbortError') return;
    document.getElementById('list-body').innerHTML = '<div class="empty-state"><p class="empty-text" style="color:#ef4444">Failed to load schematics.</p></div>';
  }
}

// ── Browse rail popovers (styled like the detail-page popovers) ─
function openSearchPopover(anchor) {
  const { pop } = createPopover(anchor, `
    <h3 class="dialog-title">Search</h3>
    <input class="input" id="pop-search-input" placeholder="Search schematics..." value="${esc(_listState.q)}" />`, null, 'action-popover-menu');
  const input = pop.querySelector('#pop-search-input');
  let timer;
  input.addEventListener('input', () => {
    clearTimeout(timer);
    timer = setTimeout(() => {
      _listState.q = input.value;
      _listState.offset = 0;
      document.getElementById('search-btn')?.classList.toggle('active', !!_listState.q);
      loadList();
    }, 280);
  });
  input.focus();
  input.setSelectionRange(input.value.length, input.value.length);
}

function openSortPopover(anchor) {
  const options = [['uploadDate', 'Upload date'], ['updatedDate', 'Updated date'], ['name', 'Name'], ['size', 'Size']];
  const { pop, close } = createPopover(anchor, `
    <h3 class="dialog-title">Sort by</h3>
    <div class="popover-menu">
      ${options.map(([v, l]) => `<button type="button" class="popover-item ${_listState.sort === v ? 'active' : ''}" data-value="${v}">${l}</button>`).join('')}
    </div>`, null, 'action-popover-menu');
  pop.querySelectorAll('.popover-item').forEach(item => item.addEventListener('click', () => {
    close();
    renderList({ sort: item.dataset.value, offset: 0 });
  }));
}

function openOrderPopover(anchor) {
  const { pop, close } = createPopover(anchor, `
    <h3 class="dialog-title">Order</h3>
    <div class="popover-menu">
      <button type="button" class="popover-item ${_listState.order === 'desc' ? 'active' : ''}" data-value="desc">Newest first</button>
      <button type="button" class="popover-item ${_listState.order === 'asc' ? 'active' : ''}" data-value="asc">Oldest first</button>
    </div>`, null, 'action-popover-menu');
  pop.querySelectorAll('.popover-item').forEach(item => item.addEventListener('click', () => {
    close();
    renderList({ order: item.dataset.value, offset: 0 });
  }));
}

function renderListBody(data) {
  const el = document.getElementById('list-body');
  if (!data.items || data.items.length === 0) {
    const mine = _listState.owner === 'me';
    el.innerHTML = `
      <div class="empty-state panel">
        <div class="empty-icon">&#128196;</div>
        <p class="empty-text">${_listState.q ? 'No schematics match your search.' : (mine ? "You haven't uploaded any schematics yet." : 'No schematics uploaded yet.')}</p>
        ${!_listState.q ? '<p class="empty-hint"><a href="#/upload">Upload your first schematic</a></p>' : ''}
      </div>`;
    return;
  }

  let grid = '<div class="schematics-grid">';
  for (const m of data.items) {
    grid += `
      <div class="schematic-card" data-id="${m.id}" data-href="#/schematic/${m.id}" tabindex="0">
        <div class="card-head">
          <div class="card-thumb">
            <img class="card-thumb-img" alt="Preview of ${esc(m.name)}" />
            <div class="card-thumb-loading"></div>
          </div>
          <div class="card-title-wrap">
            <div class="card-name">${esc(m.name)}</div>
            <div class="card-desc">${esc(m.description || m.fileName)}</div>
          </div>
        </div>
        <div class="card-meta">
          <span class="card-size">${fmtBytes(m.size)}</span>
          <span class="card-owner">${esc(m.ownerName || 'anonymous')}</span>
          <span class="card-date">${fmtDate(m.uploadDate)}</span>
          <a href="${API.downloadUrl(m.id)}" class="button button-secondary button-sm card-download" download title="Download ${esc(m.fileName)}" onclick="event.stopPropagation()">Download</a>
        </div>
      </div>`;
  }
  grid += '</div>';

  // pagination lives in the fixed right-hand rail
  const totalPages = Math.ceil(data.total / _listState.limit) || 1;
  const curPage = Math.floor(data.offset / _listState.limit) + 1;
  const hasPrev = data.offset > 0;
  const hasNext = data.offset + data.items.length < data.total;
  const pagination = document.getElementById('rail-pagination');
  if (pagination) {
    pagination.innerHTML = `
      <div class="pagination">
        <button class="rail-btn" id="pg-prev" title="Previous page" aria-label="Previous page" ${!hasPrev ? 'disabled' : ''}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M18 15l-6-6-6 6"/></svg>
        </button>
        <span class="page-info" title="${data.total} total">${curPage}/${totalPages}</span>
        <button class="rail-btn" id="pg-next" title="Next page" aria-label="Next page" ${!hasNext ? 'disabled' : ''}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg>
        </button>
      </div>`;
  }

  el.innerHTML = grid;

  el.querySelectorAll('.schematic-card').forEach(card => {
    const go = () => { location.hash = card.dataset.href; };
    card.addEventListener('click', go);
    card.addEventListener('keydown', e => { if (e.key === 'Enter') { e.preventDefault(); go(); } });
  });

  // Kick off lazy 3D thumbnails.
  if (previewsSupported()) {
    const obs = thumbObserver();
    el.querySelectorAll('.schematic-card').forEach(card => obs.observe(card));
  } else {
    el.querySelectorAll('.card-thumb').forEach(t => t.classList.add('thumb-error'));
  }

  document.getElementById('pg-prev')?.addEventListener('click', () => {
    _listState.offset = Math.max(0, _listState.offset - _listState.limit);
    loadList();
  });
  document.getElementById('pg-next')?.addEventListener('click', () => {
    _listState.offset = _listState.offset + _listState.limit;
    loadList();
  });
}

// ───────────────────────────────────────────────────────────────
//  Upload / create view
// ───────────────────────────────────────────────────────────────
function renderUpload() {
  if (!currentUser) {
    $view.innerHTML = `
      <div class="fade-in">
        <h2 class="page-title">Upload Schematic</h2>
        <div class="panel empty-state">
          <div class="empty-icon">&#128274;</div>
          <p class="empty-text">You need an account to upload schematics.</p>
          <p class="empty-hint"><a href="#/login">Sign in</a> to continue.</p>
        </div>
      </div>`;
    return;
  }

  $view.innerHTML = `
    <div class="fade-in">
      <h2 class="page-title">Upload Schematic</h2>
      <div class="panel">
        <form id="upload-form" autocomplete="off">
          <div class="form-group">
            <div class="upload-drop" id="drop-zone">
              <div class="upload-drop-icon">&#128228;</div>
              <div class="upload-drop-text" id="drop-text">Drag &amp; drop a <strong>.litematic</strong> file here</div>
              <div class="upload-drop-hint">or click to browse &middot; max 50 MiB</div>
              <input type="file" id="file-input" accept=".litematic,.litematica" hidden />
            </div>
          </div>
          <div class="form-group">
            <label class="label" for="upload-name">Name</label>
            <input class="input" id="upload-name" placeholder="e.g. Starter House" maxlength="200" />
            <div class="form-hint">Defaults to the filename if left empty.</div>
          </div>
          <div class="form-group">
            <label class="label" for="upload-desc">Description</label>
            <textarea class="textarea" id="upload-desc" placeholder="Optional description of the schematic..." maxlength="5000"></textarea>
          </div>
          <div id="upload-progress-wrap" style="display:none">
            <div class="upload-progress"><div class="upload-progress-bar" id="upload-bar" style="width:0%"></div></div>
          </div>
          <div id="upload-error" style="display:none;margin-bottom:1rem;padding:.75rem 1rem;border-radius:.6rem;font-size:.82rem;background:rgba(127,29,29,.92);border:1px solid rgba(248,113,113,.3);color:#fecaca"></div>
          <div style="display:flex;gap:.6rem;margin-top:1rem">
            <button type="submit" class="button button-primary" id="upload-btn" disabled>Upload</button>
            <a href="#/" class="button button-secondary">Cancel</a>
          </div>
        </form>
      </div>
    </div>`;

  const drop   = document.getElementById('drop-zone');
  const input  = document.getElementById('file-input');
  const btn    = document.getElementById('upload-btn');
  const text   = document.getElementById('drop-text');
  let chosenFile = null;

  function setFile(f) {
    chosenFile = f;
    btn.disabled = !f;
    if (f) {
      drop.classList.add('has-file');
      text.innerHTML = `<strong>${esc(f.name)}</strong> &middot; ${fmtBytes(f.size)}`;
    } else {
      drop.classList.remove('has-file');
      text.innerHTML = 'Drag &amp; drop a <strong>.litematic</strong> file here';
    }
  }

  drop.addEventListener('click', () => input.click());
  input.addEventListener('change', () => { if (input.files[0]) setFile(input.files[0]); });
  drop.addEventListener('dragover', e => { e.preventDefault(); drop.classList.add('drag-over'); });
  drop.addEventListener('dragleave', () => drop.classList.remove('drag-over'));
  drop.addEventListener('drop', e => { e.preventDefault(); drop.classList.remove('drag-over'); if (e.dataTransfer.files[0]) setFile(e.dataTransfer.files[0]); });

  document.getElementById('upload-form').addEventListener('submit', async e => {
    e.preventDefault();
    if (!chosenFile) return;
    const errEl = document.getElementById('upload-error');
    errEl.style.display = 'none';
    btn.disabled = true;
    btn.textContent = 'Uploading...';
    document.getElementById('upload-progress-wrap').style.display = '';
    const bar = document.getElementById('upload-bar');

    const fd = new FormData();
    fd.append('file', chosenFile);
    fd.append('name', document.getElementById('upload-name').value.trim());
    fd.append('description', document.getElementById('upload-desc').value.trim());

    try {
      const meta = await API.upload(fd, p => { bar.style.width = Math.round(p * 100) + '%'; });
      toast('Schematic uploaded');
      location.hash = '#/schematic/' + meta.id;
    } catch (err) {
      errEl.textContent = err.message || 'Upload failed';
      errEl.style.display = '';
      btn.disabled = false;
      btn.textContent = 'Upload';
      document.getElementById('upload-progress-wrap').style.display = 'none';
    }
  });
}

// ───────────────────────────────────────────────────────────────
//  Detail view
// ───────────────────────────────────────────────────────────────
async function renderDetail(id) {
  $view.innerHTML = '<div class="panel fade-in"><div class="empty-state"><p class="empty-text">Loading...</p></div></div>';

  let meta;
  try { meta = await API.get(id); } catch {
    $view.innerHTML = '<div class="panel fade-in"><h2 class="page-title">Schematic not found</h2><a href="#/" class="button button-secondary">Back to list</a></div>';
    return;
  }

  const dlUrl    = API.downloadUrl(id);
  const owner    = canManage(meta);

  $view.classList.add('view-wide');
  document.body.classList.add('detail-view');
  const navSub = document.getElementById('nav-sub');
  if (navSub) {
    const navSubText = document.getElementById('nav-sub-text');
    if (navSubText) navSubText.textContent = meta.name;
    navSub.hidden = false;
  }
  $view.innerHTML = `
    <div class="fade-in detail-layout">
      <div class="panel detail-info">
        <h2 class="page-title detail-name">${esc(meta.name)}</h2>
        ${meta.description ? '<p class="detail-description">' + esc(meta.description) + '</p>' : ''}
        <div class="meta-grid">
          <div class="meta-item"><span class="meta-label">Uploader</span><span class="meta-value">${esc(meta.ownerName || 'anonymous')}</span></div>
          <div class="meta-item"><span class="meta-label">File Size</span><span class="meta-value">${fmtBytes(meta.size)}</span></div>
          <div class="meta-item"><span class="meta-label">Uploaded</span><span class="meta-value">${fmtDateTime(meta.uploadDate)}</span></div>
        </div>
        <details class="meta-advanced">
          <summary>Advanced</summary>
          <div class="meta-grid">
            <div class="meta-item"><span class="meta-label">ID</span><span class="meta-value">${esc(meta.id)}</span></div>
            <div class="meta-item"><span class="meta-label">Original File</span><span class="meta-value">${esc(meta.fileName)}</span></div>
            <div class="meta-item"><span class="meta-label">Content Type</span><span class="meta-value">${esc(meta.contentType)}</span></div>
            <div class="meta-item"><span class="meta-label">Last Updated</span><span class="meta-value">${fmtDateTime(meta.updatedDate)}</span></div>
          </div>
        </details>
      </div>
      <div class="detail-stage">
        <aside class="detail-rail" aria-label="Actions">
          <a href="${dlUrl}" class="rail-btn" download title="Download" aria-label="Download">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 3v12"/><path d="M7 10l5 5 5-5"/><path d="M5 21h14"/></svg>
          </a>
          ${owner ? `<button type="button" class="rail-btn" id="detail-edit" title="Edit" aria-label="Edit">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 20h9"/><path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z"/></svg>
          </button>
          <button type="button" class="rail-btn rail-btn-danger" id="detail-delete" title="Delete" aria-label="Delete">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 6h18"/><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/><path d="M10 11v6M14 11v6"/></svg>
          </button>` : ''}
        </aside>
        <div class="panel viewer-panel">
          <div class="viewer-container" id="detail-viewer">
            <div class="viewer-loading">Loading 3D preview&hellip;</div>
          </div>
        </div>
        <div class="viewer-hint">Drag to rotate &middot; scroll to zoom</div>
      </div>
    </div>`;

  document.getElementById('detail-edit')?.addEventListener('click', e => {
    const btn = e.currentTarget;
    if (btn.__popover) { btn.__popover.close(); return; }
    openEditPopover(btn, meta, id);
  });

  document.getElementById('detail-delete')?.addEventListener('click', async e => {
    const btn = e.currentTarget;
    if (btn.__popover) { btn.__popover.close(); return; }
    const ok = await confirm(btn, 'Delete Schematic', `Are you sure you want to delete "${meta.name}"? This cannot be undone.`);
    if (!ok) return;
    try {
      await API.remove(id);
      toast('Schematic deleted');
      location.hash = '#/';
    } catch { toast('Failed to delete', 'error'); }
  });

  // Mount the interactive 3D viewer (cleaned up on route change).
  const viewerEl = document.getElementById('detail-viewer');
  mountDetailViewer(viewerEl, id).then(cleanup => { if (cleanup) setViewCleanup(cleanup); });
}

// ───────────────────────────────────────────────────────────────
//  Edit popover (anchored to the edit button)
// ───────────────────────────────────────────────────────────────
function openEditPopover(anchor, meta, id) {
  const { pop, close } = createPopover(anchor, `
    <h3 class="dialog-title">Edit Schematic</h3>
    <form id="edit-popover-form" autocomplete="off">
      <div class="form-group">
        <label class="label" for="pop-edit-name">Name</label>
        <input class="input" id="pop-edit-name" value="${esc(meta.name)}" maxlength="200" required />
      </div>
      <div class="form-group">
        <label class="label" for="pop-edit-desc">Description</label>
        <textarea class="textarea" id="pop-edit-desc" maxlength="5000">${esc(meta.description || '')}</textarea>
      </div>
      <div class="dialog-actions">
        <button type="button" class="button button-secondary" data-act="cancel">Cancel</button>
        <button type="submit" class="button button-primary" data-act="save">Save Changes</button>
      </div>
    </form>`);

  pop.querySelector('[data-act="cancel"]').addEventListener('click', close);
  pop.querySelector('#edit-popover-form').addEventListener('submit', async e => {
    e.preventDefault();
    const btn = pop.querySelector('[data-act="save"]');
    btn.disabled = true;
    btn.textContent = 'Saving...';
    try {
      await API.update(id, {
        name: pop.querySelector('#pop-edit-name').value.trim(),
        description: pop.querySelector('#pop-edit-desc').value.trim(),
      });
      toast('Schematic updated');
      close();
      route();
    } catch (err) {
      toast(err.message || 'Update failed', 'error');
      btn.disabled = false;
      btn.textContent = 'Save Changes';
    }
  });
  pop.querySelector('#pop-edit-name')?.focus();
}

// ───────────────────────────────────────────────────────────────
//  Sign in / sign up (single form)
// ───────────────────────────────────────────────────────────────
function renderAuth() {
  if (currentUser) { location.hash = '#/'; return; }
  $view.innerHTML = `
    <div class="fade-in auth-view">
      <div class="panel">
        <h2 class="page-title">Sign In</h2>
        <p class="auth-desc">New here? Pick a username and password &mdash; your account is created automatically.</p>
        <form id="auth-form" autocomplete="on">
          <div class="form-group">
            <label class="label" for="auth-username">Username</label>
            <input class="input" id="auth-username" name="username" autocomplete="username" minlength="3" maxlength="32" required />
            <div class="form-hint">3-32 characters: letters, digits, underscore or hyphen.</div>
          </div>
          <div class="form-group">
            <label class="label" for="auth-password">Password</label>
            <input class="input" id="auth-password" name="password" type="password" autocomplete="current-password" minlength="8" maxlength="72" required />
            <div class="form-hint">At least 8 characters.</div>
          </div>
          <div class="auth-error" id="auth-error" hidden></div>
          <div class="auth-actions">
            <button type="submit" class="button button-primary" id="auth-btn">Continue</button>
          </div>
        </form>
      </div>
    </div>`;

  document.getElementById('auth-form').addEventListener('submit', async e => {
    e.preventDefault();
    const btn = document.getElementById('auth-btn');
    const errEl = document.getElementById('auth-error');
    errEl.hidden = true;
    btn.disabled = true;
    btn.textContent = 'Please wait...';
    try {
      const { user, created } = await API.enter(
        document.getElementById('auth-username').value.trim(),
        document.getElementById('auth-password').value,
      );
      currentUser = user;
      renderAuthNav();
      toast(created ? 'Account created' : 'Welcome back, ' + user.username);
      location.hash = '#/';
    } catch (err) {
      errEl.textContent = err.message || 'Authentication failed';
      errEl.hidden = false;
      btn.disabled = false;
      btn.textContent = 'Continue';
    }
  });
}

// ───────────────────────────────────────────────────────────────
//  Boot
// ───────────────────────────────────────────────────────────────
(async function boot() {
  await refreshAuth();
  route();
})();
