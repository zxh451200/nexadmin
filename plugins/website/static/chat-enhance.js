// 官网客服挂件增强层：不改动 chat-widget.js 的既有会话逻辑，只通过 DOM 事件与
// 观察器补上商品引用、翻译、名牌/地址卡片、客服身份/本地时间、表情与更多菜单。样式随脚本注入，
// 四套主题共用一份；无脚本时原有挂件照常工作。
(function () {
  'use strict';

  var CSS = [
    '.chat-panel{position:relative}',
    '.chat-product{display:flex;align-items:center;gap:8px;margin:0 10px 8px;padding:8px;border:1px solid rgba(128,128,128,.28);border-radius:4px;background:Canvas;color:CanvasText;font-size:11px}',
    '.chat-product[hidden]{display:none}',
    '.chat-product-cover{flex:none;width:38px;height:38px;overflow:hidden;border-radius:3px;background:rgba(128,128,128,.12)}',
    '.chat-product-cover img{width:100%;height:100%;object-fit:cover}',
    '.chat-product-info{display:flex;flex:1;flex-direction:column;gap:2px;min-width:0}',
    '.chat-product-info strong{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:12px}',
    '.chat-product-info span{opacity:.75}',
    '.chat-product-remove{flex:none;border:0;background:transparent;color:inherit;font-size:18px;line-height:1;cursor:pointer;opacity:.6}',
    '.chat-product-remove:hover{opacity:1}',
    // 客服名 + 本地时间跟标题同一行（对齐阿里商详的头部）：中间用 · 分隔，过长省略
    '.chat-agent{font-size:11px;opacity:.72;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}',
    '.chat-agent>span+span::before{content:"·";margin-inline:6px;opacity:.6}',
    '.chat-agent>span:empty{display:none}',
    // 一行放不下时只让「姓名 · 时间」收窄省略，标题和按钮不参与压缩，避免整行被挤到换行
    '.chat-identity-text{flex-wrap:nowrap;overflow:hidden}',
    '.chat-identity-text>strong{flex:none;white-space:nowrap}',
    '.chat-agent{min-width:0}',
    '.chat-agent[hidden]{display:none}',
    '.chat-assurance{margin:0;padding:6px 12px;border-block-end:1px solid rgba(128,128,128,.18);background:rgba(128,128,128,.06);font-size:10.5px;line-height:1.5;opacity:.8}',
    // 工具栏/头部的小按钮统一成「无边框 + 圆角 + hover 底色」：主题里 .chat-attach 是 32px 白底方框，
    // 和旁边 28px 无框的表情/翻译并排就成了三种规格，一排图标看着高低不齐。
    '.chat-tool,.chat-attach{display:inline-flex;align-items:center;justify-content:center;width:30px;height:30px;flex:none;padding:0;border:0;border-radius:8px;background:transparent;color:inherit;cursor:pointer;opacity:.75}',
    '.chat-tool svg,.chat-attach svg{width:17px;height:17px;flex:none}',
    '.chat-tool:hover,.chat-attach:hover{opacity:1;background:rgba(128,128,128,.16);color:inherit}',
    // 头部是深色底，灰底会糊成一块浅斑，改用白色高光
    '.chat-head .chat-tool:hover{background:rgba(255,255,255,.14)}',
    '.chat-tool[disabled],.chat-attach[disabled]{opacity:.4;cursor:default}',
    '.chat-send svg{width:17px;height:17px}',
    '.chat-emoji-pop,.chat-more-pop{position:absolute;z-index:20;display:flex;flex-wrap:wrap;gap:4px;padding:8px;border:1px solid rgba(128,128,128,.3);border-radius:4px;background:Canvas;color:CanvasText;box-shadow:0 12px 28px rgba(0,0,0,.18)}',
    '.chat-emoji-pop[hidden],.chat-more-pop[hidden]{display:none}',
    '.chat-emoji-pop{inset-inline-start:8px;inset-block-end:96px;display:grid;grid-template-columns:repeat(9,minmax(0,1fr));gap:2px;box-sizing:border-box;width:min(304px,calc(100% - 16px));max-height:216px;overflow-y:auto;padding:8px}',
    // content-visibility：176 个 emoji 首开时光栅化全部字形，弱机/高负载下点开有可感知的停顿；
    // 视口内只有 ~45 个，屏外的跳过绘制（滚动到再画），首开绘制量直接砍到 1/4
    '.chat-emoji-pop button{display:flex;align-items:center;justify-content:center;width:100%;aspect-ratio:1/1;padding:0;border:0;border-radius:4px;background:transparent;color:inherit;font-size:18px;line-height:1;cursor:pointer;content-visibility:auto;contain-intrinsic-size:auto 32px}',
    '.chat-emoji-pop button:hover{background:rgba(128,128,128,.18)}',
    '.chat-emoji-pop::-webkit-scrollbar{width:6px}',
    '.chat-emoji-pop::-webkit-scrollbar-thumb{border-radius:3px;background:rgba(128,128,128,.4)}',
    '.chat-more-pop{inset-inline-end:10px;inset-block-start:calc(var(--chat-head-h,86px) + 4px);flex-direction:column;min-width:120px}',
    '.chat-more-pop button{padding:6px 8px;border:0;border-radius:3px;background:transparent;color:inherit;text-align:start;font:inherit;font-size:12px;cursor:pointer}',
    '.chat-more-pop button:hover{background:rgba(128,128,128,.14)}',
    '.chat-msg-translate{margin:2px 0 0;padding:0;border:0;background:transparent;color:inherit;font-size:10.5px;text-decoration:underline;cursor:pointer;opacity:.65}',
    '.chat-msg-translate:hover{opacity:1}',
    '.chat-compose textarea{resize:vertical;max-height:220px}',
    '.chat-card-pop{position:absolute;z-index:22;inset-inline-start:8px;inset-block-end:96px;box-sizing:border-box;width:min(300px,calc(100% - 16px));padding:14px 14px 12px;border:1px solid rgba(128,128,128,.28);border-radius:8px;background:Canvas;color:CanvasText;box-shadow:0 16px 38px rgba(0,0,0,.22);font-size:12px}',
    '.chat-card-pop[hidden]{display:none}',
    '.chat-card-pop h3{margin:0 0 3px;font-size:13.5px;font-weight:600}',
    '.chat-card-hint{margin:0 0 13px;font-size:11px;line-height:1.5;opacity:.66}',
    '.chat-card-fields{display:grid;grid-template-columns:1fr 1fr;gap:10px 8px}',
    // 不透明度只能落在字段文字上：label 整体 opacity 会把里面的下拉展开层
    // 一起压成 82% 半透明，底下的「详细地址」就透出来了（opacity 造出的是整组合成）
    '.chat-card-fields label{display:flex;flex-direction:column;gap:4px;min-width:0;font-size:11.5px}',
    '.chat-card-fields label>span{opacity:.82}',
    '.chat-card-fields label.wide{grid-column:1/-1}',
    '.chat-card-fields input{box-sizing:border-box;width:100%;height:30px;padding:0 9px;border:1px solid rgba(128,128,128,.34);border-radius:4px;background:transparent;color:inherit;font:inherit;font-size:12.5px;transition:border-color .15s ease,box-shadow .15s ease}',
    // 聚焦别用深色描边：currentColor 在浅色主题里就是近黑，1px 边框叠 3px 光晕后
    // 比卡片本身还抢眼。改成「边线加深一档 + 一层极淡的光晕」，安静但看得见。
    '.chat-card-fields input:focus{outline:none;border-color:rgba(128,128,128,.72);box-shadow:0 0 0 3px rgba(128,128,128,.13);background:rgba(128,128,128,.03)}',
    '.chat-card-actions{display:flex;gap:8px;margin-top:14px}',
    '.chat-card-actions button{flex:1;height:32px;border:1px solid rgba(128,128,128,.36);border-radius:4px;background:transparent;color:inherit;font:inherit;font-size:12.5px;cursor:pointer}',
    '.chat-card-actions button:hover{border-color:currentColor}',
    // 主按钮做反色实心：原来主次都只有一圈边框，只靠字重区分，一眼看不出该点哪个。
    // 注意不能写 background:currentColor 再改 color —— 同一规则里 color 先落值，
    // currentColor 会跟着变成 Canvas，按钮就白底白字消失了；用 CanvasText 显式取反色。
    '.chat-card-actions .is-primary{border-color:transparent;background:var(--pc-accent,CanvasText);color:Canvas;font-weight:600}',
    '.chat-card-actions .is-primary:hover{filter:brightness(1.08)}',
    // 地址卡片的国家/州省下拉：触发钮长得和输入框一致，展开层浮在表单上面；
    // 跟 .chat-card-pop 一样用 Canvas/currentColor，浅深主题都不用另配色。
    '.chat-card-select{position:relative}',
    '.chat-card-trigger{display:flex;align-items:center;gap:5px;box-sizing:border-box;width:100%;height:30px;padding:0 9px;border:1px solid rgba(128,128,128,.34);border-radius:4px;background:transparent;color:inherit;font:inherit;font-size:12.5px;cursor:pointer;text-align:start;transition:border-color .15s ease,box-shadow .15s ease}',
    '.chat-card-trigger:hover,.chat-card-trigger:focus-visible{border-color:rgba(128,128,128,.72)}',
    '.chat-card-trig-flag{flex:none;font-size:13px;line-height:1}',
    '.chat-card-trig-flag:empty{display:none}',
    '.chat-card-trig-txt{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}',
    '.chat-card-trig-txt.is-ph{opacity:.5}',
    '.chat-card-trig-chev{flex:none;display:inline-flex;color:inherit;opacity:.55}',
    '.chat-card-trig-chev svg{width:14px;height:14px}',
    '.chat-card-drop{position:absolute;z-index:26;top:100%;inset-inline:0;margin-top:4px;border:1px solid rgba(128,128,128,.34);border-radius:6px;background:Canvas;color:CanvasText;box-shadow:0 10px 26px rgba(0,0,0,.2);overflow:hidden}',
    '.chat-card-drop[hidden]{display:none}',
    '.chat-card-search{box-sizing:border-box;width:100%;height:28px;padding:0 9px;border:0;border-block-end:1px solid rgba(128,128,128,.24);background:transparent;color:inherit;font:inherit;font-size:12px}',
    '.chat-card-search:focus{outline:none;background:rgba(128,128,128,.05)}',
    '.chat-card-options{max-height:188px;overflow-y:auto}',
    '.chat-card-options::-webkit-scrollbar{width:6px}',
    '.chat-card-options::-webkit-scrollbar-thumb{border-radius:3px;background:rgba(128,128,128,.4)}',
    '.chat-card-options button{display:flex;align-items:center;gap:6px;width:100%;padding:0 9px;border:0;background:transparent;color:inherit;font:inherit;font-size:12px;line-height:28px;cursor:pointer;text-align:start}',
    '.chat-card-options button:hover{background:rgba(128,128,128,.14)}',
    '.chat-card-options button.is-sel{font-weight:600}',
    '.chat-opt-flag{flex:none;line-height:1}',
    '.chat-opt-name{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}',
    '.chat-opt-dial{flex:none;opacity:.55;font-size:11px}',
    '.chat-opt-empty{display:block;padding:8px 9px;font-size:11.5px;opacity:.55}',
    // 电话区号前缀：随国家联动，纯展示；提交值里才真正拼上区号
    '.chat-phone-wrap{display:flex;align-items:center;box-sizing:border-box;width:100%;height:30px;padding:0 9px;border:1px solid rgba(128,128,128,.34);border-radius:4px;background:transparent;transition:border-color .15s ease,box-shadow .15s ease}',
    '.chat-phone-wrap:focus-within{border-color:rgba(128,128,128,.72);box-shadow:0 0 0 3px rgba(128,128,128,.13);background:rgba(128,128,128,.03)}',
    '.chat-card-fields .chat-phone-wrap input{height:100%;flex:1;min-width:0;border:0;padding:0;background:transparent;font-size:12.5px}',
    '.chat-card-fields .chat-phone-wrap input:focus{outline:none;border:0;box-shadow:none;background:transparent}',
    '.chat-phone-dial{flex:none;padding-inline-end:7px;margin-inline-end:7px;border-inline-end:1px solid rgba(128,128,128,.3);font-size:12px;opacity:.72}',
    '.chat-phone-dial[hidden]{display:none}',
    '.chat-bubble-card{white-space:pre-wrap;border-inline-start:2px solid currentColor;font-size:11.5px;line-height:1.6}',
    '.chat.is-expanded .chat-panel{width:min(760px,calc(100vw - 40px));max-width:calc(100vw - 40px);height:min(86dvh,840px);max-height:86dvh}',
    '.chat-panel{transition:width .2s ease,height .2s ease}',
    '@media(max-width:760px){.chat.is-expanded .chat-panel{width:calc(100vw - 16px);max-width:calc(100vw - 16px);height:min(88dvh,760px)}}',
    '@media(prefers-reduced-motion:reduce){.chat-panel{transition:none}}'
  ].join('');

  // 表情按「笑脸 → 手势 → 商务物件」排布，对齐参考面板的视觉顺序；
  // 用系统 emoji 而非平台自有图片：自托管、无外部请求、跨主题不变色。
  var EMOJI_FACES = ['😀','😃','😄','😁','😆','😅','🤣','😂','🙂','🙃','😉','😊','😇','🥰','😍','🤩','😘','😗','😚','😙','😋','😛','😜','🤪','😝','🤑','🤗','🤭','🤫','🤔','🤨','😐','😑','😶','😏','😒','🙄','😬','😌','😔','😪','🤤','😴','😷','🤒','🤕','🥵','🥶','😵','🤯','🤠','🥳','😎','🤓','🧐','😕','🙁','😮','😯','😲','😳','🥺','😦','😧','😨','😰','😥','😢','😭','😱','😖','😣','😞','😓','😩','😫','🥱','😤','😡','😠','🤬','😈','👻','🤖','😺','😸','😹','😻','😼','😽','🙀','😿','😾'];
  var EMOJI_HANDS = ['👋','🤚','🖐','✋','🖖','👌','🤌','🤏','✌️','🤞','🤟','🤘','🤙','👈','👉','👆','👇','☝️','👍','👎','✊','👊','🤛','🤜','👏','🙌','👐','🤲','🤝','🙏','💪','🦾','✍️','💅'];
  var EMOJI_OBJECTS = ['❤️','🧡','💛','💚','💙','💜','💔','💕','💖','💗','🌹','🌷','💐','🎁','🎉','🎊','✉️','📧','📦','📮','⏰','⏳','✈️','🚢','🚚','📍','🔥','💰','💵','💳','📈','📉','✅','❌','⭐','🌟','💡','🔔','📞','📱','🖥️','🏭','🏢','📋','📎','🔒','🔑','🛒','🤝'];
  var EMOJIS = EMOJI_FACES.concat(EMOJI_HANDS, EMOJI_OBJECTS);

  // 名牌 / 地址卡片图标与站上其他图标同源 Tabler Icons v3.36.0 outline（MIT）：
  // id / map-pin。公开站是服务端模板 + 原生 JS，这里只固化这两个 path 与统一的 stroke 口径，
  // 不再混入 @ant-design/icons 的实心图标，避免同一页面出现两套画风。
  var CARD_ICON_ATTRS = ' viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"';
  var CARD_ICONS = {
    name: '<svg' + CARD_ICON_ATTRS + '><path d="M3 4m0 3a3 3 0 0 1 3 -3h12a3 3 0 0 1 3 3v10a3 3 0 0 1 -3 3h-12a3 3 0 0 1 -3 -3z" /><path d="M9 10m-2 0a2 2 0 1 0 4 0a2 2 0 1 0 -4 0" /><path d="M15 8l2 0" /><path d="M15 12l2 0" /><path d="M7 16l10 0" /></svg>',
    address: '<svg' + CARD_ICON_ATTRS + '><path d="M9 11a3 3 0 1 0 6 0a3 3 0 0 0 -6 0" /><path d="M17.657 16.657l-4.243 4.243a2 2 0 0 1 -2.827 0l-4.244 -4.243a8 8 0 1 1 11.314 0z" /></svg>',
    // 下拉触发钮右端的 chevron-down，与卡片图标同一套 Tabler 口径
    chevron: '<svg' + CARD_ICON_ATTRS + '><path d="M6 9l6 6 6-6" /></svg>'
  };

  // 地址卡片的国家/州省数据集（plugins/website/static/country-region.json）：
  // 由 country-region-data（MIT）+ country-codes-list（MIT）+ i18n-iso-countries 中文国名（MIT）
  // 合并生成，来源与许可见 docs/功能与权限清单.md。首次打开地址卡才拉取，失败时退回手写输入。
  var COUNTRY_DATA = null;
  var COUNTRY_REQ = null;
  function loadCountries() {
    if (COUNTRY_DATA) return Promise.resolve(COUNTRY_DATA);
    if (!COUNTRY_REQ) {
      COUNTRY_REQ = fetch('/website-assets/country-region.json', { credentials: 'same-origin' })
        .then(function (res) { if (!res.ok) throw new Error('unavailable'); return res.json(); })
        .then(function (list) { COUNTRY_DATA = Array.isArray(list) ? list : []; return COUNTRY_DATA; })
        .catch(function (err) { COUNTRY_REQ = null; throw err; });
    }
    return COUNTRY_REQ;
  }
  // 国旗不必存数据：ISO2 码的两个字母各加 regional-indicator 偏移就是 emoji
  function flagEmoji(code) {
    var out = '';
    for (var i = 0; i < code.length; i++) out += String.fromCodePoint(0x1F1E6 + code.charCodeAt(i) - 65);
    return out;
  }
  // 站点语言是英文就显示英文国名（州省名本来就是英文，与阿里中文界面下的表现一致）
  function siteLangEn() {
    return /^en/i.test(String(document.documentElement.lang || ''));
  }

  function readI18n() {
    try {
      var el = document.getElementById('site-i18n');
      if (el) return JSON.parse(el.textContent || '{}');
    } catch (e) { /* ignore */ }
    return {};
  }

  function init() {
    var root = document.querySelector('[data-chat]');
    if (!root) return;
    if (!document.getElementById('site-chat-enhance-style')) {
      var st = document.createElement('style');
      st.id = 'site-chat-enhance-style';
      st.textContent = CSS;
      document.head.appendChild(st);
    }
    var i18n = readI18n();
    function t(k, fb) { return i18n[k] || fb; }

    var input = root.querySelector('[data-chat-input]');
    var form = root.querySelector('[data-chat-form]');
    var log = root.querySelector('[data-chat-log]');
    var panel = root.querySelector('[data-chat-panel]');
    var productBox = root.querySelector('[data-chat-product]');
    var productCover = root.querySelector('[data-chat-product-cover]');
    var productTitle = root.querySelector('[data-chat-product-title]');
    var productPrice = root.querySelector('[data-chat-product-price]');
    var productMoq = root.querySelector('[data-chat-product-moq]');
    var productRemove = root.querySelector('[data-chat-product-remove]');
    var translateBtn = root.querySelector('[data-chat-translate]');
    var emojiBtn = root.querySelector('[data-chat-emoji]');
    var emojiPop = root.querySelector('[data-chat-emoji-pop]');
    var moreBtn = root.querySelector('[data-chat-more]');
    var morePop = root.querySelector('[data-chat-more-pop]');
    var expandBtn = root.querySelector('[data-chat-expand]');
    var clearBtn = root.querySelector('[data-chat-clear]');
    var agentBox = root.querySelector('[data-chat-agent]');
    var agentName = root.querySelector('[data-chat-agent-name]');
    var agentTime = root.querySelector('[data-chat-agent-time]');
    var chatTitle = root.querySelector('#chat-title');

    var state = { product: null, sent: false, timezone: '', expanded: false };

    // ---- 翻译：方向由后端按站点默认语言与当前页面语言决定 ----
    function translate(text, dir) {
      return fetch('/website/visitor/translate', {
        method: 'POST', credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
        body: JSON.stringify({ text: text, dir: dir, lang: document.documentElement.lang || '' })
      }).then(function (res) {
        if (res.status === 429) throw new Error('rate');
        if (!res.ok) throw new Error('unavailable');
        return res.json().catch(function () { return {}; });
      }).then(function (body) {
        var data = body && body.data;
        if (!data || typeof data.text !== 'string') throw new Error('bad');
        return data;
      });
    }
    function note(text) {
      if (!log) return;
      var p = document.createElement('p');
      p.className = 'chat-note';
      p.textContent = text;
      log.appendChild(p);
      log.scrollTop = log.scrollHeight;
    }
    if (translateBtn) {
      translateBtn.addEventListener('click', function () {
        var text = String((input && input.value) || '').trim();
        if (!text || translateBtn.disabled) return;
        translateBtn.disabled = true;
        translate(text, 'out').then(function (data) {
          if (input && !data.skipped) input.value = data.text;
          if (data.skipped) note(t('chatTranslateSame', '当前语言一致，无需翻译'));
          else note(t('chatTranslated', '已翻译为对方语言'));
        }).catch(function () {
          note(t('chatTranslateFailed', '翻译失败，请稍后重试'));
        }).then(function () { translateBtn.disabled = false; });
      });
    }

    // ---- 收到的客服消息：气泡下挂「翻译」按钮，可来回切换原文/译文 ----
    function decorateMessages() {
      if (!log) return;
      // 商品 / 名牌 / 地址这类结构化消息是多行文本，给气泡加一点卡片感
      Array.prototype.forEach.call(log.querySelectorAll('.chat-bubble'), function (b) {
        var txt = String(b.textContent || '').trim();
        if (/^\[[^\]]+\]/.test(txt) && txt.indexOf('\n') > 0) b.classList.add('chat-bubble-card');
      });
      var rows = log.querySelectorAll('.chat-msg[data-from="agent"]');
      Array.prototype.forEach.call(rows, function (row) {
        if (row.querySelector('[data-chat-translate-msg]')) return;
        var bubble = row.querySelector('.chat-bubble');
        if (!bubble) return;
        var source = bubble.textContent || '';
        if (!source.trim()) return;
        var btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'chat-msg-translate';
        btn.setAttribute('data-chat-translate-msg', '');
        btn.textContent = t('chatTranslateIn', '翻译此消息');
        btn.addEventListener('click', function () {
          if (btn.disabled) return;
          if (btn.getAttribute('data-done') === '1') {
            bubble.textContent = source;
            btn.textContent = t('chatTranslateIn', '翻译此消息');
            btn.removeAttribute('data-done');
            return;
          }
          btn.disabled = true;
          translate(source, 'in').then(function (data) {
            if (!data.skipped) bubble.textContent = data.text;
            btn.textContent = t('chatTranslateOriginal', '查看原文');
            btn.setAttribute('data-done', '1');
            if (data.skipped) note(t('chatTranslateSame', '当前语言一致，无需翻译'));
          }).catch(function () {
            note(t('chatTranslateFailed', '翻译失败，请稍后重试'));
          }).then(function () { btn.disabled = false; });
        });
        row.appendChild(btn);

        // 头部客服身份：从消息 meta 里取坐席名，AI 回答不冒充真人
        var meta = row.querySelector('.chat-meta');
        if (meta && agentName && !state.agentName) {
          var who = String(meta.textContent || '').split('·')[0].trim();
          if (who && who !== 'AI' && who !== t('chatAgent', '客服')) {
            state.agentName = who;
            // 名字顶到标题上（对齐阿里商详：头部就是「坐席姓名 · 本地时间」一行），
            // 面板只有 380 宽，再挂「在线客服 + 姓名 + 时间」会被截断成省略号。
            if (chatTitle) chatTitle.textContent = who;
            agentName.textContent = '';
            if (agentBox) agentBox.hidden = false;
            updateTime(); // 姓名先到时补上本地时间，姓名后到时它自己会亮出来
          }
        }
      });
    }
    if (log && 'MutationObserver' in window) {
      var mo = new MutationObserver(function () { decorateMessages(); });
      mo.observe(log, { childList: true, subtree: false });
    }
    decorateMessages();

    // ---- 对方本地时间：IM 的服务时区由公开 status 接口下发 ----
    // 只在已经知道坐席姓名时露出来：「本地时间 23:19」单独一行看不出跟谁对话，宁可不显示。
    function updateTime() {
      if (!state.timezone || !agentTime) return;
      try {
        var s = new Intl.DateTimeFormat([], { timeZone: state.timezone, hour: '2-digit', minute: '2-digit' }).format(new Date());
        agentTime.textContent = t('chatLocalTime', '本地时间') + ' ' + s;
        if (agentBox && state.agentName) agentBox.hidden = false;
      } catch (e) { /* 时区名非法时忽略 */ }
    }
    fetch('/api/v1/plugins/im/visitor/status', { credentials: 'same-origin', headers: { Accept: 'application/json' } })
      .then(function (r) { return r.ok ? r.json().catch(function () { return {}; }) : {}; })
      .then(function (body) {
        var cfg = (body && body.data) || {};
        state.timezone = cfg.timezone || '';
        updateTime();
        if (state.timezone) window.setInterval(updateTime, 60000);
      }).catch(function () { /* 未装 IM 时挂件本就隐藏 */ });

    // 详情页的「数量」步进器：把访客选好的数量一并带进询盘，
    // 客服第一句就能看到要多少，不用来回问。列表页没有步进器，返回空值即可。
    function pageQty() {
      var inp = document.querySelector('[data-pd-qty-input]');
      if (!inp) return { qty: '', unit: '' };
      var box = document.querySelector('[data-pd-qty]');
      var v = parseInt(String(inp.value).replace(/[^\d]/g, ''), 10);
      return { qty: isNaN(v) ? '' : String(v), unit: box ? (box.getAttribute('data-pd-qty-unit') || '') : '' };
    }

    // ---- 商品引用卡：点详情页/列表页的「立即询盘」把商品带进会话 ----
    function renderProduct() {
      if (!productBox) return;
      if (!state.product) { productBox.hidden = true; return; }
      productBox.hidden = false;
      if (productTitle) productTitle.textContent = state.product.title || '';
      if (productPrice) productPrice.textContent = state.product.price || '';
      if (productMoq) {
        var bits = [];
        if (state.product.moq) bits.push(state.product.moq);
        if (state.product.qty) bits.push(t('chatQuantity', '数量') + ': ' + state.product.qty + (state.product.qtyUnit ? ' ' + state.product.qtyUnit : ''));
        productMoq.textContent = bits.join(' · ');
      }
      if (productCover) {
        productCover.textContent = '';
        if (state.product.cover) {
          var img = document.createElement('img');
          img.src = state.product.cover;
          img.alt = '';
          productCover.appendChild(img);
        }
      }
    }
    function clearProduct() {
      state.product = null;
      state.sent = false;
      renderProduct();
    }
    if (productRemove) productRemove.addEventListener('click', clearProduct);
    // 挂上商品后再调数量：卡片与待发消息跟着变，不会发出旧数字
    document.addEventListener('input', function (e) {
      if (!state.product || !e.target || !e.target.matches || !e.target.matches('[data-pd-qty-input]')) return;
      var q = pageQty();
      state.product.qty = q.qty;
      state.product.qtyUnit = q.unit;
      state.sent = false;
      renderProduct();
    });
    document.addEventListener('click', function (e) {
      var cta = e.target.closest ? e.target.closest('[data-chat-open-product]') : null;
      if (!cta) return;
      // 面板没渲染出来时别吞点击：<a> 还得靠 href 走邮件 / 联系页
      if (!panel) return;
      e.preventDefault();
      var q = pageQty();
      state.product = {
        title: cta.getAttribute('data-chat-title') || '',
        price: cta.getAttribute('data-chat-price') || '',
        moq: cta.getAttribute('data-chat-moq') || '',
        qty: q.qty,
        qtyUnit: q.unit,
        url: cta.getAttribute('data-chat-url') || '',
        cover: cta.getAttribute('data-chat-cover') || ''
      };
      state.sent = false;
      renderProduct();
      if (panel.hidden) {
        var launcher = root.querySelector('[data-chat-open]');
        if (launcher) launcher.click();
      }
      // 「联系商家」是直接对话：先垫一句可发送的开场白，
      // 点完输入框里立刻有字，不会像点了没反应；用户自己写过就不覆盖。
      if (cta.getAttribute('data-chat-intent') === 'contact' && input && !String(input.value || '').trim()) {
        input.value = t('chatGreeting', '您好，我想进一步了解这款产品的详细信息。');
        input.dispatchEvent(new Event('input', { bubbles: true }));
      }
      if (input) {
        input.focus();
        // 面板展开有过渡，等一帧再聚焦，键盘和光标才落得进去
        window.setTimeout(function () { input.focus(); }, 80);
      }
    });
    // capture 阶段改写输入值，让原挂件的 send() 直接把商品上下文一起发出去
    if (form) {
      form.addEventListener('submit', function () {
        if (!input || !state.product || state.sent) return;
        var lines = ['[' + t('chatProductLabel', '咨询商品') + '] ' + state.product.title];
        if (state.product.price) lines.push(state.product.price);
        if (state.product.moq) lines.push(state.product.moq);
        if (state.product.qty) lines.push(t('chatQuantity', '数量') + ': ' + state.product.qty + (state.product.qtyUnit ? ' ' + state.product.qtyUnit : ''));
        if (state.product.url) lines.push(location.origin + state.product.url);
        input.value = lines.join('\n') + '\n\n' + String(input.value || '').trim();
        state.sent = true;
      }, true);
    }

    // ---- 名牌 / 地址卡片：访客填一次，之后一键复用；发送走原挂件的 send() ----
    var CARD_DEFS = {
      name: {
        title: t('chatCardNameTitle', '我的名片'),
        marker: '[' + t('chatCardNameLabel', '名片') + ']',
        // 两列网格里按「必填 → 公司 → 选填」排：原来姓名、公司、邮箱各自独占整行，
        // 中间夹着职位/电话半行，满行与半行交替，卡片看上去参差不齐。
        // 现在 1+1 / 占满 / 1+1，每行都收在同一列宽上。
        fields: [
          { key: 'name', label: t('chatCardName', '姓名'), required: true },
          { key: 'phone', label: t('chatCardPhone', '电话'), required: true },
          { key: 'company', label: t('chatCardCompany', '公司'), wide: true },
          { key: 'job', label: t('chatCardJob', '职位') },
          { key: 'email', label: t('chatCardEmail', '邮箱') }
        ]
      },
      address: {
        title: t('chatCardAddressTitle', '收货地址'),
        marker: '[' + t('chatCardAddressLabel', '收货地址') + ']',
        // 对齐 Alibaba 添加地址：国家/地区 → 州/省 → 城市三段（州与城市都是必填），
        // 城市 在州/省右边并排，详细地址独占一行，邮编收尾。
        fields: [
          { key: 'recipient', label: t('chatCardRecipient', '收件人'), required: true },
          { key: 'phone', label: t('chatCardPhone', '电话'), required: true },
          { key: 'country', label: t('chatCardCountry', '国家/地区'), required: true },
          { key: 'region', label: t('chatCardRegion', '州/省'), required: true },
          { key: 'city', label: t('chatCardCity', '城市'), required: true },
          { key: 'street', label: t('chatCardStreet', '详细地址'), required: true, wide: true },
          { key: 'postcode', label: t('chatCardPostcode', '邮编') }
        ]
      }
    };
    var cardPop = document.createElement('div');
    cardPop.className = 'chat-card-pop';
    cardPop.setAttribute('data-chat-card-pop', '');
    cardPop.hidden = true;
    if (panel) panel.appendChild(cardPop);
    var activeKind = null;
    // 地址卡片的下拉状态（国家对象、区号）要跨 openCard/submitCard 两个函数使用
    var addrState = null;

    // 可搜索下拉（国家/州省）：选项由调用方灌入，选中值放在 hidden input 的
    // data-field 上 —— submitCard 仍按 [data-field] 取值，普通字段与下拉字段共用一条路径。
    // 选项不超过 12 个就不出搜索框（多数国家的州省一屏能看全）。
    function buildSelect(key, placeholder, onPick) {
      var wrap = document.createElement('div');
      wrap.className = 'chat-card-select';
      var trig = document.createElement('button');
      trig.type = 'button';
      trig.className = 'chat-card-trigger';
      trig.setAttribute('aria-haspopup', 'listbox');
      trig.setAttribute('data-select-trigger', '');
      var flagEl = document.createElement('span');
      flagEl.className = 'chat-card-trig-flag';
      var txtEl = document.createElement('span');
      txtEl.className = 'chat-card-trig-txt is-ph';
      txtEl.textContent = placeholder;
      var chev = document.createElement('span');
      chev.className = 'chat-card-trig-chev';
      chev.innerHTML = CARD_ICONS.chevron;
      trig.appendChild(flagEl); trig.appendChild(txtEl); trig.appendChild(chev);
      var hidden = document.createElement('input');
      hidden.type = 'hidden';
      hidden.setAttribute('data-field', key);
      var drop = document.createElement('div');
      drop.className = 'chat-card-drop';
      drop.hidden = true;
      var search = document.createElement('input');
      search.type = 'text';
      search.className = 'chat-card-search';
      search.setAttribute('autocomplete', 'off');
      search.hidden = true;
      var opts = document.createElement('div');
      opts.className = 'chat-card-options';
      opts.setAttribute('role', 'listbox');
      drop.appendChild(search); drop.appendChild(opts);
      wrap.appendChild(trig); wrap.appendChild(hidden); wrap.appendChild(drop);

      var items = [];
      var selKey = null;
      function render(q) {
        opts.textContent = '';
        q = String(q || '').toLowerCase();
        var n = 0;
        for (var i = 0; i < items.length; i++) {
          var it = items[i];
          if (q && it.needle.indexOf(q) < 0) continue;
          if (++n > 300) break;
          var b = document.createElement('button');
          b.type = 'button';
          b.setAttribute('role', 'option');
          b.setAttribute('data-opt', it.key);
          if (it.key === selKey) b.classList.add('is-sel');
          if (it.flag) {
            var fl = document.createElement('span');
            fl.className = 'chat-opt-flag';
            fl.textContent = it.flag;
            b.appendChild(fl);
          }
          var nm = document.createElement('span');
          nm.className = 'chat-opt-name';
          nm.textContent = it.disp;
          b.appendChild(nm);
          if (it.dial) {
            var dl = document.createElement('span');
            dl.className = 'chat-opt-dial';
            dl.textContent = it.dial;
            b.appendChild(dl);
          }
          opts.appendChild(b);
        }
        if (!opts.firstChild) {
          var empty = document.createElement('span');
          empty.className = 'chat-opt-empty';
          empty.textContent = t('chatCardSearchEmpty', '无匹配结果');
          opts.appendChild(empty);
        }
      }
      function apply(it) {
        selKey = it.key;
        hidden.value = it.disp;
        flagEl.textContent = it.flag || '';
        txtEl.textContent = it.disp;
        txtEl.classList.remove('is-ph');
      }
      trig.addEventListener('click', function () {
        var open = drop.hidden;
        // 同时只开一个下拉，别让国家列表压住州省列表
        Array.prototype.forEach.call(cardPop.querySelectorAll('.chat-card-drop'), function (d) { if (d !== drop) d.hidden = true; });
        drop.hidden = !open;
        trig.setAttribute('aria-expanded', open ? 'true' : 'false');
        if (!drop.hidden) {
          render(search.value);
          if (!search.hidden) search.focus();
        }
      });
      search.addEventListener('input', function () { render(search.value); });
      opts.addEventListener('click', function (e) {
        var b = e.target.closest ? e.target.closest('[data-opt]') : null;
        if (!b) return;
        var it = null;
        for (var i = 0; i < items.length; i++) {
          if (String(items[i].key) === b.getAttribute('data-opt')) { it = items[i]; break; }
        }
        if (!it) return;
        apply(it);
        drop.hidden = true;
        trig.setAttribute('aria-expanded', 'false');
        if (onPick) onPick(it);
      });
      return {
        wrap: wrap,
        apply: apply,
        setItems: function (list) {
          items = list;
          search.hidden = list.length <= 12;
          search.setAttribute('placeholder', key === 'country'
            ? t('chatCardCountrySearch', '搜索国家/地区')
            : t('chatCardRegionSearch', '搜索省/州'));
          render(search.value);
        },
        clear: function () {
          selKey = null;
          hidden.value = '';
          flagEl.textContent = '';
          txtEl.textContent = placeholder;
          txtEl.classList.add('is-ph');
        },
        // 数据拉不到，或存过的不在数据集里的旧手写值：整体退回普通文本框，不挡发卡
        fallbackText: function (val) {
          var inp = document.createElement('input');
          inp.type = 'text';
          inp.value = val || '';
          inp.setAttribute('data-field', key);
          inp.setAttribute('autocomplete', 'off');
          wrap.replaceWith(inp);
          if (val) inp.focus();
        }
      };
    }

    function closeOpenDrops() {
      var closed = false;
      Array.prototype.forEach.call(cardPop.querySelectorAll('.chat-card-drop'), function (d) {
        if (!d.hidden) { d.hidden = true; closed = true; }
      });
      Array.prototype.forEach.call(cardPop.querySelectorAll('[data-select-trigger]'), function (tg) {
        tg.setAttribute('aria-expanded', 'false');
      });
      return closed;
    }
    // Esc 有两个监听器（历史遗留），一次按键两个都会跑：第一个收掉下拉时记下事件
    // timeStamp，第二个凭同一事件号跳过，才能做到「先收下拉、再收卡片」分两步。
    var escDropAt = -1;
    function escForCard(e) {
      if (!cardPop || cardPop.hidden) return false;
      if (closeOpenDrops()) { escDropAt = e.timeStamp; return true; }
      if (e.timeStamp === escDropAt) return true;
      return false;
    }

    function readCard(kind) {
      try { return JSON.parse(localStorage.getItem('nexchat.card.' + kind) || '{}') || {}; }
      catch (e) { return {}; }
    }
    function saveCard(kind, vals) {
      try { localStorage.setItem('nexchat.card.' + kind, JSON.stringify(vals)); } catch (e) { /* 隐私模式忽略 */ }
    }
    function closeCard() { cardPop.hidden = true; activeKind = null; }

    function openCard(kind) {
      if (activeKind === kind && !cardPop.hidden) { closeCard(); return; }
      if (emojiPop) emojiPop.hidden = true;
      if (morePop) morePop.hidden = true;
      activeKind = kind;
      var def = CARD_DEFS[kind];
      var saved = readCard(kind);
      cardPop.textContent = '';
      var h = document.createElement('h3'); h.textContent = def.title; cardPop.appendChild(h);
      var hint = document.createElement('p');
      hint.className = 'chat-card-hint';
      hint.textContent = t('chatCardHint', '信息仅用于本次询盘沟通');
      cardPop.appendChild(hint);
      var grid = document.createElement('div'); grid.className = 'chat-card-fields';
      // 地址卡片的国家/州省/电话走专用控件：对齐阿里「添加地址」的形态 ——
      // 国家带国旗可搜索、州省跟着国家换成下拉、电话自动带区号。
      // 数据是异步的：先铺好空下拉，拉到数据再灌选项并回填存过的值。
      var addr = { country: null, dial: '', dialEl: null, cityEl: null, pendingRegion: '' };
      addrState = kind === 'address' ? addr : null;
      var regionSel = null;
      function dispName(c) { return siteLangEn() ? c.en : c.zh; }
      function syncRegion(c) {
        if (!regionSel || regionSel.dead) return;
        var rs = c.r || [];
        if (!rs.length) {
          // 该国没有州省数据（如一些小国）：退回手写输入，与阿里的行为一致
          regionSel.fallbackText(addr.pendingRegion || '');
          return;
        }
        regionSel.setItems(rs.map(function (nm) { return { key: nm, disp: nm, needle: nm.toLowerCase() }; }));
        if (addr.pendingRegion && rs.indexOf(addr.pendingRegion) >= 0) {
          regionSel.apply({ key: addr.pendingRegion, disp: addr.pendingRegion, needle: addr.pendingRegion.toLowerCase() });
        } else {
          regionSel.clear();
        }
        addr.pendingRegion = '';
      }
      function buildCountry(label, savedVal) {
        var sel = buildSelect('country', t('chatCardCountrySelect', '选择国家/地区'), function (it) {
          // 用户换国家：城市跟着清空（阿里同款行为）；开卡回填上次选择时走的是
          // 下面的 restore 分支，不经过这里，已存的城市值不会被动到
          if (addr.country && addr.country.c !== it.raw.c && addr.cityEl) addr.cityEl.value = '';
          addr.country = it.raw;
          addr.dial = it.raw.dial || '';
          if (addr.dialEl) {
            addr.dialEl.textContent = addr.dial;
            addr.dialEl.hidden = !addr.dial;
          }
          syncRegion(it.raw);
        });
        label.appendChild(sel.wrap);
        loadCountries().then(function (list) {
          var items = list.map(function (c) {
            return {
              key: c.c, disp: dispName(c), flag: flagEmoji(c.c), dial: c.dial, raw: c,
              needle: (c.zh + ' ' + c.en + ' ' + c.c + ' ' + (c.dial || '')).toLowerCase()
            };
          });
          sel.setItems(items);
          var hit = null;
          for (var i = 0; i < items.length; i++) {
            if (items[i].raw.zh === savedVal || items[i].raw.en === savedVal) { hit = items[i]; break; }
          }
          if (hit) {
            sel.apply(hit);
            addr.country = hit.raw;
            addr.dial = hit.raw.dial || '';
            if (addr.dialEl) {
              addr.dialEl.textContent = addr.dial;
              addr.dialEl.hidden = !addr.dial;
            }
            syncRegion(hit.raw);
          } else if (savedVal) {
            sel.fallbackText(savedVal); // 旧手写值不在数据集里，保持可编辑
          }
        }).catch(function () {
          sel.fallbackText(savedVal); // 数据不可用也不挡发卡
        });
      }
      function buildRegion(label, savedVal) {
        addr.pendingRegion = savedVal || '';
        var sel = buildSelect('region', t('chatCardRegionSelect', '选择省/州'), function () { /* 州省无联动 */ });
        regionSel = sel;
        label.appendChild(sel.wrap);
      }
      function buildCity(label, savedVal) {
        var inp = document.createElement('input');
        inp.type = 'text';
        inp.value = savedVal || '';
        inp.setAttribute('data-field', 'city');
        inp.setAttribute('autocomplete', 'off');
        label.appendChild(inp);
        addr.cityEl = inp;
      }
      function buildPhone(label, saved) {
        var raw = String(saved.phone || '');
        // 上次提交时已拼过区号，回显时剥掉，避免前缀块 + 输入值重复
        if (saved.phoneDial && raw.indexOf(saved.phoneDial + ' ') === 0) raw = raw.slice(saved.phoneDial.length + 1);
        var wrap = document.createElement('div');
        wrap.className = 'chat-phone-wrap';
        var dial = document.createElement('span');
        dial.className = 'chat-phone-dial';
        dial.hidden = true;
        var inp = document.createElement('input');
        inp.type = 'tel';
        inp.value = raw;
        inp.setAttribute('data-field', 'phone');
        inp.setAttribute('autocomplete', 'off');
        wrap.appendChild(dial); wrap.appendChild(inp);
        label.appendChild(wrap);
        addr.dialEl = dial;
        if (saved.phoneDial) {
          addr.dial = saved.phoneDial;
          dial.textContent = saved.phoneDial;
          dial.hidden = false;
        }
      }
      def.fields.forEach(function (f) {
        var label = document.createElement('label');
        if (f.wide) label.className = 'wide';
        var span = document.createElement('span');
        span.textContent = f.label + (f.required ? ' *' : '');
        label.appendChild(span);
        if (kind === 'address' && f.key === 'country') buildCountry(label, saved.country);
        else if (kind === 'address' && f.key === 'region') buildRegion(label, saved.region);
        else if (kind === 'address' && f.key === 'city') buildCity(label, saved.city);
        else if (kind === 'address' && f.key === 'phone') buildPhone(label, saved);
        else {
          var inp = document.createElement('input');
          inp.type = f.key === 'email' ? 'email' : 'text';
          inp.value = saved[f.key] || '';
          inp.setAttribute('data-field', f.key);
          inp.setAttribute('autocomplete', 'off');
          label.appendChild(inp);
        }
        grid.appendChild(label);
      });
      cardPop.appendChild(grid);
      var actions = document.createElement('div'); actions.className = 'chat-card-actions';
      var cancel = document.createElement('button');
      cancel.type = 'button'; cancel.textContent = t('chatCardCancel', '取消');
      cancel.addEventListener('click', closeCard);
      var send = document.createElement('button');
      send.type = 'button'; send.className = 'is-primary';
      send.textContent = t('chatCardSend', '发送卡片');
      send.addEventListener('click', function () { submitCard(kind); });
      actions.appendChild(cancel); actions.appendChild(send);
      cardPop.appendChild(actions);
      cardPop.hidden = false;
      var first = cardPop.querySelector('input[type="text"],input[type="tel"],input[type="email"]');
      if (first) first.focus();
    }

    function submitCard(kind) {
      var def = CARD_DEFS[kind], vals = {}, missing = false;
      def.fields.forEach(function (f) {
        var el = cardPop.querySelector('[data-field="' + f.key + '"]');
        var v = el ? String(el.value || '').trim() : '';
        vals[f.key] = v;
        if (f.required && !v) missing = true;
      });
      if (missing) { note(t('chatCardRequired', '请填写必填项')); return; }
      // 地址卡片：把区号拼进电话值（用户自己带了 + 的不重复拼）；phoneDial 只进
      // localStorage（下次开卡还原前缀块），不进消息正文，消息里已含在电话值里。
      if (kind === 'address' && addrState) {
        vals.phoneDial = addrState.dial;
        if (vals.phone && addrState.dial && vals.phone.charAt(0) !== '+') {
          vals.phone = addrState.dial + ' ' + vals.phone;
        }
      }
      saveCard(kind, vals);
      var lines = [def.marker];
      def.fields.forEach(function (f) { if (vals[f.key]) lines.push(f.label + ': ' + vals[f.key]); });
      var text = lines.join('\n');
      if (input) {
        var cur = String(input.value || '').trim();
        input.value = cur ? cur + '\n\n' + text : text;
      }
      closeCard();
      // requestSubmit 会触发 form 的 submit 事件：商品引用上下文的 capture 监听与
      // 原挂件 send() 都会照常执行，不需要复制任何会话逻辑。
      if (form && typeof form.requestSubmit === 'function') form.requestSubmit();
      else { var sb = root.querySelector('[data-chat-send]'); if (sb) sb.click(); }
    }

    var bar = root.querySelector('.chat-compose-bar');
    if (bar && panel) {
      var frag = document.createDocumentFragment();
      [['name', CARD_ICONS.name, t('chatNameCard', '名牌卡片')],
       ['address', CARD_ICONS.address, t('chatAddressCard', '地址卡片')]].forEach(function (item) {
        var btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'chat-tool chat-card-btn';
        btn.setAttribute('data-chat-card-btn', item[0]);
        btn.setAttribute('aria-label', item[2]);
        btn.title = item[2];
        btn.innerHTML = item[1];
        btn.addEventListener('click', function () { openCard(item[0]); });
        frag.appendChild(btn);
      });
      bar.insertBefore(frag, bar.firstChild);
      document.addEventListener('click', function (e) {
        if (cardPop.hidden) return;
        if (cardPop.contains(e.target) || (e.target.closest && e.target.closest('[data-chat-card-btn]'))) return;
        closeCard();
      });
    }

    // ---- 表情与更多菜单 ----
    // 表情按钮懒构建：不占每次页面加载的成本，第一次点开才铺按钮；
    // 选中用浮层上的一个事件委托，不再给 176 个按钮各自挂监听。
    var emojiBuilt = false;
    var buildEmojis = function () {
      if (emojiBuilt) return;
      emojiBuilt = true;
      EMOJIS.forEach(function (em) {
        var b = document.createElement('button');
        b.type = 'button';
        b.textContent = em;
        b.title = em;
        b.setAttribute('aria-label', em);
        b.setAttribute('data-em', em);
        emojiPop.appendChild(b);
      });
    };
    if (emojiPop) {
      emojiPop.addEventListener('click', function (e) {
        var em = e.target && e.target.getAttribute && e.target.getAttribute('data-em');
        if (!em || !input) return;
        input.value = String(input.value || '') + em;
        // 选完就收起：浮层压在消息区上，留着挡内容；要连发再点一次按钮
        emojiPop.hidden = true;
        input.focus();
      });
    }
    if (emojiBtn && emojiPop) {
      emojiBtn.addEventListener('click', function () {
        if (!emojiPop.hidden) { emojiPop.hidden = true; return; }
        buildEmojis();
        emojiPop.hidden = false;
        if (morePop) morePop.hidden = true;
        if (cardPop) cardPop.hidden = true;
      });
    }
    if (moreBtn && morePop) {
      moreBtn.addEventListener('click', function () {
        morePop.hidden = !morePop.hidden;
        if (!morePop.hidden) {
          if (emojiPop) emojiPop.hidden = true;
          if (cardPop) cardPop.hidden = true;
        }
      });
    }
    // 点浮层以外的任何地方都收起：原来只有「再点一次按钮」才关，
    // 点聊天区、点页面空白、换了别的话题，这块大浮层就一直杵在消息上面。
    document.addEventListener('click', function (e) {
      var t = e.target;
      var inEmoji = t && t.closest && t.closest('[data-chat-emoji]');
      var inMore = t && t.closest && t.closest('[data-chat-more]');
      if (emojiPop && !emojiPop.hidden && !emojiPop.contains(t) && !inEmoji) emojiPop.hidden = true;
      if (morePop && !morePop.hidden && !morePop.contains(t) && !inMore) morePop.hidden = true;
      // 下拉展开层点外面就收：卡片区内部的点击（点别的字段）也算外面
      if (cardPop && !cardPop.hidden) {
        Array.prototype.forEach.call(cardPop.querySelectorAll('.chat-card-drop'), function (d) {
          if (!d.hidden && !d.parentElement.contains(t)) d.hidden = true;
        });
      }
    });
    document.addEventListener('keydown', function (e) {
      if (e.key !== 'Escape') return;
      if (emojiPop) emojiPop.hidden = true;
      if (morePop) morePop.hidden = true;
      // Esc 先收下拉再收卡片：连按两次才把整个地址卡关掉
      if (escForCard(e)) return;
      if (cardPop && !cardPop.hidden) closeCard();
    });
    // 「更多」菜单贴着头部下沿展开：头部高度会随「客服姓名 + 本地时间」那一行出现而变，
    // 写死 top 就会像之前那样压在标题和 × 上面，交给 ResizeObserver 实时量。
    var headEl = root.querySelector('.chat-head');
    if (headEl) {
      var syncHeadH = function () { panel.style.setProperty('--chat-head-h', headEl.offsetHeight + 'px'); };
      syncHeadH();
      if (window.ResizeObserver) new window.ResizeObserver(syncHeadH).observe(headEl);
    }
    if (expandBtn) {
      expandBtn.addEventListener('click', function () {
        state.expanded = !state.expanded;
        root.classList.toggle('is-expanded', state.expanded);
        expandBtn.textContent = state.expanded ? t('chatCollapse', '收起面板') : t('chatExpand', '展开面板');
        if (morePop) morePop.hidden = true;
      });
    }
    if (clearBtn) {
      clearBtn.addEventListener('click', function () {
        if (input) input.value = '';
        clearProduct();
        if (morePop) morePop.hidden = true;
        if (input) input.focus();
      });
    }
    function inNode(target, sel) {
      return target && target.closest ? target.closest(sel) : null;
    }
    document.addEventListener('click', function (e) {
      if (emojiPop && !emojiPop.hidden &&
          !inNode(e.target, '[data-chat-emoji-pop]') && !inNode(e.target, '[data-chat-emoji]')) emojiPop.hidden = true;
      if (morePop && !morePop.hidden &&
          !inNode(e.target, '[data-chat-more-pop]') && !inNode(e.target, '[data-chat-more]')) morePop.hidden = true;
    });
    document.addEventListener('keydown', function (e) {
      if (e.key !== 'Escape') return;
      if (emojiPop) emojiPop.hidden = true;
      if (morePop) morePop.hidden = true;
      // Esc 先收下拉再收卡片（与上面第一个 Esc 处理器同口径）
      if (escForCard(e)) return;
      closeCard();
      // 放大后的窗口用 Esc 还原，和浮层一样不必再去点菜单。
      if (state.expanded && expandBtn) expandBtn.click();
    });
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', init);
  else init();
})();
