// voci PTT recorder
// Contracts: /api/voice/transcribe, /api/voice/emit, /api/context, MediaRecorder
// Fields from transcribe: Rewritten, RawTranscript, Kind, Confidence (ActionProposal — no json tags → capitalized)
// Fields to emit: { text: ..., "kind": ... }
(function() {
  var chunks = [];
  var recorder = null;
  var previewedKind = 'direct_prompt';
  var mediaStream = null;
  var ctxTimer = null;

  var statusEl = document.getElementById('status');
  var previewEl = document.getElementById('preview');
  var rawEl = document.getElementById('raw');
  var rewrittenEl = document.getElementById('rewritten');
  var kindEl = document.getElementById('kind');
  var confidenceEl = document.getElementById('confidence');
  var confirmBtn = document.getElementById('confirm');
  var cancelBtn = document.getElementById('cancel');

  var ctxKnownEl = document.getElementById('ctx-known-body');
  var ctxTasksEl = document.getElementById('ctx-tasks-body');
  var ctxDialogueEl = document.getElementById('ctx-dialogue-body');
  var ctxSessionEl = document.getElementById('ctx-session-body');

  function isInputFocused() {
    var el = document.activeElement;
    return el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.isContentEditable);
  }

  function startRecording() {
    if (recorder && recorder.state === 'recording') return;
    refreshContext();
    navigator.mediaDevices.getUserMedia({ audio: true }).then(function(stream) {
      mediaStream = stream;
      chunks = [];
      recorder = new MediaRecorder(stream);
      recorder.ondataavailable = function(e) { if (e.data.size > 0) chunks.push(e.data); };
      recorder.onstop = function() {
        var blob = new Blob(chunks, { type: recorder.mimeType || 'audio/webm' });
        sendAudio(blob);
        stream.getTracks().forEach(function(t) { t.stop(); });
      };
      recorder.start();
      statusEl.textContent = 'Recording… (release Space to stop)';
    }).catch(function(err) {
      statusEl.textContent = 'Microphone error: ' + err.message;
    });
  }

  function stopRecording() {
    if (recorder && recorder.state === 'recording') {
      recorder.stop();
      statusEl.textContent = 'Processing…';
    }
  }

  function sendAudio(blob) {
    fetch('/api/voice/transcribe', { method: 'POST', body: blob })
      .then(function(r) { return r.json(); })
      .then(function(proposal) {
        rawEl.textContent = proposal.RawTranscript || '';
        rewrittenEl.value = proposal.Rewritten || '';
        kindEl.textContent = proposal.Kind || '';
        confidenceEl.textContent = proposal.Confidence != null ? (proposal.Confidence * 100).toFixed(0) + '%' : '';
        previewedKind = proposal.Kind || 'direct_prompt';
        previewEl.style.display = '';
        statusEl.textContent = 'Review and confirm';
      })
      .catch(function(err) {
        statusEl.textContent = 'Transcribe error: ' + err.message;
      });
  }

  function reset() {
    previewEl.style.display = 'none';
    rewrittenEl.value = '';
    rawEl.textContent = '';
    kindEl.textContent = '';
    confidenceEl.textContent = '';
    statusEl.textContent = 'Hold Space to record';
    previewedKind = 'direct_prompt';
  }

  // /api/context: fetch hint and render the context panel sections
  function refreshContext() {
    fetch('/api/context')
      .then(function(r) { return r.json(); })
      .then(function(resp) { renderContext(resp.hint || ''); })
      .catch(function() {});
  }

  function extractSection(hint, heading) {
    var idx = hint.indexOf(heading);
    if (idx < 0) return '';
    var body = hint.slice(idx + heading.length);
    var next = body.search(/\n## /);
    if (next >= 0) body = body.slice(0, next);
    return body.trim();
  }

  function renderDialogue(section) {
    if (!section) { ctxDialogueEl.innerHTML = ''; return; }
    var lines = section.split('\n').filter(function(l) { return l.trim() !== ''; });
    var html = lines.map(function(line) {
      var role = '', content = line;
      if (line.startsWith('A: ')) { role = 'A'; content = line.slice(3); }
      else if (line.startsWith('U: ')) { role = 'U'; content = line.slice(3); }
      var label = role === 'A' ? '<b>A</b>' : role === 'U' ? '<b>U</b>' : '';
      var threshold = 120;
      if (content.length <= threshold || role !== 'A') {
        return '<div class="dialogue-turn">' + label + ' <span>' + escHtml(content) + '</span></div>';
      }
      // Long assistant turn: collapsible
      var preview = escHtml(content.slice(0, threshold)) + '…';
      var full = escHtml(content);
      return '<details class="dialogue-turn"><summary>' + label + ' ' + preview + '</summary>' + full + '</details>';
    }).join('');
    ctxDialogueEl.innerHTML = html || '';
  }

  function escHtml(s) {
    return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
  }

  function renderContext(hint) {
    ctxKnownEl.textContent = extractSection(hint, '## Known Entities') || '(none)';
    ctxTasksEl.textContent = extractSection(hint, '## Active Tasks') || '(none)';
    renderDialogue(extractSection(hint, '## Recent Dialogue'));
    ctxSessionEl.textContent = extractSection(hint, '## Claude Code Session') || '';
  }

  confirmBtn.addEventListener('click', function() {
    var text = rewrittenEl.value.trim();
    if (!text) return;
    fetch('/api/voice/emit', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ text: text, "kind": previewedKind })
    }).then(function(r) {
      if (r.status === 204) {
        reset();
      } else {
        statusEl.textContent = 'Emit failed: ' + r.status;
      }
    }).catch(function(err) {
      statusEl.textContent = 'Emit error: ' + err.message;
    });
  });

  cancelBtn.addEventListener('click', reset);

  document.addEventListener('keydown', function(e) {
    if (e.code === 'Space' && !isInputFocused() && !e.repeat) {
      e.preventDefault();
      startRecording();
    }
  });
  document.addEventListener('keyup', function(e) {
    if (e.code === 'Space' && !isInputFocused()) {
      e.preventDefault();
      stopRecording();
    }
  });

  // Poll context every 5 seconds; also refresh immediately on load
  refreshContext();
  ctxTimer = setInterval(refreshContext, 5000);
})();
