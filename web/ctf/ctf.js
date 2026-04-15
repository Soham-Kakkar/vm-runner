document.addEventListener('DOMContentLoaded', () => {
  const params = new URLSearchParams(window.location.search);
  const id = params.get('id');
  const titleEl = document.getElementById('ctf-title');
  const metaEl = document.getElementById('ctf-meta');
  const challengeList = document.getElementById('challenge-list');

  const APP_URL = window.location.protocol === 'file:' ? 'http://localhost:8080' : '';

  function toTitleCase(value) {
    return value.replace(/[-_]/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
  }

  // session state
  let currentSession = null;
  let ws = null;
  let rfb = null;
  let term = null;
  let fitAddon = null;
  let termListener = null;
  const STORAGE_KEY = 'vmrunner_current_session';

  function protocolPrefix() { return window.location.protocol === 'https:' ? 'wss:' : 'ws:' }
  function makeWSURL(path) {
    if (!path) return '';
    // If the caller already provided a full ws/wss URL or another scheme, return it as-is.
    if (path.startsWith('ws:') || path.startsWith('wss:') || path.includes('://')) return path;
    const host = window.location.host || 'localhost:8080';
    return `${protocolPrefix()}//${host}${path}`;
  }

  function ensureTerminal() {
    if (term) return term;
    term = new Terminal({ cursorBlink: true, convertEol: true, fontFamily: 'IBM Plex Mono, monospace', fontSize: 13 });
    fitAddon = new FitAddon.FitAddon();
    term.loadAddon(fitAddon);
    const terminalContainer = document.getElementById('terminal');
    term.open(terminalContainer);
    fitAddon.fit();
    window.addEventListener('resize', () => fitAddon && fitAddon.fit());
    return term;
  }

  function destroyTerminal() {
    if (termListener) { try { termListener.dispose() } catch (e) {} termListener = null }
    if (term) { term.dispose(); term = null }
    fitAddon = null;
    document.getElementById('terminal').innerHTML = '';
  }

  function setupVNC(vncUrl) {
    if (rfb) { rfb.disconnect(); rfb = null }
    const attach = () => { if (!window.RFB) { setTimeout(attach, 100); return };
      rfb = new window.RFB(document.getElementById('vnc-screen'), makeWSURL(vncUrl)); rfb.scaleViewport = true; rfb.resizeSession = true; };
    attach();
  }

  function connectSessionSocket(wsUrl) {
    if (ws) { try { ws.close() } catch {} ws = null }
    const t = ensureTerminal();
    const full = makeWSURL(wsUrl);
    try {
      ws = new WebSocket(full);
    } catch (err) {
      console.error('failed to create websocket for', full, err);
      return;
    }
    ws.onopen = () => {
      if (termListener) try { termListener.dispose() } catch (e) {}
      termListener = t.onData(data => { if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'input', payload: data })); });
    };
    ws.onmessage = ev => {
      const msg = JSON.parse(ev.data);
      if (msg.type === 'vm_output') {
        const output = String(msg.payload || '');
        t.write(output);
      } else if (msg.type === 'flag_found') {
        document.getElementById('answer-feedback').textContent = 'Submission accepted.';
      } else if (msg.type === 'error') {
        t.write(`\r\n[ERROR] ${msg.payload}\r\n`);
        document.getElementById('answer-feedback').textContent = String(msg.payload || 'Session error');
      }
    };
    ws.onclose = () => { if (termListener) { try{ termListener.dispose() }catch{} termListener=null } };
  }

  async function initializeSession(session) {
    if (!session) return;
    currentSession = session;
    
    // Reconstruct URLs if missing (e.g. when loading from persisted session API)
    if (!session.ws_url) session.ws_url = `/ws/session/${session.id}`;
    if (!session.vnc_url) session.vnc_url = `/vnc/session/${session.id}`;

    document.getElementById('session-title').textContent = (session.challenge && session.challenge.title) || 'Session';
    document.getElementById('validator-pill').textContent = (session.challenge && session.challenge.validator) || 'static';
    document.getElementById('runtime-pill').textContent = session.challenge?.vm_config?.display_type || 'terminal';
    document.getElementById('challenge-desc').textContent = session.challenge?.description || 'No description available.';
    
    document.getElementById('session-view').classList.remove('hidden');
    document.getElementById('no-session-msg').classList.add('hidden');
    
    if (session.challenge?.vm_config?.display_type === 'vnc') {
      document.getElementById('terminal-panel').classList.add('hidden');
      document.getElementById('vnc-container').classList.remove('hidden');
      setupVNC(session.vnc_url);
    } else {
      document.getElementById('vnc-container').classList.add('hidden');
      document.getElementById('terminal-panel').classList.remove('hidden');
    }
    if (session.ws_url) connectSessionSocket(session.ws_url);
    
    // Always ensure the storage is in sync with the current active session
    saveSessionToStorage(session);
  }

    function saveSessionToStorage(session) {
      try {
        if (session && session.id) {
          localStorage.setItem(STORAGE_KEY, session.id);
        }
      } catch (e) {
        console.warn('failed to save session to storage', e);
      }
    }

    function clearSessionStorage() {
      try { localStorage.removeItem(STORAGE_KEY) } catch (e) {}
    }

    async function loadPersistedSession() {
      try {
        const sid = localStorage.getItem(STORAGE_KEY);
        if (!sid) return;
        const resp = await fetch(`${APP_URL}/api/sessions/${encodeURIComponent(sid)}`);
        if (!resp.ok) { clearSessionStorage(); return }
        const session = await resp.json();
        if (!session || !session.challenge || session.challenge.ctf_id !== id) {
          clearSessionStorage();
          return;
        }
        initializeSession(session);
      } catch (e) {
        console.warn('failed to rehydrate session from storage', e);
      }
    }

  async function submitAnswer(answer) {
    if (!currentSession) return;
    const resp = await fetch(`${APP_URL}/api/sessions/${currentSession.id}/submit-answer`, { method: 'POST', headers: { 'Content-Type':'application/json' }, body: JSON.stringify({ answer }) });
    const payload = await resp.json();
    document.getElementById('answer-feedback').textContent = (resp.ok && payload.is_correct) ? 'Correct answer.' : 'Incorrect answer.';
  }

  async function stopSession() {
    if (!currentSession) return;
    await fetch(`${APP_URL}/api/sessions/${currentSession.id}/stop`, { method: 'POST' });
    if (ws) { try { ws.close() } catch {} ws = null }
    if (rfb) { try { rfb.disconnect() } catch {} rfb = null }
    destroyTerminal();
    currentSession = null;
    clearSessionStorage();
    document.getElementById('session-view').classList.add('hidden');
    document.getElementById('no-session-msg').classList.remove('hidden');
  }

  document.addEventListener('click', ev => { if (ev.target && ev.target.matches && ev.target.matches('.start')) ev.target.dispatchEvent(new Event('start-click')) });

  async function loadCTF() {
    if (!id) return;
    const res = await fetch(`${APP_URL}/api/ctfs/${encodeURIComponent(id)}`);
    if (!res.ok) { titleEl.textContent = 'CTF not found'; return; }
    const ctf = await res.json();
    titleEl.textContent = ctf.title || id;
    metaEl.textContent = `${ctf.status} · ${ctf.visibility}`;
    challengeList.innerHTML = '';
    ctf.challenges.forEach(ch => {
      const item = document.createElement('a');
      item.href = '#';
      item.className = 'nav-item';
      item.innerHTML = `<span>🚩</span> ${ch.title}`;
      item.addEventListener('click', async (e) => {
        e.preventDefault();
        // Remove active class from others
        challengeList.querySelectorAll('.nav-item').forEach(el => el.classList.remove('active'));
        item.classList.add('active');
        
        const body = { challenge_id: ch.id };
        try {
          const sid = localStorage.getItem(STORAGE_KEY);
          if (sid) body.session_id = sid;
        } catch (e) {}
        const resp = await fetch(`${APP_URL}/api/sessions`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
        if (!resp.ok) { alert('Failed to start session'); return; }
        const payload = await resp.json();
        const session = payload.session || payload;
        initializeSession(session);
        saveSessionToStorage(session);
      });
      challengeList.appendChild(item);
    });
  }

  document.getElementById('end-session-btn').addEventListener('click', stopSession);
  document.getElementById('submit-form').addEventListener('submit', async e => { e.preventDefault(); const v = document.getElementById('answer-input').value.trim(); if (!v) return; await submitAnswer(v); });

  loadCTF().then(() => loadPersistedSession()).catch(err => { titleEl.textContent = 'Error loading CTF'; metaEl.textContent = err.message || ''; });
});
