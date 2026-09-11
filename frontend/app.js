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
        else reject(new Error(xhr.responseText || 'upload failed'));
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
    if (!r.ok) throw new Error(await r.text());
    return r.json();
  },
  async remove(id) {
    const r = await fetch('/api/schematics/' + id, { method: 'DELETE' });
    if (!r.ok) throw new Error('delete failed');
  },
  downloadUrl(id) { return '/api/schematics/' + id + '/file'; },
};

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
//  Confirm dialog (returns Promise<boolean>)
// ───────────────────────────────────────────────────────────────
let _confirmResolve = null;
function confirm(title, message, confirmLabel = 'Delete') {
  return new Promise(resolve => {
    _confirmResolve = resolve;
    document.getElementById('confirm-title').textContent = title;
    document.getElementById('confirm-message').textContent = message;
    document.getElementById('confirm-ok').textContent = confirmLabel;
    document.getElementById('confirm-overlay').hidden = false;
  });
}
document.getElementById('confirm-ok').onclick = () => { document.getElementById('confirm-overlay').hidden = true; _confirmResolve?.(true); };
document.getElementById('confirm-cancel').onclick = () => { document.getElementById('confirm-overlay').hidden = true; _confirmResolve?.(false); };
document.getElementById('confirm-overlay').onclick = e => { if (e.target.id === 'confirm-overlay') { e.currentTarget.hidden = true; _confirmResolve?.(false); } };

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
//  Router
// ───────────────────────────────────────────────────────────────
const $view = document.getElementById('view');
let _currentAbort = null;          // AbortController to cancel in-flight fetches

function route() {
  if (_currentAbort) _currentAbort.abort();
  _currentAbort = new AbortController();

  const hash = location.hash.slice(1) || '/';
  const parts = hash.split('/').filter(Boolean);

  // highlight nav
  document.querySelectorAll('.nav-link').forEach(a => a.classList.remove('active'));
  if (parts[0] === 'upload') document.querySelector('[data-nav="upload"]')?.classList.add('active');
  else document.querySelector('[data-nav="schematics"]')?.classList.add('active');

  if (parts.length === 0 || (parts.length === 1 && parts[0] === '')) { renderList(); return; }
  if (parts[0] === 'upload') { renderUpload(); return; }
  if (parts[0] === 'schematic' && parts[1] && parts[2] === 'edit') { renderEdit(parts[1]); return; }
  if (parts[0] === 'schematic' && parts[1]) { renderDetail(parts[1]); return; }
  $view.innerHTML = '<div class="panel fade-in"><h2 class="page-title">Not found</h2></div>';
}
window.addEventListener('hashchange', route);

// ───────────────────────────────────────────────────────────────
//  List / browse view
// ───────────────────────────────────────────────────────────────
let _listState = { q: '', sort: 'uploadDate', order: 'desc', limit: 20, offset: 0 };

async function renderList(overrides = {}) {
  Object.assign(_listState, overrides);
  const s = _listState;

  let html = `
    <div class="fade-in">
      <h2 class="page-title">Schematics</h2>
      <div class="toolbar">
        <input class="input" id="search-input" placeholder="Search schematics..." value="${esc(s.q)}" />
        <select class="select" id="sort-select">
          <option value="uploadDate"  ${s.sort === 'uploadDate'  ? 'selected' : ''}>Upload date</option>
          <option value="updatedDate" ${s.sort === 'updatedDate' ? 'selected' : ''}>Updated date</option>
          <option value="name"        ${s.sort === 'name'        ? 'selected' : ''}>Name</option>
          <option value="size"        ${s.sort === 'size'        ? 'selected' : ''}>Size</option>
        </select>
        <select class="select" id="order-select">
          <option value="desc" ${s.order === 'desc' ? 'selected' : ''}>Newest first</option>
          <option value="asc"  ${s.order === 'asc'  ? 'selected' : ''}>Oldest first</option>
        </select>
      </div>
      <div id="list-body"><div class="empty-state"><div class="empty-icon">...</div><p class="empty-text">Loading...</p></div></div>
    </div>`;
  $view.innerHTML = html;

  // events
  let searchTimer;
  document.getElementById('search-input').addEventListener('input', e => {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => renderList({ q: e.target.value, offset: 0 }), 280);
  });
  document.getElementById('sort-select').addEventListener('change', e => renderList({ sort: e.target.value, offset: 0 }));
  document.getElementById('order-select').addEventListener('change', e => renderList({ order: e.target.value, offset: 0 }));

  // fetch
  try {
    const data = await API.list({ q: s.q, sort: s.sort, order: s.order, limit: s.limit, offset: s.offset });
    renderListBody(data);
  } catch (err) {
    if (err.name === 'AbortError') return;
    document.getElementById('list-body').innerHTML = '<div class="empty-state"><p class="empty-text" style="color:#ef4444">Failed to load schematics.</p></div>';
  }
}

function renderListBody(data) {
  const el = document.getElementById('list-body');
  if (!data.items || data.items.length === 0) {
    el.innerHTML = `
      <div class="empty-state panel">
        <div class="empty-icon">&#128196;</div>
        <p class="empty-text">${_listState.q ? 'No schematics match your search.' : 'No schematics uploaded yet.'}</p>
        ${!_listState.q ? '<p class="empty-hint"><a href="#/upload">Upload your first schematic</a></p>' : ''}
      </div>`;
    return;
  }

  let grid = '<div class="schematics-grid">';
  for (const m of data.items) {
    grid += `
      <div class="schematic-card" data-href="#/schematic/${m.id}" tabindex="0">
        <div class="card-icon">&#128196;</div>
        <div class="card-body">
          <div class="card-name">${esc(m.name)}</div>
          <div class="card-desc">${esc(m.description || m.fileName)}</div>
        </div>
        <div class="card-meta">
          <span class="card-size">${fmtBytes(m.size)}</span>
          <span>${fmtDate(m.uploadDate)}</span>
        </div>
        <a href="${API.downloadUrl(m.id)}" class="button button-secondary button-sm card-download" download title="Download ${esc(m.fileName)}" onclick="event.stopPropagation()">Download</a>
      </div>`;
  }
  grid += '</div>';

  // pagination
  const totalPages = Math.ceil(data.total / _listState.limit) || 1;
  const curPage = Math.floor(data.offset / _listState.limit) + 1;
  const hasPrev = data.offset > 0;
  const hasNext = data.offset + data.items.length < data.total;
  grid += `
    <div class="pagination">
      <button class="button button-secondary button-sm" id="pg-prev" ${!hasPrev ? 'disabled' : ''}>&#8249; Prev</button>
      <span class="page-info">${curPage} / ${totalPages} &middot; ${data.total} total</span>
      <button class="button button-secondary button-sm" id="pg-next" ${!hasNext ? 'disabled' : ''}>Next &#8250;</button>
    </div>`;

  el.innerHTML = grid;

  el.querySelectorAll('.schematic-card').forEach(card => {
    const go = () => { location.hash = card.dataset.href; };
    card.addEventListener('click', go);
    card.addEventListener('keydown', e => { if (e.key === 'Enter') { e.preventDefault(); go(); } });
  });

  document.getElementById('pg-prev')?.addEventListener('click', () => {
    renderList({ offset: Math.max(0, _listState.offset - _listState.limit) });
  });
  document.getElementById('pg-next')?.addEventListener('click', () => {
    renderList({ offset: _listState.offset + _listState.limit });
  });
}

// ───────────────────────────────────────────────────────────────
//  Upload / create view
// ───────────────────────────────────────────────────────────────
function renderUpload() {
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

  const editHash = '#/schematic/' + id + '/edit';
  const dlUrl    = API.downloadUrl(id);

  $view.innerHTML = `
    <div class="fade-in">
      <div class="panel detail-panel">
        <h2 class="page-title detail-name">${esc(meta.name)}</h2>
        ${meta.description ? '<p class="detail-description">' + esc(meta.description) + '</p>' : ''}
        <div class="detail-actions">
          <a href="${dlUrl}" class="button button-primary" download>Download</a>
          <a href="${editHash}" class="button button-secondary">Edit</a>
          <button class="button button-danger" id="detail-delete">Delete</button>
        </div>
      </div>
      <div class="panel">
        <div class="meta-grid">
          <div class="meta-item"><span class="meta-label">ID</span><span class="meta-value">${esc(meta.id)}</span></div>
          <div class="meta-item"><span class="meta-label">Original File</span><span class="meta-value">${esc(meta.fileName)}</span></div>
          <div class="meta-item"><span class="meta-label">File Size</span><span class="meta-value">${fmtBytes(meta.size)}</span></div>
          <div class="meta-item"><span class="meta-label">Content Type</span><span class="meta-value">${esc(meta.contentType)}</span></div>
          <div class="meta-item"><span class="meta-label">Uploaded</span><span class="meta-value">${fmtDateTime(meta.uploadDate)}</span></div>
          <div class="meta-item"><span class="meta-label">Last Updated</span><span class="meta-value">${fmtDateTime(meta.updatedDate)}</span></div>
        </div>
      </div>
    </div>`;

  document.getElementById('detail-delete').addEventListener('click', async () => {
    const ok = await confirm('Delete Schematic', `Are you sure you want to delete "${meta.name}"? This cannot be undone.`);
    if (!ok) return;
    try {
      await API.remove(id);
      toast('Schematic deleted');
      location.hash = '#/';
    } catch { toast('Failed to delete', 'error'); }
  });
}

// ───────────────────────────────────────────────────────────────
//  Edit view
// ───────────────────────────────────────────────────────────────
async function renderEdit(id) {
  $view.innerHTML = '<div class="panel fade-in"><div class="empty-state"><p class="empty-text">Loading...</p></div></div>';

  let meta;
  try { meta = await API.get(id); } catch {
    $view.innerHTML = '<div class="panel fade-in"><h2 class="page-title">Schematic not found</h2><a href="#/" class="button button-secondary">Back to list</a></div>';
    return;
  }

  $view.innerHTML = `
    <div class="fade-in">
      <h2 class="page-title">Edit Schematic</h2>
      <div class="panel">
        <form id="edit-form" autocomplete="off">
          <div class="form-group">
            <label class="label" for="edit-name">Name</label>
            <input class="input" id="edit-name" value="${esc(meta.name)}" maxlength="200" required />
          </div>
          <div class="form-group">
            <label class="label" for="edit-desc">Description</label>
            <textarea class="textarea" id="edit-desc" maxlength="5000">${esc(meta.description)}</textarea>
          </div>
          <div style="display:flex;gap:.6rem;margin-top:1rem">
            <button type="submit" class="button button-primary" id="edit-btn">Save Changes</button>
            <a href="#/schematic/${id}" class="button button-secondary">Cancel</a>
          </div>
        </form>
      </div>
    </div>`;

  document.getElementById('edit-form').addEventListener('submit', async e => {
    e.preventDefault();
    const btn = document.getElementById('edit-btn');
    btn.disabled = true;
    btn.textContent = 'Saving...';
    try {
      await API.update(id, {
        name: document.getElementById('edit-name').value.trim(),
        description: document.getElementById('edit-desc').value.trim(),
      });
      toast('Schematic updated');
      location.hash = '#/schematic/' + id;
    } catch (err) {
      toast(err.message || 'Update failed', 'error');
      btn.disabled = false;
      btn.textContent = 'Save Changes';
    }
  });
}

// ───────────────────────────────────────────────────────────────
//  Boot
// ───────────────────────────────────────────────────────────────
route();
