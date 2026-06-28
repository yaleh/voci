// voci PTT recorder — placeholder (Phase C will fill this in)
// Contracts referenced: /api/voice/transcribe, /api/voice/emit, MediaRecorder
// Fields read from transcribe response: Rewritten, RawTranscript, Kind, Confidence (capitalized — ActionProposal has no json tags)
// Fields sent to emit: { text: ..., "kind": ... }
(function() {
  var chunks = [];
  var recorder = null;
  var previewedKind = 'direct_prompt';
  var mediaStream = null;

  var statusEl = document.getElementById('status');
  var previewEl = document.getElementById('preview');
  var rawEl = document.getElementById('raw');
  var rewrittenEl = document.getElementById('rewritten');
  var kindEl = document.getElementById('kind');
  var confidenceEl = document.getElementById('confidence');
  var confirmBtn = document.getElementById('confirm');
  var cancelBtn = document.getElementById('cancel');

  function isInputFocused() {
    var el = document.activeElement;
    return el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.isContentEditable);
  }

  function startRecording() {
    if (recorder && recorder.state === 'recording') return;
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
})();
