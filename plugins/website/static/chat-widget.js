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
    // 未读提醒：面板收起时客服发来的消息只能在启动钮上提醒，点开即清零。
    var unreadEl = null;
    var unreadCount = 0;
    var launcherLabel = launcher ? (launcher.getAttribute('aria-label') || '') : '';
    if (launcher) {
      unreadEl = document.createElement('span');
      unreadEl.className = 'chat-launcher-unread';
      unreadEl.hidden = true;
      unreadEl.setAttribute('aria-hidden', 'true');
      launcher.appendChild(unreadEl);
    }
    function paintUnread() {
      if (!launcher) return;
      if (unreadEl) {
        unreadEl.textContent = unreadCount > 99 ? '99+' : String(unreadCount);
        unreadEl.hidden = unreadCount <= 0;
      }
      launcher.setAttribute('aria-label', unreadCount > 0 ? launcherLabel + ' (' + unreadCount + ')' : launcherLabel);
    }
    function clearUnread() { if (unreadCount) { unreadCount = 0; paintUnread(); } }
    var log = root.querySelector('[data-chat-log]');
    var empty = root.querySelector('[data-chat-empty]');
    var form = root.querySelector('[data-chat-form]');
    var input = root.querySelector('[data-chat-input]');
    var sendBtn = root.querySelector('[data-chat-send]');
    var typingEl = root.querySelector('[data-chat-typing]');
    var closedBox = root.querySelector('[data-chat-closed]');
    var closedTitle = root.querySelector('[data-chat-closed-title]');
    var ratingBox = root.querySelector('[data-chat-rating]');
    var ratingTitle = root.querySelector('[data-chat-rating-title]');
    var starsBox = root.querySelector('[data-chat-stars]');
    var stars = Array.prototype.slice.call(root.querySelectorAll('[data-rate]'));
    var ratingComment = root.querySelector('[data-chat-rating-comment]');
    var ratingSubmit = root.querySelector('[data-chat-rating-submit]');
    var thanks = root.querySelector('[data-chat-thanks]');
    var newBtn = root.querySelector('[data-chat-new]');
    var attachBtn = root.querySelector('[data-chat-attach]');
    var fileInput = root.querySelector('[data-chat-file]');
    var offlineEl = root.querySelector('[data-chat-offline]');
    var handoffBtn = root.querySelector('[data-chat-handoff]');
    var headEl = root.querySelector('.chat-head');

    // ---- 新消息提示音 / 桌面通知：面板收起或页面在后台时，让客服回复能被听见 ----
    // 声音用 WebAudio 现场合成（无外部音频文件）；AudioContext 必须在用户手势里创建，
    // 所以在打开面板/点开关时解锁。桌面通知只在站点已获授权时才发，绝不主动弹权限框。
    var soundOn = true;
    try { soundOn = localStorage.getItem('nexchat.sound') !== '0'; } catch (e) { /* 隐私模式忽略 */ }
    var audioCtx = null;
    function unlockAudio() {
      var AC = window.AudioContext || window.webkitAudioContext;
      if (!AC) return;
      if (!audioCtx) {
        try { audioCtx = new AC(); } catch (e) { audioCtx = null; }
      }
      if (audioCtx && audioCtx.state === 'suspended') {
        try { audioCtx.resume().catch(function () {}); } catch (e) { /* 忽略 */ }
      }
    }
    function chime() {
      if (!soundOn || !audioCtx || audioCtx.state !== 'running') return;
      try {
        var t0 = audioCtx.currentTime;
        [880, 660].forEach(function (freq, i) {
          var osc = audioCtx.createOscillator();
          var gain = audioCtx.createGain();
          osc.type = 'sine';
          osc.frequency.value = freq;
          var at = t0 + i * 0.11;
          gain.gain.setValueAtTime(0.0001, at);
          gain.gain.exponentialRampToValueAtTime(0.12, at + 0.015);
          gain.gain.exponentialRampToValueAtTime(0.0001, at + 0.18);
          osc.connect(gain);
          gain.connect(audioCtx.destination);
          osc.start(at);
          osc.stop(at + 0.2);
        });
      } catch (e) { /* 音频不可用时静默 */ }
    }
    function notifyAgent(m) {
      if (!soundOn || !document.hidden) return;
      try {
        if (!window.Notification || Notification.permission !== 'granted') return;
        var text = m.content ? String(m.content).slice(0, 140) : t('chat.attach', '附件');
        var n = new Notification(t('chat.title', '在线客服'), { body: text, tag: 'nex-chat' });
        n.onclick = function () {
          try { window.focus(); } catch (e) { /* 忽略 */ }
          openPanel();
          n.close();
        };
      } catch (e) { /* 忽略 */ }
    }
    var soundBtn = null;
    var SOUND_ICON = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M6 15h-2a1 1 0 0 1 -1 -1v-4a1 1 0 0 1 1 -1h2l3.5 -4.5a.8 .8 0 0 1 1.5 .5v14a.8 .8 0 0 1 -1.5 .5l-3.5 -4.5"/><path d="M15 8a5 5 0 0 1 0 8"/><path d="M17.7 5a9 9 0 0 1 0 14"/></svg>';
    var SOUND_OFF_ICON = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M6 15h-2a1 1 0 0 1 -1 -1v-4a1 1 0 0 1 1 -1h2l3.5 -4.5a.8 .8 0 0 1 1.5 .5v14a.8 .8 0 0 1 -1.5 .5l-3.5 -4.5"/><path d="M15 8a5 5 0 0 1 0 8"/><path d="M16 10l4 4"/><path d="M20 10l-4 4"/></svg>';
    function paintSound() {
      if (!soundBtn) return;
      soundBtn.innerHTML = soundOn ? SOUND_ICON : SOUND_OFF_ICON;
      var label = soundOn ? t('chat.soundOn', '消息提示音：开') : t('chat.soundOff', '消息提示音：关');
      soundBtn.setAttribute('aria-label', label);
      soundBtn.setAttribute('aria-pressed', soundOn ? 'true' : 'false');
      soundBtn.title = label;
      soundBtn.classList.toggle('is-off', !soundOn);
    }
    if (headEl) {
      soundBtn = document.createElement('button');
      soundBtn.type = 'button';
      soundBtn.className = 'chat-tool chat-sound';
      soundBtn.addEventListener('click', function () {
        soundOn = !soundOn;
        try { localStorage.setItem('nexchat.sound', soundOn ? '1' : '0'); } catch (e) { /* 忽略 */ }
        paintSound();
        if (soundOn) { unlockAudio(); chime(); }
      });
      headEl.insertBefore(soundBtn, headEl.querySelector('[data-chat-expand]') || headEl.querySelector('[data-chat-close]'));
      paintSound();
    }

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
      var init = Object.assign({ credentials: 'same-origin', headers: { Accept: 'application/json', 'X-Visitor-Token': visitorToken() } }, opt || {});
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
        headers: { Accept: 'application/json', 'X-Visitor-Token': visitorToken() }
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
      if (code === 'conv_reopened') return t('chatReopened', '会话已重新开启');
      if (code === 'conv_merged_into') return t('chatMergedInto', '会话已合并，发送新消息将开启新会话');
      if (code === 'message_recalled') return t('chatRecalled', '消息已撤回');
      if (code === 'handoff') return t('chatHandoff', '已转人工客服，请稍候');
      return m.content || '';
    }
    function noteCodeOf(m) {
      if (!m.meta) return '';
      try { return (JSON.parse(m.meta) || {}).code || ''; } catch (e) { return ''; }
    }

    // 会话状态是最容易刷屏的一类系统消息：结束 → 重新开启常常成对出现，攒几轮就是
    // 一叠药丸。这里就地改写最后一条状态条：同一状态不重复追加，来回切换只留最新一次。
    var STATE_NOTES = { conv_closed: 1, conv_reopened: 1, conv_merged_into: 1 };
    var lastNoteEl = null;
    function appendNoteEl(code, text) {
      var p = document.createElement('p');
      p.className = 'chat-note';
      if (code) p.setAttribute('data-code', code);
      p.textContent = text;
      log.appendChild(p);
      return p;
    }
    function addSystemNote(code, text) {
      var prev = lastNoteEl && lastNoteEl.isConnected ? lastNoteEl : null;
      // 只有状态条还是日志最后一项时才就地改写：中间夹了真实消息，说明状态变化
      // 发生在那条消息之后，应该另起一条，不能回头改早先那条。
      // 日期分隔符（.chat-day）是本地排版元素、不是真实消息，允许它夹在中间：
      // 否则跨天时"先插日期再渲染状态条"会让相邻判断失败，每天各留一条状态药丸。
      var tail = log.lastElementChild;
      while (tail && tail !== prev && tail.classList && tail.classList.contains('chat-day')) {
        tail = tail.previousElementSibling;
      }
      var adjacent = !!prev && prev === tail;
      if (adjacent && STATE_NOTES[code]) {
        var pc = prev.getAttribute('data-code');
        if (pc === code) { lastNoteEl = prev; return; }
        if (pc && STATE_NOTES[pc]) {
          prev.setAttribute('data-code', code);
          prev.textContent = text;
          // 再往前如果也停在同一状态（多轮来回），旧的那条删掉，只留最后一次；
          // 中间夹的日期分隔符一并带走，避免留下孤立的分隔线。
          var before = prev.previousElementSibling;
          while (before && before.classList &&
                 (before.classList.contains('chat-note') || before.classList.contains('chat-day'))) {
            if (before.classList.contains('chat-note') && before.getAttribute('data-code') !== code) break;
            var dead = before;
            before = before.previousElementSibling;
            dead.remove();
            if (dead === lastNoteEl) lastNoteEl = prev;
          }
          lastNoteEl = prev;
          return;
        }
      }
      lastNoteEl = appendNoteEl(code, text);
    }

    // 会话跨天时插一条日期分隔：只有 HH:mm 的话，隔天再看分不清哪条属于哪天
    var lastDayKey = '';
    function dayKeyOf(iso) {
      var d = new Date(iso);
      return isNaN(d.getTime()) ? '' : d.getFullYear() + '-' + (d.getMonth() + 1) + '-' + d.getDate();
    }
    function dayLabelOf(iso) {
      var d = new Date(iso);
      var now = new Date();
      var k = dayKeyOf(iso);
      if (k === dayKeyOf(now)) return t('chatToday', '今天');
      if (k === dayKeyOf(new Date(now.getTime() - 864e5))) return t('chatYesterday', '昨天');
      try {
        return d.toLocaleDateString(document.documentElement.lang || undefined, { year: 'numeric', month: 'short', day: 'numeric' });
      } catch (e) { return k; }
    }
    function ensureDayDivider(iso) {
      var k = dayKeyOf(iso);
      if (!k || k === lastDayKey) return;
      lastDayKey = k;
      var d = document.createElement('p');
      d.className = 'chat-day';
      d.textContent = dayLabelOf(iso);
      log.appendChild(d);
    }

    function fileLink(att, url) {
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
      return file;
    }

    function addMessage(m) {
      if (!m || !m.seq || st.seen[m.seq]) return;
      st.seen[m.seq] = 1;
      // 面板收起时客服来的消息在启动钮上留未读（访客自己的消息与系统药丸不算）；
      // 面板收起或标签页在后台时还要响一声，后台再补系统通知（已授权时）。
      if (m.senderType !== 'visitor' && m.kind !== 'system' && panel) {
        if (panel.hidden) {
          unreadCount += 1;
          paintUnread();
        }
        if (panel.hidden || document.hidden) {
          chime();
          notifyAgent(m);
        }
      }
      var stick = atBottom();
      if (empty) empty.hidden = true;
      if (m.kind === 'system') {
        // 系统状态提示是居中药丸，不需要日期分隔；跨天时若也插 .chat-day，
        // 会和状态条交替堆成一串「日期/会话已结束/日期/会话已重新开启」，正是刷屏的来源。
        addSystemNote(noteCodeOf(m), systemText(m));
      } else {
        ensureDayDivider(m.createdAt);
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
            // 取图失败时退回文件名链接，气泡里不会剩一块空白
            img.addEventListener('error', function () {
              if (link.parentNode) link.parentNode.replaceChild(fileLink(att, url), link);
            });
            link.appendChild(img);
            bubble.appendChild(link);
          } else {
            bubble.appendChild(fileLink(att, url));
          }
        }
        if (m.content) bubble.appendChild(document.createTextNode(m.content));
        // AI 回答的命中来源（kb 插件写在 meta.sources）：展示「依据了哪些资料」，答案可追溯
        if (m.senderType === 'bot' && m.meta) {
          try {
            var sources = (JSON.parse(m.meta) || {}).sources;
            if (sources && sources.length) {
              var srcEl = document.createElement('span');
              srcEl.className = 'chat-sources';
              srcEl.textContent = t('chatSources', '知识库来源：') + sources.join('、');
              bubble.appendChild(srcEl);
            }
          } catch (e) { /* meta 非 JSON 时忽略 */ }
        }
        var meta = document.createElement('span');
        meta.className = 'chat-meta';
        // AI 回复（kb 插件的机器人）与坐席同侧渲染，meta 里标 AI 区分身份
        var who = from === 'agent'
          ? (m.senderType === 'bot' ? 'AI · ' : (m.senderName || t('chatAgent', '客服')) + ' · ')
          : '';
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
      lastNoteEl = appendNoteEl('', text);
      log.scrollTop = log.scrollHeight;
    }

    // 「连接不上」提示去重：连续失败只保留一条，恢复成功后移除，不再刷屏。
    var loadErrNote = null;
    function showLoadErr() {
      if (loadErrNote && loadErrNote.isConnected) return;
      if (empty) empty.hidden = true;
      loadErrNote = appendNoteEl('', t('chatErrLoad', '客服连接不上，请刷新页面再试'));
      lastNoteEl = loadErrNote;
      log.scrollTop = log.scrollHeight;
    }
    function clearLoadErr() {
      if (loadErrNote && loadErrNote.isConnected) loadErrNote.remove();
      loadErrNote = null;
    }

    function renderStars(n) {
      stars.forEach(function (btn) {
        var on = Number(btn.getAttribute('data-rate')) <= n;
        btn.classList.toggle('is-on', on);
        btn.setAttribute('aria-pressed', String(Number(btn.getAttribute('data-rate')) <= n && n > 0));
      });
      if (ratingSubmit) ratingSubmit.disabled = n <= 0;
    }

    // 已评价的会话保留星级（只读），只收起提问文案与评价表单；
    // 未评价时相反：星级可点，感谢文案不出现。
    function paintRating() {
      var rated = st.rating > 0;
      if (ratingBox) ratingBox.hidden = st.open;
      if (starsBox) starsBox.classList.toggle('is-readonly', rated);
      stars.forEach(function (btn) { btn.disabled = rated; });
      if (ratingTitle) ratingTitle.hidden = rated;
      if (ratingComment) ratingComment.hidden = rated;
      if (ratingSubmit) ratingSubmit.hidden = rated;
      if (thanks) thanks.hidden = !rated;
      renderStars(rated ? st.rating : st.score);
    }

    function applyConv(conv) {
      if (!conv) return;
      st.open = conv.status === 'open';
      st.rating = conv.rating || 0;
      if (closedBox) closedBox.hidden = st.open;
      if (form) form.hidden = !st.open;
      // 转人工按钮只在会话进行中且 AI 还没被接管时显示（botPaused 后服务端幂等，按钮也就没意义了）
      if (handoffBtn) handoffBtn.hidden = !st.open || !!conv.botPaused;
      if (closedTitle) {
        closedTitle.textContent = conv.closedBy === 'auto'
          ? t('chatClosedAuto', '会话因长时间无消息结束')
          : t('chatClosedTitle', '会话已结束');
      }
      paintRating();
      if (typingEl) typingEl.hidden = true;
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
      return api('/conversations/' + encodeURIComponent(st.convNo) + '/messages?since=' + st.lastSeq + '&limit=20')
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
        st.es = new EventSource(base + '/stream/' + encodeURIComponent(st.convNo) + '?visitorToken=' + encodeURIComponent(visitorToken()));
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
        // 收敛状态是闭包变量，不在 st 里；不清会被清空前的旧值污染首次渲染。
        lastNoteEl = null;
        lastDayKey = '';
        loadErrNote = null;
      }
      stopRealtime();
      applyConv({ status: 'open', rating: 0, closedBy: '' });
    }

    function startNew() {
      if (st.starting) return;
      st.starting = true;
      resetConv(false);
      // 新会话从零开始：增强层据此清掉上一轮的卡片草稿（增强层不在时没人监听，无副作用）
      try { root.dispatchEvent(new CustomEvent('chat:session-reset')); } catch (e) { /* 老浏览器忽略 */ }
      addLocalNote(t('chatNewNote', '已为你开启新会话'));
      ensureConv()
        .then(function () { return pull(); })
        .then(function () {
          clearLoadErr();
          startRealtime();
          if (input) input.focus();
        })
        .catch(function () { showLoadErr(); })
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
          paintRating();
        } else {
          addLocalNote(t('chatRateFailed', '评价提交失败，请稍后再试'));
        }
      });
    }

    function openPanel() {
      if (!panel) return;
      panel.hidden = false;
      root.classList.add('chat-open');
      clearUnread();
      unlockAudio();
      if (launcher) launcher.setAttribute('aria-expanded', 'true');
      ensureConv()
        .then(function () { return pull(); })
        .then(function () {
          clearLoadErr();
          startRealtime();
        })
        .catch(function () { showLoadErr(); });
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
            resetConv(false);
            addLocalNote(t('chatClosed', '会话已结束，发送新消息会开启新会话'));
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
            resetConv(false);
            addLocalNote(t('chatClosed', '会话已结束，发送新消息会开启新会话'));
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

    // 转人工：置「人工接管」（AI 停答），系统提示帧与「已接入」由拉取/SSE 带回；服务端幂等。
    if (handoffBtn) {
      handoffBtn.addEventListener('click', function () {
        if (!st.convNo || handoffBtn.disabled) return;
        handoffBtn.disabled = true;
        api('/conversations/' + encodeURIComponent(st.convNo) + '/handoff', { method: 'POST' })
          .then(function () {
            return pull();
          })
          .catch(function () {
            addLocalNote(t('chatHandoffFailed', '转人工失败，请稍后再试'));
          })
          .then(function () { handoffBtn.disabled = false; });
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

    // 清空聊天记录按钮：注入到头部，避免改三套主题的模板。
    // 只清当前视图（DOM + 收敛状态），不动会话号/游标/已读集合，
    // 这样增量拉取不会把清空前的旧消息又拉回来，服务端历史也不受影响。
    var head = root.querySelector('.chat-head');
    if (head && log) {
      var clearLogBtn = document.createElement('button');
      clearLogBtn.type = 'button';
      clearLogBtn.className = 'chat-tool chat-clear-log';
      clearLogBtn.setAttribute('data-chat-clear-log', '');
      clearLogBtn.setAttribute('aria-label', t('chat.clearLog', '清空聊天记录'));
      clearLogBtn.setAttribute('title', t('chat.clearLog', '清空聊天记录'));
      clearLogBtn.innerHTML = '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 6h18"/><path d="M8 6V4h8v2"/><path d="M19 6l-1 14H6L5 6"/><path d="M10 11v5M14 11v5"/></svg>';
      var expandBtn = head.querySelector('[data-chat-expand]');
      if (expandBtn) head.insertBefore(clearLogBtn, expandBtn);
      else head.appendChild(clearLogBtn);
      clearLogBtn.addEventListener('click', function () {
        log.textContent = '';
        if (empty) { empty.hidden = false; log.appendChild(empty); }
        lastNoteEl = null;
        lastDayKey = '';
        loadErrNote = null;
        addLocalNote(t('chat.clearLogDone', '聊天记录已清空'));
      });
    }
    stars.forEach(function (btn) {
      btn.addEventListener('click', function () {
        if (st.rating > 0) return;
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
        growInput();
        sendTyping();
      });
    }

    function growInput() {
      if (!input) return;
      input.style.height = 'auto';
      input.style.height = Math.min(input.scrollHeight, 128) + 'px';
    }

    // 空态常见问题：点一下填进输入框，让访客还能改词再发
    Array.prototype.forEach.call(root.querySelectorAll('[data-chat-suggest]'), function (btn) {
      btn.addEventListener('click', function () {
        if (!input) return;
        input.value = btn.textContent || '';
        growInput();
        input.focus();
      });
    });

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
        root.setAttribute('data-online', cfg.online === false ? '0' : '1');
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
