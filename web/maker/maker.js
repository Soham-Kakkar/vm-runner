document.addEventListener('DOMContentLoaded', () => {
  const ctfList = document.getElementById('challenges-list');
  const addBtn = document.getElementById('add-challenge-btn');
  const saveBtn = document.getElementById('save-ctf-btn');
  const template = document.getElementById('challenge-template');
  const uploadInput = document.getElementById('vm-image-file');
  const uploadStatus = document.getElementById('upload-status');
  const APP_URL = window.location.protocol === 'file:' ? 'http://localhost:8080' : '';

  let challengeCount = 0;

  function addChallenge() {
    challengeCount++;
    const clone = template.content.cloneNode(true);
    const container = clone.querySelector('.challenge-editor');
    container.querySelector('.challenge-num').textContent = `Question #${challengeCount}`;
    container.querySelector('.ch-qno').value = String(challengeCount);
    
    const validatorSelect = clone.querySelector('.ch-validator');
    const staticField = clone.querySelector('.static-only');
    const hmacField = clone.querySelector('.hmac-only');

    validatorSelect.addEventListener('change', () => {
      if (validatorSelect.value === 'hmac') {
        staticField.classList.add('hidden');
        hmacField.classList.remove('hidden');
      } else {
        staticField.classList.remove('hidden');
        hmacField.classList.add('hidden');
      }
    });

    clone.querySelector('.remove-btn').addEventListener('click', () => {
      container.remove();
      updateChallengeNumbers();
    });

    ctfList.appendChild(clone);
  }

  function updateChallengeNumbers() {
    const containers = ctfList.querySelectorAll('.challenge-editor');
    challengeCount = containers.length;
    containers.forEach((c, i) => {
      c.querySelector('.challenge-num').textContent = `Question #${i + 1}`;
    });
  }

  async function saveCTF() {
    const user = JSON.parse(localStorage.getItem('vmrunner_user') || '{}');
    const ctfTitle = document.getElementById('ctf-title').value.trim();
    if (!ctfTitle) {
      throw new Error('CTF name is required.');
    }

    const challenges = [];
    const containers = ctfList.querySelectorAll('.challenge-editor');
    containers.forEach(c => {
      const ch = {
        title: c.querySelector('.ch-title').value.trim(),
        description: c.querySelector('.ch-desc').value,
        validator: c.querySelector('.ch-validator').value,
        question_no: parseInt(c.querySelector('.ch-qno').value) || 1
      };
      if (!ch.title) {
        throw new Error('Each question needs a name.');
      }
      if (ch.validator === 'hmac') {
        ch.template = c.querySelector('.ch-template').value;
      } else {
        ch.flag = c.querySelector('.ch-flag').value;
      }
      if (!(ch.template || ch.flag)) {
        throw new Error('Each question needs a flag format.');
      }
      challenges.push(ch);
    });

    const imagePath = await uploadQCOW2();
    const ctf = {
      title: ctfTitle,
      visibility: document.getElementById('ctf-visibility').value,
      status: document.getElementById('ctf-status').value,
      maker: user.username || 'unknown',
      vm_config: {
        image_path: imagePath,
        display_type: document.getElementById('vm-display').value
      },
      challenges
    };

    try {
      saveBtn.disabled = true;
      saveBtn.textContent = 'Saving...';
      const resp = await fetch(`${APP_URL}/api/ctfs`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(ctf)
      });
      if (resp.ok) {
        const saved = await resp.json();
        alert(`CTF saved as ${saved.id}`);
        window.location.href = '/';
      } else {
        const err = await resp.text();
        alert('Error saving CTF: ' + err);
      }
    } catch (e) {
      alert('Network error: ' + e.message);
    } finally {
      saveBtn.disabled = false;
      saveBtn.textContent = 'Save CTF';
    }
  }

  async function uploadQCOW2() {
    const file = uploadInput.files && uploadInput.files[0];
    if (!file) {
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

  addBtn.addEventListener('click', addChallenge);
  saveBtn.addEventListener('click', async () => {
    try {
      saveBtn.disabled = true;
      await saveCTF();
    } catch (e) {
      alert(e.message || 'Failed to save CTF');
      saveBtn.disabled = false;
      saveBtn.textContent = 'Save CTF';
    }
  });

  // Add one challenge by default
  addChallenge();
});
