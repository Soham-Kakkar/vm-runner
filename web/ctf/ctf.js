document.addEventListener('DOMContentLoaded', () => {
  const params = new URLSearchParams(window.location.search);
  const id = params.get('id');
  const titleEl = document.getElementById('ctf-title');
  const metaEl = document.getElementById('ctf-meta');
  const challengeList = document.getElementById('challenge-list');

  const APP_URL = window.location.protocol === 'file:' ? 'http://localhost:8080' : '';
  const currentUser = JSON.parse(localStorage.getItem('vmrunner_user') || '{}');

  function authHeaders(extra = {}) {
    return {
      ...extra,
      'X-VMRunner-User': currentUser.username || '',
      'X-VMRunner-Role': currentUser.role || ''
    };
  }

  function ensureToastContainer() {
    let el = document.getElementById('toast-container');
    if (!el) {
      el = document.createElement('div');
      el.id = 'toast-container';
      document.body.appendChild(el);
    }
    return el;
  }

  const _toastHistory = new Map();
  function showToast(message, type = 'info') {
    const key = `${type}|${message}`;
    const now = Date.now();
    const prev = _toastHistory.get(key) || 0;
    if (now - prev < 3000) return;
    _toastHistory.set(key, now);
    setTimeout(() => _toastHistory.delete(key), 5000);

    const container = ensureToastContainer();
    const toast = document.createElement('div');
    toast.className = `toast ${type}`;
    toast.textContent = message;
    container.appendChild(toast);
    setTimeout(() => toast.classList.add('show'), 10);
    setTimeout(() => {
      toast.classList.remove('show');
      setTimeout(() => toast.remove(), 300);
    }, 2600);
  }

  function setFeedback(message, type) {
    const el = document.getElementById('answer-feedback');
    if (!el) return;
    el.textContent = message;
    el.classList.remove('success', 'error');
    if (type) el.classList.add(type);
  }

  function escapeHTML(value) {
    return String(value || '').replace(/[&<>"]|'/g, ch => ({
      '&': '&amp;',
      '<': '&lt;',
      '>': '&gt;',
      '"': '&quot;',
      "'": '&#39;'
    }[ch]));
  }

  let currentSession = null;
  let ws = null;
  let rfb = null;
  let term = null;
  let fitAddon = null;
  let termListener = null;
  const STORAGE_KEY = 'vmrunner_current_session';
  let answerAccepted = false;
  let currentChallengeId = null;
  let solvedChallenges = new Set();
  let lastSessionId = null;
  let terminalCommandBuf = '';
  let lastTerminalCommand = '';

  function protocolPrefix() { return window.location.protocol === 'https:' ? 'wss:' : 'ws:'; }
  function makeWSURL(path) {
    if (!path) return '';
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

  function resetTerminalCommandTracking() {
    terminalCommandBuf = '';
    lastTerminalCommand = '';
  }

  function trackTerminalCommand(data) {
    if (!data) return;
    for (const ch of data) {
      if (ch === '\r' || ch === '\n') {
        const cmd = terminalCommandBuf.trim();
        if (cmd) lastTerminalCommand = cmd;
        terminalCommandBuf = '';
        continue;
      }
      if (ch === '\b' || ch === '\u007f') {
        if (terminalCommandBuf.length > 0) {
          terminalCommandBuf = terminalCommandBuf.slice(0, -1);
        }
        continue;
      }
      if (ch === '\u001b') {
        continue;
      }
      if (ch >= ' ') {
        terminalCommandBuf += ch;
      }
    }
  }

  function destroyTerminal() {
    if (termListener) { try { termListener.dispose(); } catch (e) {} termListener = null; }
    if (term) { term.dispose(); term = null; }
    fitAddon = null;
    const terminalContainer = document.getElementById('terminal');
    if (terminalContainer) terminalContainer.innerHTML = '';
  }

  function setupVNC(vncUrl) {
    if (rfb) { rfb.disconnect(); rfb = null; }
    const attach = () => {
      if (!window.RFB) { setTimeout(attach, 100); return; }
      rfb = new window.RFB(document.getElementById('vnc-screen'), makeWSURL(vncUrl));
      rfb.scaleViewport = true;
      rfb.resizeSession = true;
    };
    attach();
  }

  function connectSessionSocket(wsUrl) {
    if (ws) { try { ws.close(); } catch {} ws = null; }
    const t = ensureTerminal();
    const full = makeWSURL(wsUrl);
    try {
      ws = new WebSocket(full);
    } catch (err) {
      console.error('failed to create websocket for', full, err);
      return;
    }
    ws.onopen = () => {
      if (termListener) try { termListener.dispose(); } catch (e) {}
      termListener = t.onData(data => {
        trackTerminalCommand(data);
        if (ws && ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ type: 'input', payload: data }));
        }
      });
    };
    ws.onmessage = ev => {
      const msg = JSON.parse(ev.data);
      if (msg.type === 'vm_output') {
        const output = String(msg.payload || '');
        t.write(output);
      } else if (msg.type === 'flag_found') {
        const payload = msg.payload || {};
        const solvedId = payload.challenge_id || currentChallengeId;
        const isCurrent = solvedId && solvedId === currentChallengeId;
        applySolvedState({ toast: isCurrent, challengeId: solvedId });
      } else if (msg.type === 'error') {
        t.write(`\r\n[ERROR] ${msg.payload}\r\n`);
        const message = String(msg.payload || 'Session error');
        setFeedback(message, 'error');
        showToast(message, 'error');
      }
    };
    ws.onclose = () => { if (termListener) { try { termListener.dispose(); } catch {} termListener = null; } };
  }

  function disableAnswerInput(disabled) {
    try {
      const input = document.getElementById('answer-input');
      const submit = document.querySelector('#submit-form [type="submit"]');
      if (input) {
        input.disabled = !!disabled;
        input.classList.toggle('answer-disabled', !!disabled);
      }
      if (submit) {
        submit.disabled = !!disabled;
        submit.classList.toggle('answer-disabled', !!disabled);
      }
    } catch (e) { console.warn('disableAnswerInput failed', e); }
  }

  function applySolvedState({ toast, challengeId }) {
    const solvedId = challengeId || currentChallengeId;
    if (!solvedId) return;
    if (solvedChallenges.has(solvedId) && answerAccepted && solvedId === currentChallengeId) return;
    solvedChallenges.add(solvedId);
    if (solvedId !== currentChallengeId) return;
    if (answerAccepted) return;
    answerAccepted = true;
    const message = 'Answer accepted.';
    setFeedback(message, 'success');
    if (toast) showToast(message, 'success');
    disableAnswerInput(true);
  }

  function resetAnswerState() {
    answerAccepted = false;
    disableAnswerInput(false);
    setFeedback('', null);
  }

  async function initializeSession(session) {
    if (!session) return;
    currentSession = session;
    const isNewSession = session.id !== lastSessionId;
    if (isNewSession) {
      solvedChallenges = new Set();
      lastSessionId = session.id;
    }
    resetTerminalCommandTracking();
    if (Array.isArray(session.submissions)) {
      session.submissions.forEach(sub => {
        if (sub && sub.is_correct && sub.challenge_id) {
          solvedChallenges.add(sub.challenge_id);
        }
      });
    }
    currentChallengeId = session.challenge?.id || session.challenge_id || null;
    resetAnswerState();

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

    if (currentChallengeId && solvedChallenges.has(currentChallengeId)) {
      applySolvedState({ toast: false, challengeId: currentChallengeId });
    }

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
    try { localStorage.removeItem(STORAGE_KEY); } catch (e) {}
  }

  async function loadPersistedSession() {
    try {
      const sid = localStorage.getItem(STORAGE_KEY);
      if (!sid) return;
      const resp = await fetch(`${APP_URL}/api/sessions/${encodeURIComponent(sid)}`, { headers: authHeaders() });
      if (!resp.ok) { clearSessionStorage(); return; }
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
    const resp = await fetch(`${APP_URL}/api/sessions/${currentSession.id}/submit-answer`, {
      method: 'POST',
      headers: authHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify({ answer, last_command: lastTerminalCommand })
    });
    const payload = await resp.json();
    if (resp.ok && payload.is_correct) {
      applySolvedState({ toast: true, challengeId: currentChallengeId });
    } else {
      const errMsg = payload && payload.error ? payload.error : 'Incorrect answer.';
      setFeedback(errMsg, 'error');
      showToast(errMsg, 'error');
    }
  }

  async function stopSession() {
    if (!currentSession) return;
    await stopCurrentSession();
    document.getElementById('session-view').classList.add('hidden');
    document.getElementById('no-session-msg').classList.remove('hidden');
  }

  async function stopCurrentSession() {
    if (!currentSession) return;
    await fetch(`${APP_URL}/api/sessions/${currentSession.id}/stop`, { method: 'POST', headers: authHeaders() });
    if (ws) { try { ws.close(); } catch {} ws = null; }
    if (rfb) { try { rfb.disconnect(); } catch {} rfb = null; }
    destroyTerminal();
    currentSession = null;
    lastSessionId = null;
    solvedChallenges = new Set();
    currentChallengeId = null;
    clearSessionStorage();
  }

  document.addEventListener('click', ev => {
    if (ev.target && ev.target.matches && ev.target.matches('.start')) {
      ev.target.dispatchEvent(new Event('start-click'));
    }
  });

  async function loadCTF() {
    if (!id) return;
    const res = await fetch(`${APP_URL}/api/ctfs/${encodeURIComponent(id)}`, { headers: authHeaders() });
    if (!res.ok) { titleEl.textContent = 'CTF not found'; return; }
    const ctf = await res.json();
    titleEl.textContent = ctf.title || id;
    metaEl.textContent = `${ctf.status} · ${ctf.visibility}`;
    challengeList.innerHTML = '';
    ctf.challenges.forEach(ch => {
      const item = document.createElement('a');
      item.href = '#';
      item.className = 'nav-item';
      item.innerHTML = `${escapeHTML(ch.title)}`;
      item.addEventListener('click', async (e) => {
        e.preventDefault();
        if (!currentSession) {
          try {
            const staleID = localStorage.getItem(STORAGE_KEY);
            if (staleID) {
              await fetch(`${APP_URL}/api/sessions/${encodeURIComponent(staleID)}/stop`, { method: 'POST', headers: authHeaders() });
              clearSessionStorage();
            }
          } catch (e) {}
        }

        const body = { challenge_id: ch.id };
        try {
          if (currentSession && currentSession.id) {
            body.session_id = currentSession.id;
          }
        } catch (e) {}
        const resp = await fetch(`${APP_URL}/api/sessions`, { method: 'POST', headers: authHeaders({ 'Content-Type': 'application/json' }), body: JSON.stringify(body) });
        if (!resp.ok) {
          const err = await resp.text();
          alert(`Failed to start session: ${err || resp.statusText}`);
          return;
        }
        const payload = await resp.json();
        const session = payload.session || payload;
        initializeSession(session);
        saveSessionToStorage(session);
        challengeList.querySelectorAll('.nav-item').forEach(el => el.classList.remove('active'));
        item.classList.add('active');
      });
      challengeList.appendChild(item);
    });
  }

  document.getElementById('end-session-btn').addEventListener('click', stopSession);
  document.getElementById('submit-form').addEventListener('submit', async e => {
    e.preventDefault();
    const v = document.getElementById('answer-input').value.trim();
    if (!v) return;
    await submitAnswer(v);
  });

  loadCTF().then(() => loadPersistedSession()).catch(err => {
    titleEl.textContent = 'Error loading CTF';
    metaEl.textContent = err.message || '';
  });
});
