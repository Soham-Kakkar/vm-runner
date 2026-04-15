document.addEventListener('DOMContentLoaded', () => {
  const ctfList = document.getElementById('ctf-list');
  const ctfCount = document.getElementById('ctf-count');
  const APP_URL = window.location.protocol === 'file:' ? 'http://localhost:8080' : '';

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
      card.innerHTML = `
        <div style="display: flex; justify-content: space-between; align-items: flex-start;">
          <h3 style="font-size: 18px;">${ctf.title}</h3>
          <span class="pill" style="color: ${ctf.status === 'published' ? 'var(--color-primary)' : 'var(--color-warning)'}">${ctf.status}</span>
        </div>
        <div style="margin: 8px 0; font-family: var(--font-mono); font-size: 12px; color: var(--text-dim);">
          ID: ${ctf.id}
        </div>
        <p style="flex-grow: 1;">${ctf.visibility === 'public' ? '🌍 Public Lab' : '🔒 Private Lab'}</p>
        <div style="display: flex; justify-content: space-between; align-items: center; margin-top: 16px; padding-top: 16px; border-top: 1px solid var(--border-default);">
          <div style="display: flex; flex-direction: column;">
            <span style="font-size: 12px; font-weight: 600;">${ctf.challenges?.length || 0} Challenges</span>
            <span style="font-size: 11px; color: var(--text-dim);">by ${ctf.maker || 'system'}</span>
          </div>
          <a href="/ctf/?id=${encodeURIComponent(ctf.id)}" class="btn primary">Enter Lab</a>
        </div>
      `;
      ctfList.appendChild(card);
    });
  }

  loadCTFs().catch(err => console.error('loadCTFs:', err));
});
