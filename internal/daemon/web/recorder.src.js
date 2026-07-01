import { marked } from 'marked';
import DOMPurify from 'dompurify';

// voci PTT recorder
// APIs: GET /api/context, POST /api/voice/transcribe, POST /api/voice/emit
(function () {
  // ── Token auth ───────────────────────────────────────────
  var STORAGE_KEY = 'voci_token';
  // Set to true when the server's initial probe returns 401 (--share mode).
  // Gates all subsequent API calls until a valid token is stored.
  var authRequired = false;
  var pollingStarted = false;

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

  function showTokenOverlay() {
    var setup = document.getElementById('voci-token-setup');
    if (setup) setup.style.display = 'flex';
  }

  function hideTokenOverlay() {
    var setup = document.getElementById('voci-token-setup');
    if (setup) setup.style.display = 'none';
  }

  function startPolling() {
    if (pollingStarted) return;
    pollingStarted = true;
    refreshContext();
    pollIntervalId = setInterval(refreshContext, C_CONFIG.contextPollMs);
  }

  // Fetches D-class VAD tuning values once (not polled) and overrides the local
  // fallback defaults. If unreachable (e.g. an older server without /api/config),
  // VAD_THRESHOLD/MIN_AUDIO_MS keep their hardcoded fallback values.
  function fetchConfig() {
    apiFetch('/api/config')
      .then(function(r) { return r.json(); })
      .then(function(resp) {
        if (typeof resp.vadThreshold === 'number' && resp.vadThreshold > 0) {
          VAD_THRESHOLD = resp.vadThreshold;
        }
        if (typeof resp.minAudioMs === 'number' && resp.minAudioMs > 0) {
          MIN_AUDIO_MS = resp.minAudioMs;
        }
      })
      .catch(function() {});
  }

  // Probes /api/context without a token to determine whether the server requires
  // Bearer auth (--share mode returns 401). In local mode (no --share) it returns
  // 200 and we proceed without prompting for a token at all.
  // If a token is already stored we skip the probe and use it directly.
  function initAuth() {
    var tok = getToken();
    if (tok) {
      refreshContext();
      fetchConfig();
      startPolling();
      return;
    }
    fetch('/api/context')
      .then(function(r) {
        if (r.status === 401) {
          authRequired = true;
          showTokenOverlay();
        } else {
          r.json().then(function(resp) {
            setConnected(true);
            var hint = resp.hint || '';
            var dlg = resp.dialogue || [];
            var dlgJson = JSON.stringify(dlg);
            if (hint !== lastHint || dlgJson !== lastDialogueJson) {
              lastHint = hint; lastDialogue = dlg; lastDialogueJson = dlgJson;
              renderContext(hint);
            }
            lastRefresh = Date.now();
          }).catch(function() {});
          fetchConfig();
          startPolling();
        }
      })
      .catch(function() { setConnected(false); startPolling(); });
  }

  function saveToken() {
    var input = document.getElementById('voci-token');
    if (!input) return;
    var val = input.value.trim();
    if (val) {
      localStorage.setItem(STORAGE_KEY, val);
      hideTokenOverlay();
      refreshContext();
      fetchConfig();
      startPolling();
    }
  }

  // expose saveToken globally for the inline onclick handler
  window.saveToken = saveToken;

  // ── VAD constants ────────────────────────────────────────
  var VAD_THRESHOLD  = 0.01;  // RMS below this is considered silence
  var MIN_AUDIO_MS   = 300;   // recordings shorter than this are discarded

  var phase = 'idle'; // idle | recording | processing
  // ── C-class config ─────────────────────────────────────────
  // UX / timing constants resolved from URL → localStorage → default.
  var PARAM_DESCRIPTORS = {
    contextPollMs:   { default: 5000, min: 500,  max: 60000 },
    statusHideMs:    { default: 2000, min: 100,  max: 30000 },
    entitySlice:     { default: 6,    min: 1,    max: 100   },
    taskPillSlice:   { default: 4,    min: 1,    max: 100   },
    taskListSlice:   { default: 6,    min: 1,    max: 100   },
    localMsgCap:     { default: 40,   min: 1,    max: 1000  },
    postEmitDelayMs: { default: 600,  min: 100,  max: 30000 },
  };

  var C_CONFIG = {};

  function resolveConfig() {
    var cfg = {};
    var params;
    try { params = new URLSearchParams(window.location.search); } catch (e) { params = { get: function() { return null; } }; }
    Object.keys(PARAM_DESCRIPTORS).forEach(function (name) {
      var desc = PARAM_DESCRIPTORS[name];
      var val = desc.default;

      // 1. URL query param
      var urlVal = params.get(name);
      if (urlVal !== null) {
        var n = parseInt(urlVal, 10);
        if (!isNaN(n)) val = n;
      } else {
        // 2. localStorage (voci_c_ prefix)
        var lsVal = localStorage.getItem('voci_c_' + name);
        if (lsVal !== null) {
          var nls = parseInt(lsVal, 10);
          if (!isNaN(nls)) val = nls;
        }
      }

      // Clamp to [min, max]
      if (val < desc.min) val = desc.min;
      if (desc.max && val > desc.max) val = desc.max;

      cfg[name] = val;
    });
    return cfg;
  }

  var pollIntervalId = null;

  function restartPolling() {
    if (pollIntervalId) { clearInterval(pollIntervalId); pollIntervalId = null; }
    if (pollingStarted) pollIntervalId = setInterval(refreshContext, C_CONFIG.contextPollMs);
  }

  var isRecording = false;
  var chunks = [], recorder = null, mediaStream = null;
  var timerSecs = 0, timerInterval = null;
  var insertAt = 0;        // cursor position captured just before processing begins
  var statusTimeout = null; // timer for auto-hiding #voci-status
  var recStartMs = 0;
  // Cancel in-flight ASR
  var currentController = null;
  var contextExpanded = false;
  var lastRefresh = Date.now();
  var lastHint = null;
  var lastDialogue = [];   // structured dialogue turns from /api/context (full Markdown)
  var lastDialogueJson = '';  // dedup guard for dialogue changes
  var lastDialogueHtml = '';
  var lastPillsHtml = '';
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
  var cancelProcBtn    = $('cancel-processing-btn');
  var processingDots   = $('processing-dots');
  var composeEl        = $('voci-compose');
  var timerEl          = $('timer-str');
  var statusEl         = $('voci-status');
  var clearBtn         = $('clear-btn');

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
    d(cancelProcBtn,  proc ? 'flex'  : 'none');
  }

  function updateSendBtn() {
    var has = composeEl.value.trim().length > 0;
    sendBtn.style.background  = has ? '#0e1e32' : '#090c15';
    sendBtn.style.borderColor = has ? '#1a3050' : '#0f1522';
    sendBtn.style.color       = has ? '#5b9cf6' : '#252f42';
    sendBtn.style.cursor      = has ? 'pointer' : 'default';
  }

  function showStatus(msg) {
    if (!statusEl) return;
    statusEl.textContent = msg;
    statusEl.style.display = 'block';
    if (statusTimeout) clearTimeout(statusTimeout);
    statusTimeout = setTimeout(function () {
      statusEl.style.display = 'none';
      statusTimeout = null;
    }, C_CONFIG.statusHideMs);
  }

  function updateClearBtn() {
    if (!clearBtn) return;
    clearBtn.style.display = composeEl.value.length > 0 ? 'inline-block' : 'none';
  }

  function pad(n) { return String(n).padStart(2, '0'); }
  function fmtTimer(s) { return Math.floor(s / 60) + ':' + pad(s % 60); }

  function esc(s) {
    return String(s)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }

  marked.setOptions({ gfm: true, breaks: false });

  function mdToHtml(text) {
    return DOMPurify.sanitize(marked.parse(text));
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

    var eLines = entSection  ? entSection.split('\n').filter(Boolean)  : [];
    var tLines = taskSection ? taskSection.split('\n').filter(Boolean) : [];

    entitiesCount.textContent = eLines.length + ' entities';

    entitiesList.innerHTML = eLines.slice(0, C_CONFIG.entitySlice).map(function (line) {
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

    var newPillsHtml = tLines.slice(0, C_CONFIG.taskPillSlice).map(function (line, i) {
      var m = line.match(/TASK-\d+/i);
      var id = m ? m[0].toUpperCase() : 'T' + (i + 1);
      var c  = TASK_COLORS[i % TASK_COLORS.length];
      return '<div style="display:flex;align-items:center;gap:3px;flex-shrink:0">' +
        '<div style="width:5px;height:5px;border-radius:50%;background:' + c + ';box-shadow:0 0 4px ' + c + '55"></div>' +
        '<span style="font-family:JetBrains Mono,monospace;font-size:9.5px;color:#4a6080">' + esc(id) + '</span>' +
        '</div>';
    }).join('<span style="color:#283848;font-size:9px">·</span>');
    if (newPillsHtml !== lastPillsHtml) { lastPillsHtml = newPillsHtml; taskPills.innerHTML = newPillsHtml; }

    tasksList.innerHTML = tLines.slice(0, C_CONFIG.taskListSlice).map(function (line, i) {
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
    // Dialogue turns come from the structured /api/context "dialogue" field,
    // preserving full Markdown (tables, code blocks, blank lines). Each turn's
    // text is rendered verbatim via marked + DOMPurify in renderDialogue().
    var ctxMsgs = lastDialogue.map(function (m) {
      return { role: m.role, text: m.text, time: time };
    });
    var ctxSet  = new Set(ctxMsgs.map(function (m) { return m.text; }));
    var pending = localMessages.filter(function (m) { return !ctxSet.has(m.text); });
    renderDialogue(ctxMsgs.concat(pending));
  }

  function renderDialogue(msgs) {
    var html;
    if (!msgs.length) {
      html = '<div style="display:flex;flex-direction:column;align-items:center;justify-content:center;height:100%;gap:6px;padding:40px 0;opacity:0.4">' +
        '<svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="#5a7090" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"></path></svg>' +
        '<span style="font-size:11px;color:#4a6080;letter-spacing:0.04em">No messages yet</span>' +
        '</div>';
    } else {
      html = msgs.map(function (msg) {
        if (msg.role === 'user') {
          // User turns render through the same marked + DOMPurify pipeline as
          // assistant turns, so dictated/pasted Markdown (paragraphs, tables,
          // lists) segments correctly instead of collapsing to a run-on.
          return '<div style="display:grid;grid-template-columns:38px 28px 1fr;padding:3px 15px;align-items:baseline">' +
            '<span style="font-family:JetBrains Mono,monospace;font-size:9.5px;color:#3d5070;text-align:right;padding-right:8px">' + esc(msg.time) + '</span>' +
            '<span style="font-family:JetBrains Mono,monospace;font-size:9.5px;color:#5a7aaa;font-weight:500">you</span>' +
            '<span style="font-size:12.5px;color:#a8bedc;line-height:1.5">' + mdToHtml(msg.text) + '</span>' +
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
          '<span style="font-size:12.5px;color:#e4eaf5;line-height:1.5">' + mdToHtml(msg.text) + '</span>' +
          '</div>' + evHtml + '</div>';
      }).join('');
    }
    if (html === lastDialogueHtml) return;
    lastDialogueHtml = html;
    dialogueFeed.innerHTML = html;
    requestAnimationFrame(function () { dialogueFeed.scrollTop = dialogueFeed.scrollHeight; });
  }

  function setConnected(ok) {
    var c = ok ? '#22c55e' : '#ef4444';
    connDot.style.background = c;
    connDot.style.boxShadow  = '0 0 5px ' + c;
  }

  function refreshContext() {
    if (authRequired && !getToken()) return;
    apiFetch('/api/context')
      .then(function(r) {
        if (r.status === 401) {
          authRequired = true;
          showTokenOverlay();
          return null;
        }
        return r.json();
      })
      .then(function(resp) {
        if (!resp) return;
        setConnected(true);
        var hint = resp.hint || '';
        var dlg = resp.dialogue || [];
        var dlgJson = JSON.stringify(dlg);
        if (hint !== lastHint || dlgJson !== lastDialogueJson) {
          lastHint = hint;
          lastDialogue = dlg;
          lastDialogueJson = dlgJson;
          renderContext(hint);
        }
        lastRefresh = Date.now();
      })
      .catch(function() { setConnected(false); });
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
          // Discard recordings that are too short to contain meaningful speech.
          if (Date.now() - recStartMs < MIN_AUDIO_MS) {
            setPhase('idle');
            return;
          }
          var blob = new Blob(chunks, { type: recorder.mimeType || 'audio/webm' });
          processAudio(blob);
        };
        recorder.start();
        recStartMs = Date.now();
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
    // Capture where to insert the transcription result in compose.
    insertAt = (composeEl.selectionStart != null) ? composeEl.selectionStart : composeEl.value.length;
    setPhase('processing');
    if (recorder && recorder.state === 'recording') recorder.stop();
  }

  function doTranscribe(blob) {
    currentController = new AbortController();
    apiFetch('/api/voice/transcribe', { method: 'POST', body: blob, signal: currentController.signal })
      .then(function (r) { return r.json(); })
      .then(function (p) {
        currentController = null;
        var rew   = p.Rewritten || '';

        if (!rew) {
          showStatus('未识别到有效内容');
        } else if (rew) {
          // Append at saved cursor position, adding a space separator if needed.
          var before = composeEl.value.slice(0, insertAt);
          var after  = composeEl.value.slice(insertAt);
          var sep    = (before.length > 0 && before[before.length - 1] !== ' ') ? ' ' : '';
          var inserted = sep + rew;
          composeEl.value = before + inserted + after;
          // Move cursor to end of inserted text.
          var newPos = insertAt + inserted.length;
          composeEl.setSelectionRange(newPos, newPos);
          updateSendBtn();
          updateClearBtn();
        }
        setPhase('idle');
      })
      .catch(function (e) {
        currentController = null;
        // AbortError means the user cancelled — return to idle silently.
        if (e && e.name === 'AbortError') { setPhase('idle'); return; }
        console.error('transcribe:', e);
        setPhase('idle');
      });
  }

  function processAudio(blob) {
    blob.arrayBuffer().then(function(buf) {
      var tmpCtx = new (window.AudioContext || window.webkitAudioContext)();
      tmpCtx.decodeAudioData(buf, function(decoded) {
        var data = decoded.getChannelData(0);
        var sum = 0;
        for (var i = 0; i < data.length; i++) sum += data[i] * data[i];
        var rms = Math.sqrt(sum / data.length);
        var hasSpeech = rms >= VAD_THRESHOLD;
        tmpCtx.close();
        if (!hasSpeech) {
          setPhase('idle');
          showStatus('未检测到语音');
          return;
        }
        doTranscribe(blob);
      }, function() {
        // decodeAudioData failed — fall through to ASR
        doTranscribe(blob);
      });
    }).catch(function() {
      doTranscribe(blob);
    });
    return; // async path takes over
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
        if (localMessages.length > C_CONFIG.localMsgCap) localMessages.splice(0, localMessages.length - C_CONFIG.localMsgCap);
        composeEl.value = '';
        updateSendBtn();
        updateClearBtn();
        setPhase('idle');
        // Re-render immediately with current hint so local messages appear at once,
        // without waiting for a hint change on the next /api/context poll.
        renderContext(lastHint || '');
        setTimeout(refreshContext, C_CONFIG.postEmitDelayMs);
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

  composeEl.addEventListener('input', function () { updateSendBtn(); updateClearBtn(); });
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

  clearBtn.addEventListener('click', function () {
    composeEl.value = '';
    composeEl.focus();
    updateSendBtn();
    updateClearBtn();
  });

  $('mic-btn').addEventListener('mousedown', startRec);
  $('mic-btn').addEventListener('touchstart', function (e) {
    e.preventDefault();
    // Blur any focused input first so the virtual keyboard collapses and the
    // page layout stabilises before recording starts; without this the keyboard
    // dismissal shifts the bottom bar mid-press, causing the button to slip
    // away from the user's finger on mobile.
    if (document.activeElement && document.activeElement !== document.body) {
      document.activeElement.blur();
    }
    requestAnimationFrame(startRec);
  });
  cancelRecBtn.addEventListener('click', function () { stopRec(false); });
  cancelProcBtn.addEventListener('click', function () {
    if (currentController) currentController.abort();
  });

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

  // ── Test helpers (used only by Playwright e2e tests) ─────
  // Exposed on window so tests can invoke processAudio without a real microphone.
  window.__voiceTest = {
    processAudio: processAudio,
    // Simulate what stopRec does before processAudio: capture cursor position.
    captureInsertAt: function () {
      insertAt = (composeEl.selectionStart != null) ? composeEl.selectionStart : composeEl.value.length;
    },
    // Render messages directly into the dialogue feed (used by E2E tests).
    injectMessages: function (msgs) {
      renderDialogue(msgs);
    },
    // Pure Markdown→sanitized-HTML transform (marked + DOMPurify), exposed so
    // frontend unit tests can assert rendering without a backend.
    mdToHtml: mdToHtml,
    // Current VAD tuning values, reflecting /api/config once fetched (or the
    // hardcoded fallback defaults if unreachable). Used by e2e config tests.
    getVadConfig: function () {
      return { vadThreshold: VAD_THRESHOLD, minAudioMs: MIN_AUDIO_MS };
    },
    // Current C-class resolved config (URL → localStorage → default hierarchy).
    // Used by E2E tests to verify resolution logic.
    getCConfig: function () {
      var copy = {};
      Object.keys(C_CONFIG).forEach(function (k) { copy[k] = C_CONFIG[k]; });
      return copy;
    },
  };

  // ── C-class settings panel ────────────────────────────────

  function populateSettingsPanel() {
    var panel = $('voci-csettings');
    if (!panel) return;
    Object.keys(PARAM_DESCRIPTORS).forEach(function (name) {
      var inp = panel.querySelector('input[name="' + name + '"]');
      if (inp) inp.value = String(C_CONFIG[name]);
    });
  }

  function hideSettings() {
    var panel = $('voci-csettings');
    if (panel) panel.style.display = 'none';
  }

  function toggleSettings() {
    var panel = $('voci-csettings');
    if (!panel) return;
    if (panel.style.display === 'flex') {
      hideSettings();
    } else {
      populateSettingsPanel();
      panel.style.display = 'flex';
    }
  }

  function buildSettingsPanel() {
    // Gear button in status bar (next to refresh button).
    var statusRight = document.querySelector('#voci-root > div:first-child > div:last-child');
    if (statusRight) {
      var gearBtn = document.createElement('button');
      gearBtn.id = 'csettings-gear';
      gearBtn.innerHTML = '&#x2699;';
      gearBtn.title = 'C-class config';
      gearBtn.style.cssText = 'font-size:11px;color:#4a6080;background:none;border:none;cursor:pointer;padding:0 1px;line-height:1;font-family:inherit;';
      gearBtn.addEventListener('click', toggleSettings);
      statusRight.insertBefore(gearBtn, statusRight.firstChild);
    }

    // Settings panel overlay (hidden by default).
    var fields = Object.keys(PARAM_DESCRIPTORS).map(function (name) {
      return '<div style="display:flex;align-items:center;justify-content:space-between;">' +
        '<label style="font-size:10px;color:#4a6080;font-family:JetBrains Mono,monospace;">' + name + '</label>' +
        '<input name="' + name + '" type="number" min="' + PARAM_DESCRIPTORS[name].min + '" style="width:80px;background:#07090d;border:1px solid #131c2e;border-radius:4px;padding:3px 5px;font-size:10px;color:#c8d4e8;font-family:JetBrains Mono,monospace;text-align:right;" />' +
        '</div>';
    }).join('');

    var panel = document.createElement('div');
    panel.id = 'voci-csettings';
    panel.style.cssText = 'display:none;position:fixed;inset:0;background:rgba(7,9,13,0.97);z-index:101;flex-direction:column;align-items:center;justify-content:center;';
    panel.innerHTML = '<div style="background:#0b0e1a;border:1px solid #131c2e;border-radius:12px;padding:20px 24px;max-width:340px;width:90%;">' +
      '<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:14px;">' +
      '<span style="font-size:12px;font-weight:600;color:#e4eaf5;letter-spacing:0.04em;">C-class config</span>' +
      '<button id="voci-csettings-close" style="color:#4a6080;font-size:16px;cursor:pointer;background:none;border:none;padding:0;line-height:1;">&#x2715;</button>' +
      '</div>' +
      '<div style="display:flex;flex-direction:column;gap:7px;margin-bottom:14px;">' + fields + '</div>' +
      '<div style="display:flex;gap:8px;">' +
      '<button id="voci-csettings-save" style="flex:1;padding:6px;border-radius:6px;background:#0e1e32;border:1px solid #1a3050;color:#5b9cf6;font-size:11px;cursor:pointer;font-family:Space Grotesk,sans-serif;">Save</button>' +
      '<button id="voci-csettings-reset" style="flex:1;padding:6px;border-radius:6px;background:none;border:1px solid #131c2e;color:#4a6080;font-size:11px;cursor:pointer;font-family:Space Grotesk,sans-serif;">Reset</button>' +
      '</div></div>';
    document.body.appendChild(panel);

    // Close button
    $('voci-csettings-close').addEventListener('click', hideSettings);

    // Save: write each field to localStorage and re-resolve config.
    $('voci-csettings-save').addEventListener('click', function () {
      Object.keys(PARAM_DESCRIPTORS).forEach(function (name) {
        var inp = panel.querySelector('input[name="' + name + '"]');
        if (inp) {
          var v = parseInt(inp.value, 10);
          if (!isNaN(v)) {
            var desc = PARAM_DESCRIPTORS[name];
            if (v < desc.min) v = desc.min;
            if (desc.max && v > desc.max) v = desc.max;
            localStorage.setItem('voci_c_' + name, String(v));
          }
        }
      });
      C_CONFIG = resolveConfig();
      populateSettingsPanel();
      restartPolling();
    });

    // Reset: remove all voci_c_ keys and re-resolve to defaults.
    $('voci-csettings-reset').addEventListener('click', function () {
      Object.keys(PARAM_DESCRIPTORS).forEach(function (name) {
        localStorage.removeItem('voci_c_' + name);
      });
      C_CONFIG = resolveConfig();
      populateSettingsPanel();
      restartPolling();
    });
  }

  // Keyboard shortcuts: '?' toggles settings, Escape closes it.
  window.addEventListener('keydown', function (e) {
    if (e.key === '?' && e.target !== composeEl) {
      e.preventDefault();
      toggleSettings();
      return;
    }
    if (e.key === 'Escape') {
      var panel = $('voci-csettings');
      if (panel && panel.style.display === 'flex') {
        hideSettings();
      }
    }
  });

  // ── Init ─────────────────────────────────────────────────

  C_CONFIG = resolveConfig();
  buildSettingsPanel();
  setPhase('idle');
  updateSendBtn();
  updateClearBtn();
  initAuth();
  setInterval(function () {
    var s = Math.floor((Date.now() - lastRefresh) / 1000);
    refreshBtn.textContent = s < 2 ? 'just now' : s < 60 ? s + 's ago' : Math.floor(s / 60) + 'm ago';
  }, 1000);

})();
