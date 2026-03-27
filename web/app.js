document.addEventListener('DOMContentLoaded', () => {
    const challengeList = document.getElementById('challenge-list');
    const sessionView = document.getElementById('session-view');
    const sessionChallengeName = document.getElementById('session-challenge-name');
    const questionsContainer = document.getElementById('questions-container');
    const endSessionBtn = document.getElementById('end-session-btn');
    const terminalContainer = document.getElementById('terminal');
    const vncContainer = document.getElementById('vnc-container');
    const vncScreen = document.getElementById('vnc-screen');

    let term;
    let ws;
    let rfb;
    let termListener = null;
    let currentSession = null;

    // Initialize Terminal
    function initTerminal(container) {
        const terminal = new Terminal({ cursorBlink: true, theme: { background: '#1a1a1a' } });
        const fitAddon = new FitAddon.FitAddon();
        terminal.loadAddon(fitAddon);
        terminal.open(container);
        fitAddon.fit();
        window.addEventListener('resize', () => fitAddon.fit());
        return terminal;
    }

    // Fetch and display challenges
    async function loadChallenges() {
        try {
            const response = await fetch('/api/challenges');
            const challenges = await response.json();
            challengeList.innerHTML = '';
            challenges.forEach(challenge => {
                const li = document.createElement('li');
                li.textContent = challenge.name;
                li.dataset.challengeId = challenge.id;
                li.addEventListener('click', () => startSession(challenge.id, challenge.name));
                challengeList.appendChild(li);
            });
        } catch (error) {
            console.error('Failed to load challenges:', error);
        }
    }

    // Start a new session or reconnect to an existing one
    async function startSession(challengeId, challengeName) {
        try {
            // Check if there's an active session first
            const checkResponse = await fetch('/api/sessions/current');
            const activeSession = await checkResponse.json();

            if (activeSession) {
                if (activeSession.challenge_id === challengeId) {
                    // It's the same challenge, just show it
                    showSession(activeSession, challengeName);
                    return;
                } else {
                    alert('A session for another challenge is already active. Please end it first.');
                    return;
                }
            }

            // No active session, start a new one
            const response = await fetch('/api/sessions', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ challenge_id: challengeId }),
            });
            const sessionData = await response.json();
            showSession(sessionData, challengeName);
        } catch (error) {
            console.error('Failed to start session:', error);
        }
    }

    function showSession(sessionData, challengeName) {
        currentSession = sessionData;
        sessionChallengeName.textContent = challengeName;
        displayQuestions(sessionData.questions);
        
        // Destroy existing terminal if any
        if (term) {
            if (termListener) {
                try { termListener.dispose(); } catch (e) {}
                termListener = null;
            }
            term.dispose();
            terminalContainer.innerHTML = '';
        }

        // Initialize new terminal for a clean state
        term = initTerminal(terminalContainer);

        // Highlight active challenge in list
        document.querySelectorAll('#challenge-list li').forEach(li => {
            if (li.dataset.challengeId === sessionData.challenge_id) {
                li.classList.add('active');
            } else {
                li.classList.remove('active');
            }
        });

        // Clear and setup log
        const log = document.getElementById('vm-log');
        if (log) log.value = '';

        if (sessionData.display_type === 'vnc') {
            terminalContainer.classList.add('hidden');
            vncContainer.classList.remove('hidden');
            setupVNC(sessionData.vnc_url);
        } else {
            terminalContainer.classList.remove('hidden');
            vncContainer.classList.add('hidden');
            if (rfb) {
                rfb.disconnect();
                rfb = null;
            }
        }

        setupWebSocket(sessionData.websocket_url);
        sessionView.classList.remove('hidden');
    }

    function setupVNC(vncUrl) {
        if (rfb) {
            rfb.disconnect();
        }

        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const url = `${protocol}//${window.location.host}${vncUrl}`;
        
        // RFB is loaded via a module script in index.html
        const initRFB = () => {
            if (!window.RFB) {
                setTimeout(initRFB, 100);
                return;
            }
            rfb = new window.RFB(vncScreen, url);
            rfb.scaleViewport = true;
            rfb.resizeSession = true;

            rfb.addEventListener('connect', () => {
                console.log('VNC Connected successfully');
            });
            rfb.addEventListener('disconnect', (e) => {
                console.log('VNC Disconnected', e);
            });
            rfb.addEventListener('securityfailure', (e) => {
                console.error('VNC Security Failure', e);
            });

            // Handle clipboard from VM
            rfb.addEventListener('clipboard', (e) => {
                const text = e.detail.text;
                if (navigator.clipboard && navigator.clipboard.writeText) {
                    navigator.clipboard.writeText(text).catch(err => {
                        console.error('Failed to copy from VM to host clipboard:', err);
                    });
                }
            });
        };
        initRFB();
    }

    // Display questions
    function displayQuestions(questions) {
        questionsContainer.innerHTML = '';
        questions.forEach(q => {
            const qDiv = document.createElement('div');
            qDiv.id = `question-${q.id}`;
            qDiv.classList.add('question');
            if (q.is_completed) {
                qDiv.classList.add('completed');
            }
            qDiv.innerHTML = `
                <p>${q.text}</p>
                <input type="text" id="answer-${q.id}" placeholder="Enter flag..." ${q.is_completed ? 'disabled' : ''}>
                <button data-question-id="${q.id}" ${q.is_completed ? 'disabled' : ''}>Submit</button>
            `;
            questionsContainer.appendChild(qDiv);
        });

        questionsContainer.querySelectorAll('button').forEach(btn => {
            if (!btn.disabled) {
                btn.addEventListener('click', () => {
                    const questionId = btn.dataset.questionId;
                    const answer = document.getElementById(`answer-${questionId}`).value;
                    submitAnswer(questionId, answer);
                });
            }
        });
    }

    // Submit an answer
    async function submitAnswer(questionId, answer) {
        try {
            const response = await fetch(`/api/sessions/${currentSession.session_id}/submit`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ answer }),
            });
            const result = await response.json();
            if (result.correct) {
                const qDiv = document.getElementById(`question-${questionId}`);
                qDiv.classList.add('completed');
                qDiv.querySelector('input').disabled = true;
                qDiv.querySelector('button').disabled = true;
                alert(`Question completed!`);
            } else {
                alert('Incorrect answer.');
            }
        } catch (error) {
            console.error('Failed to submit answer:', error);
        }
    }

    // Setup WebSocket connection
    function setupWebSocket(wsUrl) {
        if (ws) {
            ws.close();
        }

        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        ws = new WebSocket(`${protocol}//${window.location.host}${wsUrl}`);

        ws.onopen = () => {
            // ensure we only have one onData listener
            if (termListener) {
                try { termListener.dispose(); } catch (e) {}
                termListener = null;
            }
            termListener = term.onData(data => {
                if (ws.readyState === WebSocket.OPEN) {
                    ws.send(JSON.stringify({ type: 'input', payload: data }));
                }
            });
        };

        ws.onmessage = (event) => {
            const msg = JSON.parse(event.data);
            switch (msg.type) {
                case 'vm_output':
                    const output = msg.payload;
                    term.write(output);
                    term.scrollToBottom();

                    const log = document.getElementById('vm-log');
                    if (log) {
                        const ansiRegex = /[\u001b\u009b][[()#;?]*(?:[0-9]{1,4}(?:;[0-9]{0,4})*)?[0-9A-ORZcf-nqry=><]/g;
                        const cleanOutput = msg.payload.replace(ansiRegex, '');
                        
                        // Limit log size to ~10,000 chars for performance
                        const newLog = (log.value + cleanOutput).slice(-10000);
                        log.value = newLog;
                        log.scrollTop = log.scrollHeight;
                    }
                    break;
                case 'flag_found':
                    const qId = msg.payload.question_id;
                    const qDiv = document.getElementById(`question-${qId}`);
                    if (qDiv) {
                        qDiv.classList.add('completed');
                        qDiv.querySelector('input').disabled = true;
                        qDiv.querySelector('button').disabled = true;
                    }
                    alert(`Flag detected automatically for question ${qId}!`);
                    break;
                case 'error':
                    term.write(`\n\r[ERROR: ${msg.payload}]\n\r`);
                    break;
            }
        };

        ws.onclose = () => {
            term.write(`\n\r[WebSocket connection closed]\n\r`);
            if (termListener) {
                try { termListener.dispose(); } catch (e) {}
                termListener = null;
            }
        };
    }
    
    // End the current session
    function endSession() {
        if (!currentSession) return;
        // Tell server to end the session, then cleanup client-side state.
        fetch(`/api/sessions/${currentSession.session_id}/end`, { method: 'POST' }).catch(() => {} ).finally(() => {
            if (ws) ws.close();
            if (rfb) {
                rfb.disconnect();
                rfb = null;
            }

            // Remove active highlight from challenge list
            document.querySelectorAll('#challenge-list li').forEach(li => li.classList.remove('active'));

            if (term) {
                if (termListener) {
                    try { termListener.dispose(); } catch (e) {}
                    termListener = null;
                }
                term.dispose();
                term = null;
                terminalContainer.innerHTML = '';
            }

            currentSession = null;
            sessionView.classList.add('hidden');
        });
        
        // Session end requested on server as well.
    }

    endSessionBtn.addEventListener('click', endSession);

    // Initial load
    loadChallenges();
});
