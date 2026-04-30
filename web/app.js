document.addEventListener('DOMContentLoaded', () => {
  const ctfList = document.getElementById('ctf-list');
  const ctfCount = document.getElementById('ctf-count');
  const APP_URL = window.location.protocol === 'file:' ? 'http://localhost:8080' : '';
  const currentUser = JSON.parse(localStorage.getItem('vmrunner_user') || '{}');

  function escapeHTML(value) {
    return String(value || '').replace(/[&<>"']/g, ch => ({
      '&': '&amp;',
      '<': '&lt;',
      '>': '&gt;',
      '"': '&quot;',
      "'": '&#39;'
    }[ch]));
  }

  function canEditCTF(ctf) {
    const creator = String(ctf.maker || ctf.owner_id || '').trim();
    if (!creator || creator === 'unknown' || creator === 'system') {
      return currentUser.role === 'admin';
    }
    return currentUser.username === creator;
  }

  async function loadCTFs() {
    // If this script runs on a page without a CTF list (e.g. the per-CTF page), skip.
    if (!ctfList || !ctfCount) return;
    const res = await fetch(`${APP_URL}/api/ctfs`);
    if (!res.ok) throw new Error('failed to load ctfs');
    const ctfs = await res.json();
    ctfList.innerHTML = '';
    ctfCount.textContent = String(ctfs.length || 0);
    ctfs.forEach(ctf => {
      const card = document.createElement('div');
      card.className = 'card';
      const editLink = 
        `<a href="/maker/?id=${encodeURIComponent(ctf.id)}" class="btn">Edit</a>`
        // : '';
      card.innerHTML = `
        <div style="display: flex; justify-content: space-between; align-items: flex-start;">
          <h3 style="font-size: 18px;">${escapeHTML(ctf.title)}</h3>
          <span class="pill" style="color: ${ctf.status === 'published' ? 'var(--color-primary)' : 'var(--color-warning)'}">${escapeHTML(ctf.status)}</span>
        </div>
        <div style="margin: 8px 0; font-family: var(--font-mono); font-size: 12px; color: var(--text-dim);">
          ID: ${escapeHTML(ctf.id)}
        </div>
        <p style="flex-grow: 1;">${ctf.visibility === 'public' ? '🌍 Public Lab' : '🔒 Private Lab'}</p>
        <div style="display: flex; justify-content: space-between; align-items: center; margin-top: 16px; padding-top: 16px; border-top: 1px solid var(--border-default);">
          <div style="display: flex; flex-direction: column;">
            <span style="font-size: 12px; font-weight: 600;">${ctf.challenges?.length || 0} Challenges</span>
            <span style="font-size: 11px; color: var(--text-dim);">by ${escapeHTML(ctf.maker || 'system')}</span>
          </div>
          <div style="display: flex; gap: 8px;">
            ${editLink}
            <a href="/ctf/?id=${encodeURIComponent(ctf.id)}" class="btn primary">Enter Lab</a>
          </div>
        </div>
      `;
      ctfList.appendChild(card);
    });
  }

  loadCTFs().catch(err => console.error('loadCTFs:', err));
});
