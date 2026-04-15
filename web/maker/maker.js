document.addEventListener('DOMContentLoaded', () => {
  const ctfList = document.getElementById('challenges-list');
  const addBtn = document.getElementById('add-challenge-btn');
  const saveBtn = document.getElementById('save-ctf-btn');
  const template = document.getElementById('challenge-template');

  let challengeCount = 0;

  function addChallenge() {
    challengeCount++;
    const clone = template.content.cloneNode(true);
    const container = clone.querySelector('.challenge-editor');
    container.querySelector('.challenge-num').textContent = `Challenge #${challengeCount}`;
    
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
      c.querySelector('.challenge-num').textContent = `Challenge #${i + 1}`;
    });
  }

  async function saveCTF() {
    const user = JSON.parse(localStorage.getItem('vmrunner_user') || '{}');
    const ctf = {
      id: document.getElementById('ctf-id').value,
      title: document.getElementById('ctf-title').value,
      visibility: document.getElementById('ctf-visibility').value,
      status: document.getElementById('ctf-status').value,
      maker: user.username || 'unknown',
      vm_config: {
        image_path: document.getElementById('vm-image').value,
        memory_mb: parseInt(document.getElementById('vm-memory').value) || 1024,
        display_type: document.getElementById('vm-display').value,
        architecture: "x86_64",
        image_format: "qcow2"
      },
      challenges: []
    };

    const containers = ctfList.querySelectorAll('.challenge-editor');
    containers.forEach(c => {
      const ch = {
        id: c.querySelector('.ch-id').value,
        title: c.querySelector('.ch-title').value,
        description: c.querySelector('.ch-desc').value,
        validator: c.querySelector('.ch-validator').value,
        question_no: parseInt(c.querySelector('.ch-qno').value) || 1
      };
      if (ch.validator === 'hmac') {
        ch.template = c.querySelector('.ch-template').value;
      } else {
        ch.flag = c.querySelector('.ch-flag').value;
      }
      ctf.challenges.push(ch);
    });

    try {
      const resp = await fetch('/api/ctfs', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(ctf)
      });
      if (resp.ok) {
        alert('CTF Saved successfully!');
        window.location.href = '/';
      } else {
        const err = await resp.text();
        alert('Error saving CTF: ' + err);
      }
    } catch (e) {
      alert('Network error: ' + e.message);
    }
  }

  addBtn.addEventListener('click', addChallenge);
  saveBtn.addEventListener('click', saveCTF);

  // Add one challenge by default
  addChallenge();
});
