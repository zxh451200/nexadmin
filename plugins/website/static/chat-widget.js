/*
 * 在线客服访客挂件（可复用组件）。
 *
 * 宿主页面只需要挂件 DOM（见 plugins/website/tpl/layout.tmpl 的 [data-chat] 块），
 * 然后调用：NexChat.mount(root, { t: function (key, fallback) { ... } });
 *
 * 能力：开会话（幂等）、文本消息、增量拉取 + SSE、结束态（保留历史 / 开始新会话）、
 * 满意度评价、客服输入中提示、访客已读上报。传输层不依赖任何框架。
 */
(function () {
  'use strict';
  if (window.NexChat) return;

  function mount(root, opts) {
    if (!root || root.getAttribute('data-chat-ready') === '1' || !window.fetch) return;
    root.setAttribute('data-chat-ready', '1');

    var t = opts && typeof opts.t === 'function' ? opts.t : function (key, fallback) { return fallback || key; };
    var base = (opts && opts.base) || '/api/v1/plugins/im/visitor';

    var launcher = root.querySelector('[data-chat-open]');
    var panel = root.querySelector('[data-chat-panel]');
    var log = root.querySelector('[data-chat-log]');
    var empty = root.querySelector('[data-chat-empty]');
    var form = root.querySelector('[data-chat-form]');
    var input = root.querySelector('[data-chat-input]');
    var sendBtn = root.querySelector('[data-chat-send]');
    var typingEl = root.querySelector('[data-chat-typing]');
    var closedBox = root.querySelector('[data-chat-closed]');
    var closedTitle = root.querySelector('[data-chat-closed-title]');
    var ratingBox = root.querySelector('[data-chat-rating]');
    var stars = Array.prototype.slice.call(root.querySelectorAll('[data-rate]'));
    var ratingComment = root.querySelector('[data-chat-rating-comment]');
    var ratingSubmit = root.querySelector('[data-chat-rating-submit]');
    var thanks = root.querySelector('[data-chat-thanks]');
    var newBtn = root.querySelector('[data-chat-new]');
    var attachBtn = root.querySelector('[data-chat-attach]');
    var fileInput = root.querySelector('[data-chat-file]');
    var offlineEl = root.querySelector('[data-chat-offline]');

    var st = {
      convNo: '',
      lastSeq: 0,
      open: false,
      sending: false,
      pulling: false,
      polling: 0,
      es: null,
      seen: {},
      rating: 0,
      score: 0,
      readSeq: 0,
      typingAt: 0,
      typingTimer: 0,
      starting: false
    };

    function store(key, value) {
      try {
        if (value === undefined) return window.localStorage.getItem(key);
        window.localStorage.setItem(key, value);
        return value;
      } catch (e) {
        return null;
      }
    }

    function visitorToken() {
      var token = store('nex_chat_token');
      if (!token) {
        token = window.crypto && window.crypto.randomUUID
          ? window.crypto.randomUUID()
          : 'v-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 12);
        store('nex_chat_token', token);
      }
      return token;
    }

    function guestName() {
      var name = store('nex_chat_name');
      if (!name) {
        name = t('chatGuest', '访客') + ' ' + (1000 + Math.floor(Math.random() * 9000));
        store('nex_chat_name', name);
      }
      return name;
    }

    function genNo() {
      var g = parseInt(store('nex_chat_gen') || '0', 10);
      if (!isFinite(g) || g < 0) g = 0;
      return g === 0 ? 'web' : 'web-' + g;
    }

    function bumpGeneration() {
      var g = parseInt(store('nex_chat_gen') || '0', 10);
      if (!isFinite(g) || g < 0) g = 0;
      store('nex_chat_gen', String(g + 1));
    }

    function parseResponse(res) {
      return res.json().catch(function () { return {}; }).then(function (body) {
        if (!res.ok) {
          var err = new Error((body && body.message) || 'HTTP ' + res.status);
          err.status = res.status;
          throw err;
        }
        return body ? body.data : null;
      });
    }

    function api(path, opt) {
      var init = Object.assign({ credentials: 'same-origin', headers: { Accept: 'application/json' } }, opt || {});
      if (init.body) init.headers = Object.assign({ 'Content-Type': 'application/json' }, init.headers);
      return fetch(base + path, init).then(parseResponse);
    }

    function upload(path, file) {
      var form = new FormData();
      form.append('file', file);
      return fetch(base + path, {
        method: 'POST',
        body: form,
        credentials: 'same-origin',
        headers: { Accept: 'application/json' }
      }).then(parseResponse);
    }

    function ensureConv() {
      if (st.convNo) return Promise.resolve(st.convNo);
      return api('/conversations', {
        method: 'POST',
        body: JSON.stringify({
          visitorToken: visitorToken(),
          visitorName: guestName(),
          bizType: 'website',
          bizNo: genNo(),
          subject: (document.title || '').slice(0, 200)
        })
      }).then(function (conv) {
        st.convNo = conv.convNo;
        st.lastSeq = 0;
        applyConv(conv);
        return st.convNo;
      });
    }

    function fmtTime(iso) {
      try { return new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }); } catch (e) { return ''; }
    }

    function fmtSize(bytes) {
      if (!bytes || bytes < 0) return '';
      var units = ['B', 'KB', 'MB', 'GB'];
      var value = bytes;
      var unit = 0;
      while (value >= 1024 && unit < units.length - 1) { value /= 1024; unit += 1; }
      return (value < 10 && unit > 0 ? value.toFixed(1) : Math.round(value)) + units[unit];
    }

    function attachmentOf(m) {
      if (!m || !m.meta) return null;
      try { return (JSON.parse(m.meta) || {}).attachment || null; } catch (e) { return null; }
    }
    function atBottom() { return log.scrollHeight - log.scrollTop - log.clientHeight < 60; }

    function systemText(m) {
      var code = '';
      if (m.meta) {
        try { code = (JSON.parse(m.meta) || {}).code || ''; } catch (e) { code = ''; }
      }
      if (code === 'conv_closed') return t('chatClosedTitle', '会话已结束');
      return m.content || '';
    }

    function addMessage(m) {
      if (!m || !m.seq || st.seen[m.seq]) return;
      st.seen[m.seq] = 1;
      var stick = atBottom();
      if (empty) empty.hidden = true;
      if (m.kind === 'system') {
        var note = document.createElement('p');
        note.className = 'chat-note';
        note.textContent = systemText(m);
        log.appendChild(note);
      } else {
        var from = m.senderType === 'visitor' ? 'visitor' : 'agent';
        var row = document.createElement('div');
        row.className = 'chat-msg';
        row.setAttribute('data-from', from);
        var bubble = document.createElement('div');
        bubble.className = 'chat-bubble';
        var att = attachmentOf(m);
        if (att) {
          var url = base + '/attachments/' + encodeURIComponent(att.token);
          if (String(att.mime || '').indexOf('image/') === 0) {
            var link = document.createElement('a');
            link.href = url;
            link.target = '_blank';
            link.rel = 'noreferrer';
            var img = document.createElement('img');
            img.className = 'chat-image';
            img.src = url;
            img.alt = att.name || '';
            img.loading = 'lazy';
            link.appendChild(img);
            bubble.appendChild(link);
          } else {
            var file = document.createElement('a');
            file.className = 'chat-file';
            file.href = url;
            file.target = '_blank';
            file.rel = 'noreferrer';
            file.appendChild(document.createTextNode(att.name || 'file'));
            var size = document.createElement('span');
            size.className = 'chat-file-meta';
            size.textContent = fmtSize(att.size);
            file.appendChild(size);
            bubble.appendChild(file);
          }
        }
        if (m.content) bubble.appendChild(document.createTextNode(m.content));
        var meta = document.createElement('span');
        meta.className = 'chat-meta';
        var who = from === 'agent' ? (m.senderName || t('chatAgent', '客服')) + ' · ' : '';
        meta.textContent = who + fmtTime(m.createdAt);
        row.appendChild(bubble);
        row.appendChild(meta);
        log.appendChild(row);
      }
      if (stick) log.scrollTop = log.scrollHeight;
    }

    function addLocalNote(text) {
      if (!text) return;
      if (empty) empty.hidden = true;
      var note = document.createElement('p');
      note.className = 'chat-note';
      note.textContent = text;
      log.appendChild(note);
      log.scrollTop = log.scrollHeight;
    }

    function renderStars(n) {
      stars.forEach(function (btn) {
        var on = Number(btn.getAttribute('data-rate')) <= n;
        btn.classList.toggle('is-on', on);
        btn.setAttribute('aria-pressed', String(Number(btn.getAttribute('data-rate')) <= n && n > 0));
      });
      if (ratingSubmit) ratingSubmit.disabled = n <= 0;
    }

    function applyConv(conv) {
      if (!conv) return;
      st.open = conv.status === 'open';
      st.rating = conv.rating || 0;
      if (closedBox) closedBox.hidden = st.open;
      if (form) form.hidden = !st.open;
      if (closedTitle) {
        closedTitle.textContent = conv.closedBy === 'auto'
          ? t('chatClosedAuto', '会话因长时间无消息结束')
          : t('chatClosedTitle', '会话已结束');
      }
      if (ratingBox) ratingBox.hidden = st.open || st.rating > 0;
      if (thanks) thanks.hidden = st.open || st.rating === 0;
      if (typingEl) typingEl.hidden = true;
      renderStars(st.rating || st.score);
    }

    function markRead() {
      if (!st.convNo || !st.lastSeq || document.hidden || st.readSeq >= st.lastSeq) return;
      if (!panel || panel.hidden) return;
      var seq = st.lastSeq;
      api('/conversations/' + encodeURIComponent(st.convNo) + '/read', {
        method: 'POST',
        body: JSON.stringify({ seq: seq })
      }).then(function () { st.readSeq = seq; }).catch(function () { /* 下一轮再报 */ });
    }

    function showTyping() {
      if (!typingEl) return;
      typingEl.hidden = false;
      window.clearTimeout(st.typingTimer);
      st.typingTimer = window.setTimeout(function () { typingEl.hidden = true; }, 4000);
      if (st.open) log.scrollTop = log.scrollHeight;
    }

    function sendTyping() {
      if (!st.convNo) return;
      var now = Date.now();
      if (now - st.typingAt < 2500) return;
      st.typingAt = now;
      api('/conversations/' + encodeURIComponent(st.convNo) + '/typing', { method: 'POST' }).catch(function () {});
    }

    function pull() {
      if (!st.convNo || st.pulling) return Promise.resolve();
      st.pulling = true;
      return api('/conversations/' + encodeURIComponent(st.convNo) + '/messages?since=' + st.lastSeq + '&limit=50')
        .then(function (data) {
          var rows = (data && data.messages) || [];
          rows.forEach(function (m) {
            if (m.seq > st.lastSeq) st.lastSeq = m.seq;
            addMessage(m);
          });
          if (data && data.conversation) applyConv(data.conversation);
          markRead();
        })
        .catch(function (err) {
          if (err && err.status === 404) { st.convNo = ''; }
        })
        .then(function () { st.pulling = false; }, function () { st.pulling = false; });
    }

    function startPolling() {
      if (st.polling) return;
      st.polling = window.setInterval(function () {
        if (st.open && !document.hidden) pull();
      }, 5000);
    }

    function stopRealtime() {
      if (st.es) { st.es.close(); st.es = null; }
      if (st.polling) { window.clearInterval(st.polling); st.polling = 0; }
      if (st.typingTimer) { window.clearTimeout(st.typingTimer); st.typingTimer = 0; }
      if (typingEl) typingEl.hidden = true;
    }

    function startRealtime() {
      stopRealtime();
      startPolling();
      if (!window.EventSource || !st.convNo) return;
      try {
        st.es = new EventSource(base + '/stream/' + encodeURIComponent(st.convNo));
      } catch (e) { st.es = null; return; }
      st.es.onmessage = function (e) {
        var data = null;
        try { data = JSON.parse(e.data); } catch (err) { data = null; }
        if (data && data.type === 'typing') {
          if (data.senderType === 'agent') showTyping();
          return;
        }
        if (data && data.type) { pull(); return; }
        if (data && data.seq) {
          if (data.seq > st.lastSeq) st.lastSeq = data.seq;
          addMessage(data);
          if (data.senderType === 'agent') markRead();
          return;
        }
        pull();
      };
      st.es.onerror = function () {
        if (st.es) { st.es.close(); st.es = null; }
      };
    }

    function resetConv(keepLog) {
      bumpGeneration();
      st.convNo = '';
      st.lastSeq = 0;
      st.open = true;
      st.seen = {};
      st.readSeq = 0;
      st.rating = 0;
      st.score = 0;
      if (!keepLog && log) {
        log.textContent = '';
        if (empty) { empty.hidden = false; log.appendChild(empty); }
      }
      stopRealtime();
      applyConv({ status: 'open', rating: 0, closedBy: '' });
    }

    function startNew() {
      if (st.starting) return;
      st.starting = true;
      resetConv(true);
      addLocalNote(t('chatNewNote', '已为你开启新会话'));
      ensureConv()
        .then(function () { return pull(); })
        .then(function () {
          startRealtime();
          if (input) input.focus();
        })
        .catch(function () { addLocalNote(t('chatErrLoad', '客服连接不上，请刷新页面再试')); })
        .then(function () { st.starting = false; }, function () { st.starting = false; });
    }

    function submitRating() {
      if (!st.convNo || st.score <= 0) return;
      api('/conversations/' + encodeURIComponent(st.convNo) + '/rating', {
        method: 'POST',
        body: JSON.stringify({ score: st.score, comment: ratingComment ? String(ratingComment.value || '').slice(0, 500) : '' })
      }).then(function (conv) {
        applyConv(conv);
        if (ratingComment) ratingComment.value = '';
      }).catch(function (err) {
        if (err && err.status === 409) {
          st.rating = st.score;
          renderStars(st.rating);
          if (ratingBox) ratingBox.hidden = true;
          if (thanks) thanks.hidden = false;
        } else {
          addLocalNote(t('chatRateFailed', '评价提交失败，请稍后再试'));
        }
      });
    }

    function openPanel() {
      if (!panel) return;
      panel.hidden = false;
      root.classList.add('chat-open');
      if (launcher) launcher.setAttribute('aria-expanded', 'true');
      ensureConv()
        .then(function () { return pull(); })
        .then(function () { startRealtime(); })
        .catch(function () { addLocalNote(t('chatErrLoad', '客服连接不上，请刷新页面再试')); });
      if (input) window.setTimeout(function () { input.focus(); }, 30);
    }

    function closePanel() {
      if (!panel) return;
      panel.hidden = true;
      root.classList.remove('chat-open');
      if (launcher) {
        launcher.setAttribute('aria-expanded', 'false');
        launcher.focus();
      }
      stopRealtime();
    }

    function send() {
      if (!input || st.sending) return;
      var text = String(input.value || '').trim();
      if (!text) return;
      st.sending = true;
      if (sendBtn) sendBtn.disabled = true;
      var payload = { senderId: visitorToken(), senderName: guestName(), content: text };
      var restarted = false;
      var post = function () {
        return ensureConv().then(function () {
          return api('/conversations/' + encodeURIComponent(st.convNo) + '/messages', {
            method: 'POST',
            body: JSON.stringify(payload)
          });
        });
      };
      post()
        .catch(function (err) {
          if (err && err.status === 409) {
            restarted = true;
            addLocalNote(t('chatClosed', '会话已结束，发送新消息会开启新会话'));
            resetConv(true);
            return post();
          }
          throw err;
        })
        .then(function (m) {
          input.value = '';
          input.style.height = '';
          if (m && m.seq) {
            if (m.seq > st.lastSeq) st.lastSeq = m.seq;
            addMessage(m);
          }
          if (restarted) startRealtime();
          return pull();
        })
        .catch(function () { addLocalNote(t('chatErrSend', '发送失败，请稍后重试')); })
        .then(function () {
          st.sending = false;
          if (sendBtn) sendBtn.disabled = false;
        });
    }

    function sendFile(file) {
      if (!file || st.sending) return;
      st.sending = true;
      if (sendBtn) sendBtn.disabled = true;
      if (attachBtn) attachBtn.disabled = true;
      ensureConv()
        .then(function () {
          return upload('/conversations/' + encodeURIComponent(st.convNo) + '/attachments', file);
        })
        .then(function (att) {
          return api('/conversations/' + encodeURIComponent(st.convNo) + '/messages', {
            method: 'POST',
            body: JSON.stringify({
              senderId: visitorToken(),
              senderName: guestName(),
              content: '',
              attachmentToken: att.token
            })
          });
        })
        .then(function (m) {
          if (m && m.seq) {
            if (m.seq > st.lastSeq) st.lastSeq = m.seq;
            addMessage(m);
          }
          return pull();
        })
        .catch(function (err) {
          if (err && err.status === 409) {
            addLocalNote(t('chatClosed', '会话已结束，发送新消息会开启新会话'));
            resetConv(true);
          } else {
            addLocalNote(t('chatUploadFailed', '上传失败，请重试'));
          }
        })
        .then(function () {
          st.sending = false;
          if (sendBtn) sendBtn.disabled = false;
          if (attachBtn) attachBtn.disabled = false;
        });
    }

    if (attachBtn && fileInput) {
      attachBtn.addEventListener('click', function () { fileInput.click(); });
      fileInput.addEventListener('change', function () {
        var file = fileInput.files && fileInput.files[0];
        fileInput.value = '';
        if (file) sendFile(file);
      });
    }

    if (launcher) launcher.addEventListener('click', openPanel);
    var closeBtn = root.querySelector('[data-chat-close]');
    if (closeBtn) closeBtn.addEventListener('click', closePanel);
    if (panel) {
      panel.addEventListener('keydown', function (e) {
        if (e.key === 'Escape') closePanel();
      });
    }
    if (form) {
      form.addEventListener('submit', function (e) {
        e.preventDefault();
        send();
      });
    }
    if (newBtn) newBtn.addEventListener('click', startNew);
    stars.forEach(function (btn) {
      btn.addEventListener('click', function () {
        st.score = Number(btn.getAttribute('data-rate')) || 0;
        renderStars(st.score);
      });
    });
    if (ratingSubmit) ratingSubmit.addEventListener('click', submitRating);
    document.addEventListener('visibilitychange', function () {
      if (!document.hidden && st.convNo) markRead();
    });
    if (input) {
      input.addEventListener('keydown', function (e) {
        if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
          e.preventDefault();
          send();
        }
      });
      input.addEventListener('input', function () {
        input.style.height = 'auto';
        input.style.height = Math.min(input.scrollHeight, 120) + 'px';
        sendTyping();
      });
    }

    // 探测：整组路由 404 表示没装 im 插件，挂件保持隐藏。
    // 命中后顺带取回服务时间与主动邀请配置（离线时挂出提示条，在线时按延迟主动打招呼）。
    fetch(base + '/status', { headers: { Accept: 'application/json' }, credentials: 'same-origin' })
      .then(function (res) {
        if (!res.ok) return null;
        root.hidden = false;
        return res.json().catch(function () { return {}; });
      })
      .then(function (body) {
        var cfg = (body && body.data) || {};
        if (cfg.online === false) {
          if (offlineEl) {
            offlineEl.hidden = false;
            if (cfg.offlineText) offlineEl.textContent = cfg.offlineText;
          }
        } else if (offlineEl) {
          offlineEl.hidden = true;
        }
        var delay = Number(cfg.inviteDelay) || 0;
        if (delay > 0 && cfg.online !== false) {
          var invited = null;
          try { invited = window.sessionStorage.getItem('nex_chat_invited'); } catch (e) { invited = '1'; }
          if (!invited) {
            window.setTimeout(function () {
              if (!panel || !panel.hidden) return;
              try { window.sessionStorage.setItem('nex_chat_invited', '1'); } catch (e) { /* 隐私模式忽略 */ }
              openPanel();
              addLocalNote(cfg.inviteText || t('chatInviteDefault', '你好，需要帮助随时问我'));
            }, Math.min(delay, 600) * 1000);
          }
        }
      })
      .catch(function () { /* 网络不可用时保持隐藏 */ });
  }

  window.NexChat = { mount: mount };
})();
