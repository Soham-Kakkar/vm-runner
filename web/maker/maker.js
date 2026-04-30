document.addEventListener('DOMContentLoaded', () => {
  const ctfList = document.getElementById('challenges-list');
  const addBtn = document.getElementById('add-challenge-btn');
  const saveBtn = document.getElementById('save-ctf-btn');
  const template = document.getElementById('challenge-template');
  const uploadInput = document.getElementById('vm-image-file');
  const uploadStatus = document.getElementById('upload-status');
  const telemetrySection = document.getElementById('telemetry-section');
  const scoresTableBody = document.querySelector('#scores-table tbody');
  const telemetryTableBody = document.querySelector('#telemetry-table tbody');
  const APP_URL = window.location.protocol === 'file:' ? 'http://localhost:8080' : '';
  const params = new URLSearchParams(window.location.search);
  const editID = params.get('id');

  let challengeCount = 0;
  let existingCTF = null;

  function getUser() {
    return JSON.parse(localStorage.getItem('vmrunner_user') || '{}');
  }

  function authHeaders(extra = {}) {
    const user = getUser();
    return {
      ...extra,
      'X-VMRunner-User': user.username || '',
      'X-VMRunner-Role': user.role || ''
    };
  }

  function canEditCTF(ctf) {
    const user = getUser();
    const creator = String(ctf.maker || ctf.owner_id || '').trim();
    if (!creator || creator === 'unknown' || creator === 'system') {
      return user.role === 'admin';
    }
    return user.username === creator;
  }

  function escapeHTML(value) {
    return String(value || '').replace(/[&<>"']/g, ch => ({
      '&': '&amp;',
      '<': '&lt;',
      '>': '&gt;',
      '"': '&quot;',
      "'": '&#39;'
    }[ch]));
  }

  function stripANSI(value) {
    if (!value) return '';
    return String(value)
      .replace(/\x1b\[[0-9;?]*[A-Za-z]/g, '')
      .replace(/\x1b\][^\x1b]*\x07/g, '')
      .replace(/[\x00-\x1F\x7F]+/g, ' ')
      .trim();
  }

  function syncValidatorFields(container) {
    const validatorSelect = container.querySelector('.ch-validator');
    const staticField = container.querySelector('.static-only');
    const hmacField = container.querySelector('.hmac-only');
    if (validatorSelect.value === 'hmac') {
      staticField.classList.add('hidden');
      hmacField.classList.remove('hidden');
    } else {
      staticField.classList.remove('hidden');
      hmacField.classList.add('hidden');
    }
  }

  function addChallenge(challenge = {}) {
    challengeCount++;
    const clone = template.content.cloneNode(true);
    const container = clone.querySelector('.challenge-editor');
    container.dataset.challengeId = challenge.id || '';

    const idMeta = container.querySelector('.challenge-id');
    if (challenge.id) {
      idMeta.textContent = `ID: ${challenge.id}`;
      idMeta.classList.remove('hidden');
    }

    const questionNo = challenge.question_no || challengeCount;
    container.querySelector('.challenge-num').textContent = `Question #${questionNo}`;
    container.querySelector('.ch-qno').value = String(questionNo);
    container.querySelector('.ch-title').value = challenge.title || '';
    container.querySelector('.ch-desc').value = challenge.description || '';

    const validatorSelect = container.querySelector('.ch-validator');
    validatorSelect.value = challenge.validator || 'hmac';
    container.querySelector('.ch-flag').value = challenge.flag || '';
    container.querySelector('.ch-template').value = challenge.template || 'flag{<hmac>}';
    syncValidatorFields(container);

    validatorSelect.addEventListener('change', () => syncValidatorFields(container));
    container.querySelector('.ch-qno').addEventListener('input', updateChallengeNumbers);
    container.querySelector('.remove-btn').addEventListener('click', () => {
      container.remove();
      updateChallengeNumbers();
    });

    ctfList.appendChild(clone);
  }

  function updateChallengeNumbers() {
    const containers = ctfList.querySelectorAll('.challenge-editor');
    challengeCount = containers.length;
    containers.forEach((c, i) => {
      const questionNo = parseInt(c.querySelector('.ch-qno').value, 10) || i + 1;
      c.querySelector('.challenge-num').textContent = `Question #${questionNo}`;
    });
  }

  function collectChallenges() {
    const challenges = [];
    const containers = ctfList.querySelectorAll('.challenge-editor');
    containers.forEach(c => {
      const ch = {
        title: c.querySelector('.ch-title').value.trim(),
        description: c.querySelector('.ch-desc').value,
        validator: c.querySelector('.ch-validator').value,
        question_no: parseInt(c.querySelector('.ch-qno').value, 10) || 1
      };
      if (c.dataset.challengeId) ch.id = c.dataset.challengeId;
      if (!ch.title) throw new Error('Each question needs a name.');
      if (ch.validator === 'hmac') ch.template = c.querySelector('.ch-template').value;
      else ch.flag = c.querySelector('.ch-flag').value;
      if (!(ch.template || ch.flag)) throw new Error('Each question needs a flag format.');
      challenges.push(ch);
    });
    if (challenges.length === 0) throw new Error('Add at least one question.');
    return challenges;
  }

  function formatDate(value) {
    if (!value) return '-';
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return String(value);
    return d.toLocaleString();
  }

  async function loadTelemetry() {
    if (!editID || !telemetrySection) return;
    try {
      const [scoresResp, telemetryResp] = await Promise.all([
        fetch(`${APP_URL}/api/ctfs/${encodeURIComponent(editID)}/scores`, { headers: authHeaders() }),
        fetch(`${APP_URL}/api/ctfs/${encodeURIComponent(editID)}/telemetry`, { headers: authHeaders() })
      ]);

      if (scoresResp.ok) {
        const scores = await scoresResp.json();
        scoresTableBody.innerHTML = '';
        (scores.scores || []).forEach(entry => {
          const row = document.createElement('tr');
          row.innerHTML = `
            <td>${escapeHTML(entry.user || '-')}</td>
            <td>${entry.score ?? 0}</td>
            <td>${formatDate(entry.last_updated)}</td>
          `;
          scoresTableBody.appendChild(row);
        });
        if ((scores.scores || []).length === 0) {
          scoresTableBody.innerHTML = '<tr><td colspan="3" class="muted">No scores yet.</td></tr>';
        }
      }

      if (telemetryResp.ok) {
        const events = await telemetryResp.json();
        telemetryTableBody.innerHTML = '';
        events.slice().reverse().forEach(ev => {
          const row = document.createElement('tr');
          row.innerHTML = `
            <td>${formatDate(ev.created_at)}</td>
            <td>${escapeHTML(ev.user || '-')}</td>
            <td>${escapeHTML(ev.challenge_id || '-')}</td>
            <td class="command-cell">${escapeHTML(stripANSI(ev.last_command)) || '-'}</td>
          `;
          telemetryTableBody.appendChild(row);
        });
        if (!events || events.length === 0) {
          telemetryTableBody.innerHTML = '<tr><td colspan="4" class="muted">No accepted submissions yet.</td></tr>';
        }
      }

      telemetrySection.classList.remove('hidden');
    } catch (e) {
      console.warn('failed to load telemetry', e);
    }
  }

  async function saveCTF() {
    const user = JSON.parse(localStorage.getItem('vmrunner_user') || '{}');
    const ctfTitle = document.getElementById('ctf-title').value.trim();
    if (!ctfTitle) throw new Error('CTF name is required.');

    const challenges = collectChallenges();
    const imagePath = await uploadQCOW2();
    const ctf = {
      title: ctfTitle,
      visibility: document.getElementById('ctf-visibility').value,
      status: document.getElementById('ctf-status').value,
      maker: editID && existingCTF ? existingCTF.maker : user.username || 'unknown',
      vm_config: {
        image_path: imagePath,
        display_type: document.getElementById('vm-display').value
      },
      challenges
    };

    try {
      saveBtn.disabled = true;
      saveBtn.textContent = editID ? 'Saving changes...' : 'Saving...';
      const resp = await fetch(editID ? `${APP_URL}/api/ctfs/${encodeURIComponent(editID)}` : `${APP_URL}/api/ctfs`, {
        method: editID ? 'PUT' : 'POST',
        headers: authHeaders({ 'Content-Type': 'application/json' }),
        body: JSON.stringify(ctf)
      });
      if (resp.ok) {
        const saved = await resp.json();
        alert(editID ? 'CTF updated.' : `CTF saved as ${saved.id}`);
        window.location.href = `/ctf/?id=${encodeURIComponent(saved.id)}`;
      } else {
        const err = await resp.text();
        alert('Error saving CTF: ' + err);
      }
    } catch (e) {
      alert('Network error: ' + e.message);
    } finally {
      saveBtn.disabled = false;
      saveBtn.textContent = editID ? 'Save Changes' : 'Save CTF';
    }
  }

  async function uploadQCOW2() {
    const file = uploadInput.files && uploadInput.files[0];
    if (!file) {
      if (editID && existingCTF?.vm_config?.image_path) return existingCTF.vm_config.image_path;
      throw new Error('Choose a base qcow2 file first.');
    }
    if (!file.name.toLowerCase().endsWith('.qcow2')) {
      throw new Error('Base image must be a .qcow2 file.');
    }

    uploadStatus.textContent = `Uploading ${file.name}...`;
    const form = new FormData();
    form.append('image', file);
    const resp = await fetch(`${APP_URL}/api/uploads/qcow2`, {
      method: 'POST',
      headers: authHeaders(),
      body: form
    });
    if (!resp.ok) {
      const err = await resp.text();
      throw new Error(err || 'Upload failed');
    }
    const payload = await resp.json();
    uploadStatus.textContent = `Uploaded ${file.name}`;
    return payload.image_path;
  }

  async function loadExistingCTF() {
    if (!editID) {
      addChallenge();
      return;
    }

    document.querySelector('.page-header h2').textContent = 'Edit CTF';
    document.querySelector('.page-header .muted').textContent = editID;
    saveBtn.textContent = 'Save Changes';
    uploadInput.required = false;

    const resp = await fetch(`${APP_URL}/api/ctfs/${encodeURIComponent(editID)}`, { headers: authHeaders() });
    if (!resp.ok) throw new Error('Failed to load CTF for editing.');
    existingCTF = await resp.json();
    if (!canEditCTF(existingCTF)) {
      alert('Only the original creator can edit this CTF.');
      window.location.href = '/';
      return;
    }

    document.getElementById('ctf-title').value = existingCTF.title || '';
    document.getElementById('ctf-visibility').value = existingCTF.visibility || 'private';
    document.getElementById('ctf-status').value = existingCTF.status || 'draft';
    document.getElementById('vm-display').value = existingCTF.vm_config?.display_type || 'terminal';
    uploadStatus.textContent = existingCTF.vm_config?.image_path ? `Current image: ${existingCTF.vm_config.image_path}` : '';

    ctfList.innerHTML = '';
    challengeCount = 0;
    (existingCTF.challenges || []).forEach(addChallenge);
    if (challengeCount === 0) addChallenge();

    await loadTelemetry();
  }

  addBtn.addEventListener('click', () => addChallenge());
  saveBtn.addEventListener('click', async () => {
    try {
      saveBtn.disabled = true;
      await saveCTF();
    } catch (e) {
      alert(e.message || 'Failed to save CTF');
      saveBtn.disabled = false;
      saveBtn.textContent = editID ? 'Save Changes' : 'Save CTF';
    }
  });

  loadExistingCTF().catch(e => {
    alert(e.message || 'Failed to load maker.');
    window.location.href = '/';
  });
});
