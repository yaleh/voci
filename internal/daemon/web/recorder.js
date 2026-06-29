// voci PTT recorder
// APIs: GET /api/context, POST /api/voice/transcribe, POST /api/voice/emit
(function () {
  // ── Token auth ───────────────────────────────────────────
  var STORAGE_KEY = 'voci_token';

  function getToken() {
    return localStorage.getItem(STORAGE_KEY) || '';
  }

  function apiFetch(url, opts) {
    opts = opts || {};
    opts.headers = opts.headers || {};
    var tok = getToken();
    if (tok) opts.headers['Authorization'] = 'Bearer ' + tok;
    return fetch(url, opts);
  }

  function checkToken() {
    var setup = document.getElementById('voci-token-setup');
    if (setup) {
      if (!getToken()) {
        setup.style.display = 'flex';
      } else {
        setup.style.display = 'none';
      }
    }
  }

  function saveToken() {
    var input = document.getElementById('voci-token');
    if (!input) return;
    var val = input.value.trim();
    if (val) {
      localStorage.setItem(STORAGE_KEY, val);
      var setup = document.getElementById('voci-token-setup');
      if (setup) setup.style.display = 'none';
    }
  }

  // expose saveToken globally for the inline onclick handler
  window.saveToken = saveToken;

  var phase = 'idle'; // idle | recording | processing
  var isRecording = false;
  var chunks = [], recorder = null, mediaStream = null;
  var timerSecs = 0, timerInterval = null;
  var contextExpanded = false;
  var lastRefresh = Date.now();
  var localMessages = [];

  function $(id) { return document.getElementById(id); }

  var refreshBtn       = $('refresh-btn');
  var connDot          = $('conn-dot');
  var taskPills        = $('task-pills');
  var entitiesCount    = $('entities-count');
  var contextChevron   = $('context-chevron');
  var contextPanel     = $('context-panel');
  var entitiesList     = $('entities-list');
  var tasksList        = $('tasks-list');
  var dialogueFeed     = $('voci-dialogue');
  var textInputWrap    = $('text-input-wrap');
  var recordingWrap    = $('recording-wrap');
  var processingWrap   = $('processing-wrap');
  var actionLeftIdle   = $('action-left-idle');
  var actionLeftRec    = $('action-left-recording');
  var actionLeftProc   = $('action-left-processing');
  var sendBtn          = $('send-btn');
  var cancelRecBtn     = $('cancel-recording-btn');
  var processingDots   = $('processing-dots');
  var composeEl        = $('voci-compose');
  var timerEl          = $('timer-str');

  function d(el, v) { el.style.display = v; }

  function setPhase(p) {
    phase = p;
    var rec  = p === 'recording';
    var proc = p === 'processing';
    var text = !rec && !proc;

    d(textInputWrap,  text ? 'block' : 'none');
    d(recordingWrap,  rec  ? 'flex'  : 'none');
    d(processingWrap, proc ? 'flex'  : 'none');

    d(actionLeftIdle, text ? 'flex'  : 'none');
    d(actionLeftRec,  rec  ? 'block' : 'none');
    d(actionLeftProc, proc ? 'block' : 'none');

    d(sendBtn,        text ? 'flex'  : 'none');
    d(cancelRecBtn,   rec  ? 'block' : 'none');
    d(processingDots, proc ? 'flex'  : 'none');
  }

  function updateSendBtn() {
    var has = composeEl.value.trim().length > 0;
    sendBtn.style.background  = has ? '#0e1e32' : '#090c15';
    sendBtn.style.borderColor = has ? '#1a3050' : '#0f1522';
    sendBtn.style.color       = has ? '#5b9cf6' : '#252f42';
    sendBtn.style.cursor      = has ? 'pointer' : 'default';
  }

  function pad(n) { return String(n).padStart(2, '0'); }
  function fmtTimer(s) { return Math.floor(s / 60) + ':' + pad(s % 60); }

  function esc(s) {
    return String(s)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }

  // ── Context ──────────────────────────────────────────────

  var TASK_COLORS = ['#22c55e', '#f97316', '#a855f7', '#5b9cf6', '#06b6d4', '#ec4899'];

  function extractSection(hint, heading) {
    var idx = hint.indexOf(heading);
    if (idx < 0) return '';
    var body = hint.slice(idx + heading.length);
    var next = body.search(/\n## /);
    return (next >= 0 ? body.slice(0, next) : body).trim();
  }

  function renderContext(hint) {
    var entSection  = extractSection(hint, '## Known Entities');
    var taskSection = extractSection(hint, '## Active Tasks');
    var dlgSection  = extractSection(hint, '## Recent Dialogue');

    var eLines = entSection  ? entSection.split('\n').filter(Boolean)  : [];
    var tLines = taskSection ? taskSection.split('\n').filter(Boolean) : [];

    entitiesCount.textContent = eLines.length + ' entities';

    entitiesList.innerHTML = eLines.slice(0, 6).map(function (line) {
      var m = line.match(/"([^"]+)"\s*[-→>]+\s*(.+)/);
      if (m) {
        return '<div style="display:flex;align-items:baseline;gap:4px">' +
          '<span style="font-family:JetBrains Mono,monospace;font-size:9.5px;color:#4a6080;font-style:italic;flex-shrink:0">&quot;' + esc(m[1]) + '&quot;</span>' +
          '<span style="font-size:8px;color:#283848">→</span>' +
          '<span style="font-family:JetBrains Mono,monospace;font-size:9.5px;color:#5a8aba">' + esc(m[2].trim()) + '</span>' +
          '</div>';
      }
      return '<div style="font-family:JetBrains Mono,monospace;font-size:9.5px;color:#4a6080">' + esc(line) + '</div>';
    }).join('');

    taskPills.innerHTML = tLines.slice(0, 4).map(function (line, i) {
      var m = line.match(/TASK-\d+/i);
      var id = m ? m[0].toUpperCase() : 'T' + (i + 1);
      var c  = TASK_COLORS[i % TASK_COLORS.length];
      return '<div style="display:flex;align-items:center;gap:3px;flex-shrink:0">' +
        '<div style="width:5px;height:5px;border-radius:50%;background:' + c + ';box-shadow:0 0 4px ' + c + '55"></div>' +
        '<span style="font-family:JetBrains Mono,monospace;font-size:9.5px;color:#4a6080">' + esc(id) + '</span>' +
        '</div>';
    }).join('<span style="color:#283848;font-size:9px">·</span>');

    tasksList.innerHTML = tLines.slice(0, 6).map(function (line, i) {
      var m = line.match(/TASK-\d+/i);
      var id   = m ? m[0].toUpperCase() : 'T' + (i + 1);
      var c    = TASK_COLORS[i % TASK_COLORS.length];
      var desc = line.replace(/^[-*]\s*/, '').replace(/TASK-\d+\s*:?\s*/i, '').trim();
      return '<div style="display:flex;align-items:center;gap:5px">' +
        '<div style="width:4px;height:4px;border-radius:50%;background:' + c + ';flex-shrink:0"></div>' +
        '<span style="font-family:JetBrains Mono,monospace;font-size:9.5px;color:#5a8aba;flex-shrink:0">' + esc(id) + '</span>' +
        '<span style="font-size:9.5px;color:#3a5070;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">' + esc(desc) + '</span>' +
        '</div>';
    }).join('');

    var now  = new Date();
    var time = pad(now.getHours()) + ':' + pad(now.getMinutes());
    var ctxMsgs = [];
    if (dlgSection) {
      dlgSection.split('\n').filter(Boolean).forEach(function (l) {
        if      (l.startsWith('A: ')) ctxMsgs.push({ role: 'assistant', text: l.slice(3), time: time });
        else if (l.startsWith('U: ')) ctxMsgs.push({ role: 'user',      text: l.slice(3), time: time });
      });
    }
    var ctxSet  = new Set(ctxMsgs.map(function (m) { return m.text; }));
    var pending = localMessages.filter(function (m) { return !ctxSet.has(m.text); });
    renderDialogue(ctxMsgs.concat(pending));
  }

  function renderDialogue(msgs) {
    if (!msgs.length) {
      dialogueFeed.innerHTML =
        '<div style="display:flex;flex-direction:column;align-items:center;justify-content:center;height:100%;gap:6px;padding:40px 0;opacity:0.4">' +
        '<svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="#5a7090" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"></path></svg>' +
        '<span style="font-size:11px;color:#4a6080;letter-spacing:0.04em">No messages yet</span>' +
        '</div>';
      return;
    }
    dialogueFeed.innerHTML = msgs.map(function (msg) {
      if (msg.role === 'user') {
        return '<div style="display:grid;grid-template-columns:38px 28px 1fr;padding:3px 15px;align-items:baseline">' +
          '<span style="font-family:JetBrains Mono,monospace;font-size:9.5px;color:#3d5070;text-align:right;padding-right:8px">' + esc(msg.time) + '</span>' +
          '<span style="font-family:JetBrains Mono,monospace;font-size:9.5px;color:#5a7aaa;font-weight:500">you</span>' +
          '<span style="font-size:12.5px;color:#a8bedc;line-height:1.5">' + esc(msg.text) + '</span>' +
          '</div>';
      }
      var evHtml = '';
      if (msg.events && msg.events.length) {
        evHtml = '<div style="padding:1px 15px 2px;margin-left:66px">' +
          '<span style="font-family:JetBrains Mono,monospace;font-size:10px;color:#3a5880;white-space:pre;display:block;line-height:1.8">' +
          esc(msg.events.join('\n')) + '</span></div>';
      }
      return '<div style="display:flex;flex-direction:column;padding:3px 0;animation:msg-in 0.2s ease">' +
        '<div style="display:grid;grid-template-columns:38px 28px 1fr;padding:0 15px;align-items:baseline">' +
        '<span style="font-family:JetBrains Mono,monospace;font-size:9.5px;color:#3d5070;text-align:right;padding-right:8px">' + esc(msg.time) + '</span>' +
        '<span style="font-family:JetBrains Mono,monospace;font-size:9.5px;color:#d4894a;font-weight:500">cc</span>' +
        '<span style="font-size:12.5px;color:#e4eaf5;line-height:1.5">' + esc(msg.text) + '</span>' +
        '</div>' + evHtml + '</div>';
    }).join('');
    requestAnimationFrame(function () { dialogueFeed.scrollTop = dialogueFeed.scrollHeight; });
  }

  function setConnected(ok) {
    var c = ok ? '#22c55e' : '#ef4444';
    connDot.style.background = c;
    connDot.style.boxShadow  = '0 0 5px ' + c;
  }

  function refreshContext() {
    apiFetch('/api/context')
      .then(function (r) { return r.json(); })
      .then(function (resp) {
        setConnected(true);
        renderContext(resp.hint || '');
        lastRefresh = Date.now();
      })
      .catch(function () { setConnected(false); });
  }

  // ── Recording ────────────────────────────────────────────

  function startRec() {
    if (isRecording || phase !== 'idle') return;
    refreshContext();
    navigator.mediaDevices.getUserMedia({ audio: true })
      .then(function (stream) {
        mediaStream = stream;
        chunks = [];
        recorder = new MediaRecorder(stream);
        recorder.ondataavailable = function (e) { if (e.data.size > 0) chunks.push(e.data); };
        recorder.onstop = function () {
          stream.getTracks().forEach(function (t) { t.stop(); });
          var blob = new Blob(chunks, { type: recorder.mimeType || 'audio/webm' });
          processAudio(blob);
        };
        recorder.start();
        isRecording = true;
        timerSecs = 0;
        timerEl.textContent = fmtTimer(0);
        timerInterval = setInterval(function () {
          timerSecs++;
          timerEl.textContent = fmtTimer(timerSecs);
        }, 1000);
        setPhase('recording');
      })
      .catch(function (err) { console.error('mic:', err); });
  }

  function stopRec(submit) {
    if (!isRecording) return;
    clearInterval(timerInterval);
    isRecording = false;
    if (!submit) {
      if (recorder && recorder.state === 'recording') {
        recorder.onstop = null;
        recorder.stop();
      }
      setPhase('idle');
      return;
    }
    setPhase('processing');
    if (recorder && recorder.state === 'recording') recorder.stop();
  }

  function processAudio(blob) {
    apiFetch('/api/voice/transcribe', { method: 'POST', body: blob })
      .then(function (r) { return r.json(); })
      .then(function (p) {
        var kind  = p.Kind || 'direct_prompt';
        var rew   = p.Rewritten || '';
        var ambig = kind === 'ambiguous';

        composeEl.value = ambig ? '' : rew;
        updateSendBtn();
        setPhase('idle');
      })
      .catch(function (e) { console.error('transcribe:', e); setPhase('idle'); });
  }

  // ── Send ─────────────────────────────────────────────────

  function sendText(text, kind) {
    if (!text) return;
    var now  = new Date();
    var time = pad(now.getHours()) + ':' + pad(now.getMinutes());
    apiFetch('/api/voice/emit', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ text: text, kind: kind || 'direct_prompt' }),
    }).then(function (r) {
      if (r.ok || r.status === 204) {
        localMessages.push({ role: 'user',      text: text,    time: time, events: [] });
        localMessages.push({ role: 'assistant', text: 'On it.', time: time, events: [] });
        if (localMessages.length > 40) localMessages.splice(0, localMessages.length - 40);
        composeEl.value = '';
        updateSendBtn();
        setPhase('idle');
        setTimeout(refreshContext, 600);
      }
    }).catch(function (e) { console.error('emit:', e); setPhase('idle'); });
  }

  // ── Event wiring ─────────────────────────────────────────

  refreshBtn.addEventListener('click', refreshContext);

  $('entities-toggle').addEventListener('click', function () {
    contextExpanded = !contextExpanded;
    contextPanel.style.display = contextExpanded ? 'block' : 'none';
    contextChevron.textContent = contextExpanded ? '▾' : '▸';
  });

  composeEl.addEventListener('input', updateSendBtn);
  composeEl.addEventListener('keydown', function (e) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      var t = composeEl.value.trim();
      if (t) sendText(t, 'direct_prompt');
    }
  });
  sendBtn.addEventListener('click', function () {
    var t = composeEl.value.trim();
    if (t) sendText(t, 'direct_prompt');
  });

  $('mic-btn').addEventListener('mousedown', startRec);
  $('mic-btn').addEventListener('touchstart', function (e) { e.preventDefault(); startRec(); });
  cancelRecBtn.addEventListener('click', function () { stopRec(false); });

  window.addEventListener('mouseup',  function ()  { if (isRecording) stopRec(true); });
  window.addEventListener('touchend', function (e) { if (isRecording) { e.preventDefault(); stopRec(true); } });

  var spaceHeld = false;
  window.addEventListener('keydown', function (e) {
    if (e.code === 'Space' && !e.repeat && phase === 'idle' && e.target !== composeEl) {
      e.preventDefault(); spaceHeld = true; startRec();
    }
  });
  window.addEventListener('keyup', function (e) {
    if (e.code === 'Space' && spaceHeld) {
      e.preventDefault(); spaceHeld = false; stopRec(true);
    }
  });

  // ── Init ─────────────────────────────────────────────────

  setPhase('idle');
  updateSendBtn();
  checkToken();
  refreshContext();
  setInterval(refreshContext, 5000);
  setInterval(function () {
    var s = Math.floor((Date.now() - lastRefresh) / 1000);
    refreshBtn.textContent = s < 2 ? 'just now' : s < 60 ? s + 's ago' : Math.floor(s / 60) + 'm ago';
  }, 1000);

})();
