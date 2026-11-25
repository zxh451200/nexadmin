// NexAdmin 官网默认模板脚本：滚动进场、导航状态、主题/语言切换与交互微效果。无外部依赖。
// 语言切换是整页跳转：语言由服务端决定并落 cookie，前端不做无刷新换页。
(function () {
  'use strict';

  // ---- 模块级状态 ----
  var i18n = {};
  var winHandlers = [];
  var revealObserver = null;

  function onWin(target, type, fn, opts) {
    target.addEventListener(type, fn, opts);
    winHandlers.push([target, type, fn, opts]);
  }
  function clearWinHandlers() {
    winHandlers.forEach(function (h) { h[0].removeEventListener(h[1], h[2], h[3]); });
    winHandlers.length = 0;
  }

  function readI18n() {
    try {
      var el = document.getElementById('site-i18n');
      if (el) return JSON.parse(el.textContent || '{}');
    } catch (e) { /* ignore */ }
    return {};
  }
  function msg(key, fallback) { return i18n[key] || fallback; }

  // ---- 复制文本：优先 Clipboard API；权限弹窗挂起 / 拒绝时超时回退 execCommand ----
  function legacyCopy(text) {
    return new Promise(function (resolve, reject) {
      var ta = document.createElement('textarea');
      ta.value = text;
      ta.setAttribute('readonly', '');
      ta.style.position = 'fixed';
      ta.style.opacity = '0';
      document.body.appendChild(ta);
      ta.select();
      var ok = false;
      try { ok = document.execCommand('copy'); } catch (e) { ok = false; }
      document.body.removeChild(ta);
      if (ok) resolve(); else reject(new Error('copy failed'));
    });
  }
  // 有些环境首次调用 clipboard.writeText 会一直挂着等授权：1.2s 没结果就走
  // execCommand，避免「点了没有任何下文」。
  function copyText(text) {
    if (!navigator.clipboard || !window.isSecureContext) return legacyCopy(text);
    return new Promise(function (resolve, reject) {
      var settled = false;
      var timer = window.setTimeout(function () {
        if (settled) return;
        settled = true;
        legacyCopy(text).then(resolve, reject);
      }, 1200);
      navigator.clipboard.writeText(text).then(function () {
        if (settled) return;
        settled = true; window.clearTimeout(timer); resolve();
      }, function () {
        if (settled) return;
        settled = true; window.clearTimeout(timer);
        legacyCopy(text).then(resolve, reject);
      });
    });
  }

  // ---- 轻提示 toast ----
  var toastEl = null;
  var toastTimer = null;
  function toast(text) {
    if (!toastEl || !toastEl.isConnected) {
      toastEl = document.createElement('div');
      toastEl.className = 'site-toast';
      toastEl.setAttribute('role', 'status');
      document.body.appendChild(toastEl);
    }
    toastEl.textContent = text;
    requestAnimationFrame(function () { toastEl.classList.add('show'); });
    if (toastTimer) window.clearTimeout(toastTimer);
    toastTimer = window.setTimeout(function () { toastEl.classList.remove('show'); }, 2200);
  }

  // ---- 配色切换（事件委托：换页后的新按钮也直接生效） ----
  function applyThemeMeta(scheme) {
    var meta = document.querySelector('meta[name="theme-color"]');
    if (meta) meta.setAttribute('content', scheme === 'light' ? '#f7f8f8' : '#08090a');
  }
  document.addEventListener('click', function (e) {
    var btn = e.target.closest('[data-theme-toggle]');
    if (!btn) return;
    var root = document.documentElement;
    var next = root.getAttribute('data-scheme') === 'light' ? 'dark' : 'light';
    root.setAttribute('data-scheme', next);
    try { localStorage.setItem('nex-site-theme', next); } catch (err) { /* ignore */ }
    applyThemeMeta(next);
  });

  // ---- 多级导航：箭头按钮切换展开（桌面悬停由 CSS 负责，触屏靠这里） ----
  function closeNavItems(except) {
    document.querySelectorAll('.topnav .nav-item.is-open').forEach(function (n) {
      if (n === except) return;
      n.classList.remove('is-open');
      var b = n.querySelector('[data-nav-toggle]');
      if (b) b.setAttribute('aria-expanded', 'false');
    });
  }
  document.addEventListener('click', function (e) {
    var btn = e.target.closest('[data-nav-toggle]');
    if (btn) {
      var item = btn.closest('.nav-item');
      if (!item) return;
      var open = !item.classList.contains('is-open');
      closeNavItems(item);
      item.classList.toggle('is-open', open);
      btn.setAttribute('aria-expanded', open ? 'true' : 'false');
      return;
    }
    if (!e.target.closest('.topnav .nav-item')) closeNavItems(null);
  });
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') closeNavItems(null);
  });

  // ---- 品牌主题移动菜单：details 原生开合，脚本只补背景锁滚与关闭行为 ----
  document.querySelectorAll('[data-mnav]').forEach(function (menu) {
    var syncMenu = function () { document.body.classList.toggle('mnav-open', menu.open); };
    menu.addEventListener('toggle', syncMenu);
    menu.addEventListener('click', function (e) {
      if (e.target.closest('a')) menu.open = false;
    });
    menu.addEventListener('keydown', function (e) {
      if (e.key === 'Escape' && menu.open) {
        menu.open = false;
        menu.querySelector('summary').focus();
      }
    });
  });
  document.addEventListener('click', function (e) {
    document.querySelectorAll('[data-mnav][open]').forEach(function (menu) {
      if (!menu.contains(e.target)) menu.open = false;
    });
  });
  onWin(window, 'resize', function () {
    if (window.innerWidth > 980) document.querySelectorAll('[data-mnav][open]').forEach(function (menu) { menu.open = false; });
  }, { passive: true });

  // ---- 复制安装命令 / 复制邮箱（委托） ----
  document.addEventListener('click', function (e) {
    var copyBtn = e.target.closest('[data-copy]');
    if (copyBtn) {
      var src = document.querySelector(copyBtn.getAttribute('data-copy'));
      if (!src) return;
      var label = copyBtn.querySelector('[data-copy-label]');
      var original = label ? label.textContent : copyBtn.textContent;
      var setLabel = function (text) {
        if (label) label.textContent = text; else copyBtn.textContent = text;
      };
      copyText(src.textContent.trim()).then(function () {
        setLabel(msg('copied', 'Copied'));
        copyBtn.classList.add('copied');
        window.setTimeout(function () {
          setLabel(original);
          copyBtn.classList.remove('copied');
        }, 1800);
      }).catch(function () {
        setLabel(msg('copyManual', 'Copy manually'));
        window.setTimeout(function () { setLabel(original); }, 1800);
      });
      return;
    }
    var mail = e.target.closest('a[data-copy-email]');
    if (!mail) return;
    // mailto 在没配邮件客户端的机器上点了没反应：主点击改为复制地址，任何环境都有反馈。
    if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    var addr = mail.getAttribute('data-copy-email');
    copyText(addr).then(function () {
      toast(msg('emailCopied', 'Email copied'));
    }).catch(function () {
      toast(addr);
    });
  });

  // ---- 语言菜单开合（委托：<details> 原生负责开合，这里只管关） ----
  // 选择器按 data 属性匹配：内置主题与品牌主题（如 Solaris）的弹层外壳类名不同，
  // 行为钩子统一走 data-locale-pop / data-ship-to。
  document.addEventListener('click', function (e) {
    document.querySelectorAll('details[data-locale-pop][open],details[data-ship-to][open],details.lang-switch[open]').forEach(function (box) {
      if (!box.contains(e.target)) box.removeAttribute('open');
    });
  });
  document.addEventListener('keydown', function (e) {
    if (e.key !== 'Escape') return;
    document.querySelectorAll('details[data-locale-pop][open],details[data-ship-to][open],details.lang-switch[open]').forEach(function (box) {
      box.removeAttribute('open');
    });
  });

  // ---- 「设置语言和币种」弹层（对标阿里右上角）：服务端出结构，这里只管保存 ----
  // 币种写一年期全站 cookie（服务端 render 按 cookie 换算价格），语言跳模板写好的
  // Switch 地址；币种变了就刷新当前页，语言变了跳对应语言页（cookie 先落盘再跳）。
  var localeMenu = document.querySelector('[data-locale-menu]');
  if (localeMenu) {
    var localeSave = localeMenu.querySelector('[data-locale-save]');
    var localeLang = localeMenu.querySelector('[data-locale-lang]');
    var localeCur = localeMenu.querySelector('[data-locale-currency]');
    if (localeSave && localeLang && localeCur) {
      localeSave.addEventListener('click', function () {
        // 选中态是否被用户改过：对照服务端模板写死的 defaultSelected，
        // 不跟页面 URL 比（Switch 地址带不带查询串对不上会误判）。
        var curChanged = !localeCur.selectedOptions[0] || !localeCur.selectedOptions[0].defaultSelected;
        var langChanged = !localeLang.selectedOptions[0] || !localeLang.selectedOptions[0].defaultSelected;
        if (curChanged) {
          document.cookie = 'nex-site-currency=' + encodeURIComponent(localeCur.value) + ';path=/;max-age=31536000;samesite=Lax';
        }
        if (langChanged) {
          window.location.href = localeLang.value;
          return;
        }
        if (curChanged) window.location.reload();
        else localeMenu.closest('details').removeAttribute('open');
      });
    }
  }

  // ---- 「设置语言和币种」弹层里的下拉：原生 select 的展开列表由系统绘制 ----
  // （系统蓝高亮 + 方框白卡）与弹层观感割裂，这里在原控件上叠一层同宽自绘 listbox。
  // 原生 <select> 仍是唯一数据源（上面的保存逻辑照旧读 selectedOptions/defaultSelected），
  // 只在包装成功后才隐藏它：无 JS 或初始化失败时退回原生控件。
  var nxSelects = [];
  function nxCloseAll(except) {
    nxSelects.forEach(function (s) { if (s !== except) s.close(false); });
  }
  document.querySelectorAll('[data-locale-menu] select').forEach(function (sel) {
    if (sel.disabled || !sel.options.length) return;
    var wrap = document.createElement('div');
    wrap.className = 'nx-select';
    var btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'nx-select-btn';
    btn.setAttribute('aria-haspopup', 'listbox');
    btn.setAttribute('aria-expanded', 'false');
    var label = document.createElement('span');
    label.className = 'nx-select-label';
    var arrow = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    arrow.setAttribute('class', 'nx-select-arrow');
    arrow.setAttribute('viewBox', '0 0 12 12');
    arrow.setAttribute('aria-hidden', 'true');
    var arrowPath = document.createElementNS('http://www.w3.org/2000/svg', 'path');
    arrowPath.setAttribute('d', 'M2.5 4.25 6 7.75l3.5-3.5');
    arrowPath.setAttribute('fill', 'none');
    arrowPath.setAttribute('stroke', 'currentColor');
    arrowPath.setAttribute('stroke-width', '1.4');
    arrowPath.setAttribute('stroke-linecap', 'round');
    arrowPath.setAttribute('stroke-linejoin', 'round');
    arrow.appendChild(arrowPath);
    btn.appendChild(label);
    btn.appendChild(arrow);
    var list = document.createElement('ul');
    list.className = 'nx-select-list';
    list.setAttribute('role', 'listbox');
    list.tabIndex = -1;
    list.hidden = true;
    var lis = [];
    var seq = nxSelects.length;
    Array.prototype.forEach.call(sel.options, function (o, i) {
      var li = document.createElement('li');
      li.className = 'nx-select-opt';
      li.id = 'nxopt-' + seq + '-' + i;
      li.setAttribute('role', 'option');
      li.setAttribute('data-index', String(i));
      li.textContent = o.text;
      list.appendChild(li);
      lis.push(li);
    });
    // 包装插在原生 select 前：label 的第一个可标注控件变成自绘按钮，
    // 点「语言」「币种」字段文字也能开合下拉。
    sel.parentNode.insertBefore(wrap, sel);
    wrap.appendChild(btn);
    wrap.appendChild(list);
    wrap.appendChild(sel);
    var active = 0;
    var api = { close: close };
    nxSelects.push(api);

    function syncLabel() {
      var cur = sel.options[sel.selectedIndex];
      label.textContent = cur ? cur.text : '';
      lis.forEach(function (li, i) {
        var on = i === sel.selectedIndex;
        li.setAttribute('aria-selected', on ? 'true' : 'false');
        li.classList.toggle('is-selected', on);
      });
    }
    function mark() {
      lis.forEach(function (li, i) { li.classList.toggle('is-active', i === active); });
      if (lis[active]) list.setAttribute('aria-activedescendant', lis[active].id);
    }
    function close(backToBtn) {
      if (list.hidden) return;
      list.hidden = true;
      wrap.classList.remove('is-open');
      btn.setAttribute('aria-expanded', 'false');
      list.removeAttribute('aria-activedescendant');
      if (backToBtn) btn.focus();
    }
    function openAt(i) {
      nxCloseAll(api);
      active = Math.max(0, Math.min(i, lis.length - 1));
      list.hidden = false;
      wrap.classList.add('is-open');
      btn.setAttribute('aria-expanded', 'true');
      mark();
      list.focus();
    }
    function choose(i) {
      if (i < 0 || i >= lis.length) return;
      sel.selectedIndex = i;
      syncLabel();
      sel.dispatchEvent(new Event('change', { bubbles: true }));
      close(true);
    }

    btn.addEventListener('click', function () {
      if (list.hidden) openAt(sel.selectedIndex);
      else close(false);
    });
    btn.addEventListener('keydown', function (e) {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault();
        openAt(e.key === 'ArrowDown' ? sel.selectedIndex : lis.length - 1);
      }
    });
    list.addEventListener('click', function (e) {
      var li = e.target.closest('.nx-select-opt');
      if (!li) return;
      // 选项在 <label> 里：不拦这次点击，label 激活会把点击再转给触发钮，
      // 选中后刚收起的列表又被立刻打开，看起来怎么点都关不上。
      e.preventDefault();
      e.stopPropagation();
      choose(Number(li.getAttribute('data-index')));
    });
    list.addEventListener('keydown', function (e) {
      var k = e.key;
      if (k === 'ArrowDown' || k === 'ArrowUp') {
        e.preventDefault();
        active = (active + (k === 'ArrowDown' ? 1 : lis.length - 1)) % lis.length;
        mark();
        lis[active].scrollIntoView({ block: 'nearest' });
      } else if (k === 'Home' || k === 'End') {
        e.preventDefault();
        active = k === 'Home' ? 0 : lis.length - 1;
        mark();
        lis[active].scrollIntoView({ block: 'nearest' });
      } else if (k === 'Enter' || k === ' ' || k === 'Spacebar') {
        e.preventDefault();
        choose(active);
      } else if (k === 'Escape') {
        e.stopPropagation();
        close(true);
      } else if (k === 'Tab') {
        close(false);
      }
    });
    document.addEventListener('click', function (e) {
      if (!wrap.contains(e.target)) close(false);
    });
    syncLabel();
  });

  // ---- 交货至（对标阿里「交货至: 🇨🇳 CN」）：纯展示 + localStorage 记忆 ----
  // 国家清单复用地址卡的 country-region.json（{c: ISO2, zh, en, dial}），首次展开才拉。
  // 只管展示与记忆；询盘/客服侧通过 chat-enhance 读同一个 localStorage 键取默认国家。
  var shipTo = document.querySelector('[data-ship-to]');
  if (shipTo) {
    var SHIP_KEY = 'nex-site-shipto';
    var shipVal = shipTo.querySelector('[data-ship-to-val]');
    var shipList = shipTo.querySelector('[data-ship-to-list]');
    var shipSearch = shipTo.querySelector('[data-ship-to-search]');
    var shipData = null;
    // 国旗 = ISO2 码两个字母各加 regional-indicator 偏移（与 chat-enhance 同口径）
    function shipFlag(code) {
      var out = '';
      for (var i = 0; i < code.length; i++) out += String.fromCodePoint(0x1F1E6 + code.charCodeAt(i) - 65);
      return out;
    }
    function shipName(c) {
      return /^en/i.test(String(document.documentElement.lang || '')) ? c.en : (c.zh || c.en);
    }
    function shipRender(filter) {
      if (!shipList) return;
      var f = String(filter || '').toLowerCase();
      shipList.textContent = '';
      var added = 0;
      (shipData || []).forEach(function (c) {
        // 过滤对中英文名与 ISO2 码都开放（与地址卡国家搜索的 needle 同口径）：
        // 中文界面下访客同样会用英文名搜（Germany 而不是 德国）
        var name = shipName(c) || c.c;
        var needle = (c.zh + ' ' + c.en + ' ' + c.c).toLowerCase();
        if (f && needle.indexOf(f) < 0) return;
        if (added >= 60) return;
        added++;
        var b = document.createElement('button');
        b.type = 'button';
        b.setAttribute('role', 'option');
        b.setAttribute('data-ship-code', c.c);
        b.textContent = shipFlag(c.c) + ' ' + name;
        shipList.appendChild(b);
      });
      if (!added) {
        var empty = document.createElement('p');
        empty.className = 'ship-to-empty';
        empty.textContent = msg('shipToEmpty', 'No matches');
        shipList.appendChild(empty);
      }
    }
    function shipApply(code) {
      if (!shipVal) return;
      var c = (shipData || []).filter(function (x) { return x.c === code; })[0];
      // Windows 没有国旗字体：🇦🇸 会退化成两个字母，再拼 ISO 码就成了「AS AS」。
      // 触发钮只放 ISO 码（全名在 title 里）；下拉列表仍是「国旗/字母 + 国家名」。
      shipVal.textContent = code;
      shipVal.hidden = false;
      if (c) shipVal.setAttribute('title', shipName(c));
    }
    function shipSet(code) {
      try { localStorage.setItem(SHIP_KEY, code); } catch (err) { /* 隐私模式忽略 */ }
      shipApply(code);
      shipTo.removeAttribute('open');
    }
    // 初次渲染摘要：存过的国家 > 默认 US。清单没到位时也先亮出码，展开后再补 title。
    var shipSaved = 'US';
    try { shipSaved = localStorage.getItem(SHIP_KEY) || 'US'; } catch (err) { /* 隐私模式忽略 */ }
    shipApply(shipSaved);
    shipTo.addEventListener('toggle', function () {
      if (!shipTo.open || !shipList) return;
      if (shipData) { shipRender(''); if (shipSearch) shipSearch.focus(); return; }
      shipRender('');
      if (window.fetch) {
        fetch('/website-assets/country-region.json', { credentials: 'same-origin' })
          .then(function (res) { if (!res.ok) throw new Error('unavailable'); return res.json(); })
          .then(function (list) {
            shipData = Array.isArray(list) ? list : [];
            shipRender('');
            shipApply(shipSaved);
          })
          .catch(function () { shipData = []; shipRender(''); });
      }
      if (shipSearch) shipSearch.focus();
    });
    if (shipSearch) shipSearch.addEventListener('input', function () { shipRender(shipSearch.value); });
    if (shipList) shipList.addEventListener('click', function (e) {
      var b = e.target.closest('[data-ship-code]');
      if (b) shipSet(b.getAttribute('data-ship-code'));
    });
  }

  // ---- AI 提示框 CTA：提交后平滑滚到快速开始（仅首页渲染该表单） ----
  document.addEventListener('submit', function (e) {
    var form = e.target.closest('[data-prompt]');
    if (!form) return;
    e.preventDefault();
    var target = document.getElementById('quickstart');
    if (target) {
      target.scrollIntoView({ behavior: 'smooth', block: 'start' });
    } else {
      location.href = form.getAttribute('action') || '/';
    }
  });

  // ---- 商品图集：悬停放大镜（阿里商详口径）+ 点开 Viewer.js 全屏看大图 ----
  // Viewer.js 1.15.2（MIT，https://github.com/fengyuanchen/viewerjs）：开源热门、持续维护，
  // 比 PhotoSwipe 多了旋转/翻转/缩略图导航/自动播放/放大镜，触摸与键盘也都在。
  // 资源随插件 go:embed 交付，放在 /website-assets/vendor/viewerjs/，不引 CDN；
  // 首屏不下这个包，第一次点图才注入脚本（49KB），没网也能用，因为文件和站点同一个二进制。
  var VIEWER_JS = '/website-assets/vendor/viewerjs/viewer.min.js?v=1.15.2';
  var VIEWER_CSS = '/website-assets/vendor/viewerjs/viewer.min.css?v=1.15.2';
  var viewerCtor = null;
  var viewerLoading = null;

  function ensureViewer() {
    if (viewerCtor) return Promise.resolve(viewerCtor);
    if (viewerLoading) return viewerLoading;
    if (!document.getElementById('viewerjs-style')) {
      var link = document.createElement('link');
      link.id = 'viewerjs-style';
      link.rel = 'stylesheet';
      link.href = VIEWER_CSS;
      document.head.appendChild(link);
    }
    viewerLoading = new Promise(function (resolve) {
      if (window.Viewer) { viewerCtor = window.Viewer; resolve(viewerCtor); return; }
      var tag = document.createElement('script');
      tag.src = VIEWER_JS;
      tag.onload = function () { viewerCtor = window.Viewer || null; resolve(viewerCtor); };
      tag.onerror = function () { resolve(null); };
      document.head.appendChild(tag);
    }).then(function (ctor) {
      if (!ctor) viewerLoading = null; // 失败时允许下次点图重试，不把失败态缓存住
      return ctor;
    });
    return viewerLoading;
  }

  function galleryStage() { return document.querySelector('[data-pd-stage]'); }

  function activeThumb(btn) {
    var gallery = btn.closest('[data-product-gallery]');
    if (!gallery) return;
    Array.prototype.forEach.call(gallery.querySelectorAll('.product-gallery-thumb'), function (t) {
      var on = t === btn;
      t.classList.toggle('active', on);
      if (on) t.setAttribute('aria-current', 'true');
      else t.removeAttribute('aria-current');
    });
  }

  function unmountVideo() {
    var stage = galleryStage();
    var box = stage && stage.querySelector('[data-pd-video]');
    if (box) {
      var media = box.querySelector('video, iframe');
      // 先把 src 摘掉再删节点：否则 iframe / video 的网络请求与播放会拖到 GC 才停。
      if (media) media.removeAttribute('src');
      if (box.parentNode) box.parentNode.removeChild(box);
    }
    if (stage) stage.classList.remove('is-video');
  }

  // 视频缩略图：直链文件用原生 <video>，YouTube / Vimeo / Bilibili 用各自嵌入播放器。
  // 到底用哪种由服务端 Product.Video() 判定（域名白名单），前端只认 data-video-kind。
  function mountVideo(btn) {
    var stage = galleryStage();
    var kind = btn.getAttribute('data-video-kind');
    var src = btn.getAttribute('data-video-src');
    if (!stage || !src) return;
    unmountVideo();
    // 鼠标可能正停在主图上：放大镜与浮层要一起收掉，否则会盖在播放器上
    hideGalleryZoom();
    var box = document.createElement('div');
    box.className = 'pd-video';
    box.setAttribute('data-pd-video', '');
    var node;
    if (kind === 'file') {
      node = document.createElement('video');
      node.controls = true;
      node.autoplay = true;
      node.playsInline = true;
      node.preload = 'metadata';
      var poster = btn.getAttribute('data-video-poster');
      if (poster) node.poster = poster;
      node.src = src;
    } else {
      node = document.createElement('iframe');
      node.src = src;
      node.loading = 'lazy';
      node.allow = 'accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture; fullscreen';
      node.allowFullscreen = true;
      node.setAttribute('frameborder', '0');
      node.setAttribute('referrerpolicy', 'strict-origin-when-cross-origin');
    }
    node.setAttribute('title', msg('videoAria', '产品视频'));
    box.appendChild(node);
    stage.appendChild(box);
    stage.classList.add('is-video');
    activeThumb(btn);
  }

  // 缩略图统一切换：视频缩略图挂播放器并收起主图，图片缩略图换主图并卸掉播放器。
  document.addEventListener('click', function (e) {
    var btn = e.target.closest ? e.target.closest('.product-gallery-thumb') : null;
    if (!btn) return;
    if (btn.classList.contains('pd-thumb-video')) {
      mountVideo(btn);
      return;
    }
    var src = btn.getAttribute('data-gallery-src');
    var main = document.querySelector('.product-gallery-main');
    if (!src || !main) return;
    unmountVideo();
    // 换图时先收回旧的放大：倍数是按上一张的显示尺寸量出来的
    hideGalleryZoom();
    if (main.getAttribute('src') !== src) main.src = src;
    main.setAttribute('data-zoom', src);
    activeThumb(btn);
  });

  // 商品主图悬停放大：阿里商详口径——鼠标移上去直接在原地放大，不弹侧边窗口。
  // 实现只有「把光标位置换算成 transform-origin + scale」，舞台 overflow:hidden 裁掉溢出部分。
  // 不引第三方库：侧边面板那套（Drift 的 zoomPane）要自己算面板坐标，窄屏、弹层、滚动容器里
  // 全都要重算，而原地方案把这些交给 CSS，滚动/吸顶/移动端都不用特殊处理，也不多 16KB。
  // 倍数按图源分辨率收口：原图不够大时硬放只会得到一团糊。
  function initGalleryZoom() {
    var stage = galleryStage();
    var img = stage && stage.querySelector('.pd-main-img');
    if (!stage || !img || img.tagName !== 'IMG') return;
    // 触屏没有 hover：不接管，点图直接进 Viewer.js（那里有双指缩放、旋转、拖拽）
    if (!window.matchMedia('(hover: hover) and (pointer: fine)').matches) return;

    function fitZoom() {
      if (!img.naturalWidth || !img.naturalHeight) return 2.4;
      var r = img.getBoundingClientRect();
      if (!r.width || !r.height) return 2.4;
      var fit = Math.min(img.naturalWidth / r.width, img.naturalHeight / r.height);
      return Math.max(1.4, Math.min(2.4, Math.round(fit * 10) / 10));
    }

    function move(e) {
      // 视频占着舞台时图片是隐藏的，别放大
      if (stage.classList.contains('is-video')) return;
      var r = img.getBoundingClientRect();
      if (!r.width || !r.height) return;
      var x = Math.max(0, Math.min(100, ((e.clientX - r.left) / r.width) * 100));
      var y = Math.max(0, Math.min(100, ((e.clientY - r.top) / r.height) * 100));
      img.style.transformOrigin = x.toFixed(2) + '% ' + y.toFixed(2) + '%';
      if (stage.classList.contains('is-zooming')) return;
      stage.classList.add('is-zooming');
      img.style.transform = 'scale(' + fitZoom() + ')';
    }

    function off() {
      if (!stage.classList.contains('is-zooming')) return;
      stage.classList.remove('is-zooming');
      img.style.transform = '';
    }

    stage.addEventListener('mousemove', move);
    // 从视频切回图片时鼠标可能一直停在原地：mouseenter 不会再触发，mousemove 兜住
    stage.addEventListener('mouseenter', move);
    stage.addEventListener('mouseleave', off);
    // 滚动时主图位移了、光标没动，放大原点就对不上：先收起，下一次移动再算
    window.addEventListener('scroll', off, { passive: true });
    // 换图/窗口宽度变化后显示的尺寸变了，按新尺寸重新收口倍数
    img.addEventListener('load', function () {
      if (stage.classList.contains('is-zooming')) img.style.transform = 'scale(' + fitZoom() + ')';
    });
    window.addEventListener('resize', off);
  }

  /** 收起悬停放大与放大原点（切视频、滚动、换图时用） */
  function hideGalleryZoom() {
    document.querySelectorAll('.pd-stage.is-zooming').forEach(function (n) {
      n.classList.remove('is-zooming');
      var im = n.querySelector('.pd-main-img');
      if (im) im.style.transform = '';
    });
  }

  function lbSrc(img) { return img.currentSrc || img.getAttribute('src') || ''; }

  /** 取这一组图：图集主图 = 封面 + 缩略图；正文 = 同一 .article-body 内的图（跳过外链图） */
  function lbCollect(img) {
    var list = [];
    var i;
    var gallery = img.closest('[data-product-gallery]');
    if (gallery) {
      var main = gallery.querySelector('.pd-main-img');
      if (main && main.getAttribute('src')) list.push({ src: lbSrc(main), alt: img.getAttribute('alt') || '' });
      var thumbs = gallery.querySelectorAll('.product-gallery-thumb[data-gallery-src]');
      for (i = 0; i < thumbs.length; i++) list.push({ src: thumbs[i].getAttribute('data-gallery-src'), alt: img.getAttribute('alt') || '' });
    } else {
      var body = img.closest('.article-body');
      var nodes = body ? body.querySelectorAll('img') : [img];
      for (i = 0; i < nodes.length; i++) {
        var n = nodes[i];
        if (n.closest('a') || n.hasAttribute('data-no-zoom')) continue;
        list.push({ src: lbSrc(n), alt: n.getAttribute('alt') || '' });
      }
    }
    var seen = {};
    list = list.filter(function (it) {
      if (!it.src || seen[it.src]) return false;
      seen[it.src] = 1;
      return true;
    });
    if (list.length === 0) list.push({ src: lbSrc(img), alt: img.getAttribute('alt') || '' });
    return list;
  }

  // Viewer.js 从容器的 <img> 列表里取图：把 lbCollect 的结果临时铺到一个隐藏容器，
  // 这样商品主图、缩略图、正文图都能进同一组，翻页和缩略图导航自然连起来。
  function openLb(img) {
    var items = lbCollect(img);
    var want = lbSrc(img);
    var index = 0;
    for (var i = 0; i < items.length; i++) {
      if (items[i].src === want) { index = i; break; }
    }
    ensureViewer().then(function (Viewer) {
      // 脚本没加载成功（极老的浏览器 / 本地资源被拦）时退化成新窗口打开原图，不装死。
      if (!Viewer) { window.open(want, '_blank', 'noopener'); return; }
      var host = document.createElement('div');
      host.className = 'site-viewer-host';
      host.setAttribute('aria-hidden', 'true');
      items.forEach(function (it) {
        var node = document.createElement('img');
        node.src = it.src;
        node.alt = it.alt || '';
        node.setAttribute('data-zoom-src', it.src);
        host.appendChild(node);
      });
      document.body.appendChild(host);

      var instance = null;
      instance = new Viewer(host, {
        url: 'data-zoom-src',
        initialViewIndex: index,
        inline: false,
        button: true,       // 右上角关闭（模态模式）
        navbar: items.length > 1,
        title: false,
        toolbar: {
          zoomIn: true, zoomOut: true, oneToOne: true, reset: true,
          prev: items.length > 1, play: items.length > 1, next: items.length > 1,
          rotateLeft: true, rotateRight: true, flipHorizontal: true, flipVertical: true
        },
        tooltip: true,
        transition: true,
        keyboard: true,
        focus: true,
        zoomRatio: 0.15,
        minZoomRatio: 0.1,
        maxZoomRatio: 10,
        zIndex: 10010,      // 比客服挂件/询盘弹窗高一档
        toggleOnDblclick: true,
        zoomOnWheel: true,
        slideOnTouch: true,
        shown: function () { viewerLabels(instance); },
        hidden: function () {
          if (instance) instance.destroy();
          if (host.parentNode) host.parentNode.removeChild(host);
        }
      });
      instance.show();
    });
  }

  // Viewer.js 1.x 的工具栏只有图形、导航按钮写死英文；这里按本站点语言补上可访问名称，
  // 不另造一套按钮，纯改 aria-label/title，图标和交互仍是组件的。
  function viewerLabels(instance) {
    if (!instance || !instance.viewer) return;
    var map = {
      'zoom-in': 'zoomIn', 'zoom-out': 'zoomOut', 'one-to-one': 'zoomOneToOne',
      'reset': 'zoomReset', 'prev': 'zoomPrev', 'play': 'zoomPlay', 'next': 'zoomNext',
      'rotate-left': 'zoomRotateLeft', 'rotate-right': 'zoomRotateRight',
      'flip-horizontal': 'zoomFlipX', 'flip-vertical': 'zoomFlipY', 'mix': 'zoomClose'
    };
    Array.prototype.forEach.call(instance.viewer.querySelectorAll('[data-viewer-action]'), function (el) {
      var key = map[el.getAttribute('data-viewer-action')];
      if (!key) return;
      var text = msg(key, key);
      el.setAttribute('aria-label', text);
      el.setAttribute('title', text);
    });
  }

  /** 主图与正文图补上「可点开」语义：键盘可聚焦、Enter/Space 打开。
   *  新闻详情的封面图（.article-cover）同口径——此前漏了它，封面点不动而正文图能放大。
   *  主题模板的封面类带品牌前缀（sl-/tk-/au-article-cover），用后缀选择器一并覆盖。 */
  function decorateZoomables() {
    var nodes = document.querySelectorAll('[data-product-gallery] .pd-main-img, .article-body img, .article-cover, [class$="-article-cover"]');
    for (var i = 0; i < nodes.length; i++) {
      var img = nodes[i];
      if (img.closest('a') || img.hasAttribute('data-no-zoom')) continue;
      img.classList.add('is-zoomable');
      if (!img.hasAttribute('tabindex')) img.setAttribute('tabindex', '0');
      img.setAttribute('role', 'button');
      if (!img.getAttribute('aria-label')) img.setAttribute('aria-label', msg('zoom', '查看大图'));
    }
  }

  document.addEventListener('click', function (e) {
    var img = e.target.closest ? e.target.closest('img.is-zoomable') : null;
    if (!img || img.closest('a')) return;
    e.preventDefault();
    openLb(img);
  });
  document.addEventListener('keydown', function (e) {
    if (e.key !== 'Enter' && e.key !== ' ') return;
    var a = document.activeElement;
    if (a && a.tagName === 'IMG' && a.classList.contains('is-zoomable')) {
      e.preventDefault();
      openLb(a);
    }
  });

  // ---- 联系页办公地点地图（天地图）：脚本已在页面内同步加载（带 tk），window.T 就绪后打点。
  // 谷歌渠道走 iframe embed，无需 JS。加载失败静默缺席，地址卡仍可读。 ----
  function initOfficeMap() {
    var el = document.querySelector('[data-office-map]');
    if (!el || typeof window.T === 'undefined') return;
    var lng = parseFloat(el.getAttribute('data-lng'));
    var lat = parseFloat(el.getAttribute('data-lat'));
    if (isNaN(lng) || isNaN(lat)) return;
    var label = el.getAttribute('data-label') || '';
    try {
      var map = new T.Map(el, { minZoom: 3, maxZoom: 18 });
      map.centerAndZoom(new T.LngLat(lng, lat), 15);
      var marker = new T.Marker(new T.LngLat(lng, lat));
      map.addOverLay(marker);
      if (label) {
        marker.addEventListener('click', function () {
          var info = new T.InfoWindow();
          info.setContent('<div style="font:13px/1.6 sans-serif;min-width:140px">' + label.replace(/</g, '&lt;') + '</div>');
          map.openInfoWindow(info, new T.LngLat(lng, lat));
        });
      }
    } catch (e) { /* 地图初始化失败不挡联系页，地址卡仍可读 */ }
  }

  // ---- FAQ 搜索：/faq 独立页的纯前端过滤 ----
  // 问题与答案一起匹配（textContent 天然覆盖两层）；清空输入即恢复全量，
  // 打开状态原样保留——过滤不打断正在读的条目。
  function initFaqSearch() {
    var input = document.querySelector('[data-faq-search]');
    var list = document.querySelector('[data-faq-list]');
    if (!input || !list) return;
    var empty = document.querySelector('[data-faq-empty]');
    var items = Array.prototype.slice.call(list.querySelectorAll('.hp-faq-item'));
    input.addEventListener('input', function () {
      var kw = input.value.trim().toLowerCase();
      var shown = 0;
      items.forEach(function (item) {
        var hit = !kw || item.textContent.toLowerCase().indexOf(kw) !== -1;
        item.hidden = !hit;
        if (hit) shown++;
      });
      if (empty) empty.hidden = shown > 0;
    });
  }

  // ---- 页面级绑定 ----
  function initSite() {
    i18n = readI18n();
    injectCSS('site-tip-style', TIP_CSS);
    initContactForm();
    initRatingForm();
    initOfficeMap();
    initChat();
    initShare();
    initProductDetail();
    initOrderStatus();
    initProductSort();
    initProductDemo();
    initFaqSearch();
    decorateZoomables();
    clearWinHandlers();
    if (revealObserver) { revealObserver.disconnect(); revealObserver = null; }

    var reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    var fine = window.matchMedia('(hover: hover) and (pointer: fine)').matches;

    // 语言提示条：本次会话关过就不再出现。
    var langBanner = document.querySelector('[data-lang-banner]');
    if (langBanner) {
      var bannerClose = langBanner.querySelector('[data-lang-banner-close]');
      try {
        if (window.sessionStorage.getItem('nex_lang_banner_dismissed') === '1') langBanner.hidden = true;
      } catch (e) { /* 隐私模式禁用 storage 时忽略 */ }
      if (bannerClose) {
        bannerClose.addEventListener('click', function () {
          langBanner.hidden = true;
          try { window.sessionStorage.setItem('nex_lang_banner_dismissed', '1'); } catch (err) { /* ignore */ }
        });
      }
    }

    // 进场动画：进入视口时加 revealed。reduced-motion 时 CSS 已禁用过渡，这里直接标记。
    var els = document.querySelectorAll('[data-reveal]');
    if (reduced || !('IntersectionObserver' in window)) {
      els.forEach(function (el) { el.classList.add('revealed'); });
    } else {
      var io = new IntersectionObserver(function (entries) {
        entries.forEach(function (entry) {
          if (entry.isIntersecting) {
            entry.target.classList.add('revealed');
            io.unobserve(entry.target);
          }
        });
      }, { rootMargin: '0px 0px -8% 0px', threshold: 0.05 });
      revealObserver = io;
      els.forEach(function (el, i) {
        // 同屏元素轻微错峰，形成一次整体入场而不是齐步走。
        el.style.setProperty('--reveal-delay', (i % 4) * 60 + 'ms');
        io.observe(el);
      });
    }

    // 顶栏滚动后加下边框。
    var topbar = document.querySelector('.topbar');
    if (topbar) {
      var onScroll = function () {
        topbar.classList.toggle('scrolled', window.scrollY > 8);
      };
      onScroll();
      onWin(window, 'scroll', onScroll, { passive: true });
    }

    // 导航 scrollspy：当前区块对应的锚点高亮。
    var navLinks = Array.prototype.slice.call(document.querySelectorAll('.topnav a[href*="#"]'));
    var spyMap = {};
    navLinks.forEach(function (a) {
      var id = (a.getAttribute('href').split('#')[1] || '');
      if (id && document.getElementById(id)) spyMap[id] = a;
    });
    var spyIds = Object.keys(spyMap);
    if (spyIds.length) {
      var spy = function () {
        var cur = '';
        spyIds.forEach(function (id) {
          if (document.getElementById(id).getBoundingClientRect().top <= 140) cur = id;
        });
        spyIds.forEach(function (id) { spyMap[id].classList.toggle('active', id === cur); });
      };
      spy();
      onWin(window, 'scroll', spy, { passive: true });
    }

    // 移动端菜单。按钮只在窄屏出现（CSS 控制），面板用 hidden 属性表达开合。
    var toggle = document.querySelector('.nav-toggle');
    var mnav = document.getElementById('mobile-nav');
    if (toggle && mnav) {
      var setMenu = function (open) {
        mnav.hidden = !open;
        mnav.classList.toggle('open', open);
        toggle.classList.toggle('is-open', open);
        toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
        toggle.setAttribute('aria-label', open ? msg('menuClose', 'Close menu') : msg('menuOpen', 'Open menu'));
        document.body.classList.toggle('nav-open', open);
      };
      toggle.addEventListener('click', function () { setMenu(mnav.hidden); });
      mnav.addEventListener('click', function (e) {
        if (e.target.closest('a')) setMenu(false);
      });
      onWin(document, 'keydown', function (e) {
        if (e.key === 'Escape' && !mnav.hidden) {
          setMenu(false);
          toggle.focus();
        }
      });
      onWin(window, 'resize', function () {
        if (window.innerWidth > 900 && !mnav.hidden) setMenu(false);
      }, { passive: true });
    }

    // 文档导航 / 章节目录：原生 details 保证无脚本时仍可用，窄屏默认折叠。
    document.querySelectorAll('[data-docs-disclosure]').forEach(function (box) {
      var media = window.matchMedia('(max-width: ' + box.getAttribute('data-docs-disclosure') + 'px)');
      var summary = box.querySelector('summary');
      var syncDisclosure = function () {
        if (media.matches && box.contains(document.activeElement)) summary.focus();
        box.open = !media.matches;
      };
      syncDisclosure();
      onWin(media, 'change', syncDisclosure);
      box.addEventListener('keydown', function (e) {
        if (e.key === 'Escape' && box.open) {
          box.open = false;
          summary.focus();
        }
      });
    });

    // 本地全文搜索：索引来自当前语言的公开文档正文，不过滤或隐藏原有目录。
    var docsInput = document.querySelector('[data-docs-search]');
    var docsIndex = document.getElementById('docs-search-index');
    var docsResults = document.getElementById('docs-search-results');
    if (docsInput && docsIndex && docsResults) {
      var docsStatus = docsResults.querySelector('[data-docs-search-status]');
      var docsList = docsResults.querySelector('[data-docs-search-list]');
      var entries = null;
      try {
        entries = JSON.parse(docsIndex.textContent);
        if (!Array.isArray(entries)) entries = null;
      } catch (err) { /* 输入后显示不可用提示，仍保留目录 */ }

      // 只创建文本节点和 mark，不把用户输入或正文片段作为 HTML 拼接。
      var appendMatch = function (target, text, terms) {
        var lower = text.toLowerCase();
        var pos = 0;
        while (pos < text.length) {
          var at = -1, size = 0;
          terms.forEach(function (term) {
            var hit = lower.indexOf(term, pos);
            if (hit >= 0 && (at < 0 || hit < at || (hit === at && term.length > size))) {
              at = hit;
              size = term.length;
            }
          });
          if (at < 0) { target.appendChild(document.createTextNode(text.slice(pos))); break; }
          target.appendChild(document.createTextNode(text.slice(pos, at)));
          var mark = document.createElement('mark');
          mark.textContent = text.slice(at, at + size);
          target.appendChild(mark);
          pos = at + size;
        }
      };
      var docSearch = function () {
        var query = docsInput.value.trim().toLowerCase();
        docsList.replaceChildren();
        docsStatus.textContent = '';
        docsResults.hidden = !query;
        if (!query) return;
        if (!entries) {
          docsStatus.textContent = docsStatus.getAttribute('data-unavailable');
          return;
        }
        var terms = query.split(/\s+/);
        var matches = entries.filter(function (entry) {
          var full = (entry.title + ' ' + entry.summary + ' ' + entry.text).toLowerCase();
          return terms.every(function (term) { return full.indexOf(term) >= 0; });
        });
        matches.forEach(function (entry) {
          var item = document.createElement('li');
          var link = document.createElement('a');
          link.href = entry.url;
          var title = document.createElement('span');
          title.className = 'docs-result-title';
          title.lang = entry.language;
          appendMatch(title, entry.title, terms);
          link.appendChild(title);
          var lower = entry.text.toLowerCase();
          var first = -1;
          terms.forEach(function (term) {
            var hit = lower.indexOf(term);
            if (hit >= 0 && (first < 0 || hit < first)) first = hit;
          });
          var start = Math.max(0, first - 55);
          var snippet = (start ? '…' : '') + entry.text.slice(start, start + 200);
          if (start + 200 < entry.text.length) snippet += '…';
          var excerpt = document.createElement('span');
          excerpt.className = 'docs-result-excerpt';
          excerpt.lang = entry.language;
          appendMatch(excerpt, snippet, terms);
          link.appendChild(excerpt);
          if (entry.isFallback) {
            var note = document.createElement('small');
            note.textContent = docsList.getAttribute('data-fallback');
            link.appendChild(note);
          }
          item.appendChild(link);
          docsList.appendChild(item);
        });
        var count = docsStatus.getAttribute(matches.length === 1 ? 'data-count-one' : 'data-count-other');
        docsStatus.textContent = matches.length ? count.replace('{count}', String(matches.length)) : docsStatus.getAttribute('data-empty');
      };
      docsInput.addEventListener('input', docSearch);
      docsInput.addEventListener('keydown', function (e) {
        if (e.isComposing) return;
        if (e.key === 'Escape') {
          docsInput.value = '';
          docSearch();
        } else if (e.key === 'Enter' || e.key === 'ArrowDown') {
          var first = docsList.querySelector('a');
          if (first) { e.preventDefault(); first.focus(); }
        }
      });
      docSearch();
      onWin(document, 'keydown', function (e) {
        var slash = e.key === '/';
        var cmdK = (e.key === 'k' || e.key === 'K') && (e.metaKey || e.ctrlKey);
        if (!slash && !cmdK) return;
        var t = e.target;
        if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return;
        e.preventDefault();
        docsInput.focus();
        docsInput.select();
      });
    }

    // 文档代码块：右上角复制图标。按钮放在 pre 外的包裹层里，横向滚动时长命令不会把按钮带走。
    document.querySelectorAll('.docs-body pre > code').forEach(function (code, idx) {
      var pre = code.parentElement;
      if (!pre || pre.querySelector('.docs-code-copy')) return;
      var id = 'docs-code-' + idx;
      code.id = id;
      var wrap = document.createElement('div');
      wrap.className = 'docs-code-wrap';
      pre.parentNode.insertBefore(wrap, pre);
      wrap.appendChild(pre);
      var btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'docs-code-copy';
      btn.setAttribute('data-copy', '#' + id);
      btn.setAttribute('aria-label', msg('codeCopy', 'Copy code'));
      btn.title = msg('codeCopy', 'Copy code');
      btn.innerHTML = '<svg class="ico-copy" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15V6a2 2 0 0 1 2-2h9"/></svg>' +
        '<svg class="ico-check" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M5 13l4 4L19 7"/></svg>';
      var label = document.createElement('span');
      label.className = 'sr-only';
      label.setAttribute('data-copy-label', '');
      label.textContent = msg('codeCopy', 'Copy code');
      btn.appendChild(label);
      wrap.appendChild(btn);
    });

    // 文档本页目录：随滚动高亮当前小节；左侧文档树仍保持页面级 active。
    var tocTargets = Array.prototype.slice.call(document.querySelectorAll('.docs-toc a[href^="#"]'))
      .map(function (a) {
        var raw = a.getAttribute('href').slice(1);
        var id = raw;
        // 中文标题的 href 会被模板做百分号编码，getElementById 需要解码后的 id。
        try { id = decodeURIComponent(raw); } catch (e) { /* 保留原值 */ }
        return { link: a, el: document.getElementById(id) };
      })
      .filter(function (item) { return !!item.el; });
    if (tocTargets.length) {
      var syncToc = function () {
        var current = null;
        tocTargets.forEach(function (item) {
          if (item.el.getBoundingClientRect().top <= 132) current = item.link;
        });
        if (!current && window.scrollY < 120) current = tocTargets[0].link;
        tocTargets.forEach(function (item) {
          var on = item.link === current;
          item.link.classList.toggle('active', on);
          if (on) item.link.setAttribute('aria-current', 'true');
          else item.link.removeAttribute('aria-current');
        });
      };
      syncToc();
      onWin(window, 'scroll', syncToc, { passive: true });
    }

    // 文档表格：首列相邻同值合并成 rowspan。权限表按模块分组，逐行重复的 log/notice
    // 会把首列撑成一条条空块；合并后分组关系更清楚，也不影响单元格内容与搜索。
    document.querySelectorAll('.docs-body table').forEach(function (table) {
      if (table.getAttribute('data-merged-rows')) return;
      table.setAttribute('data-merged-rows', '1');
      var rows = Array.prototype.slice.call(table.tBodies[0] ? table.tBodies[0].rows : []);
      var i = 0;
      while (i < rows.length) {
        var cell = rows[i].cells[0];
        if (!cell || cell.children.length) { i++; continue; }
        var label = cell.textContent.trim();
        if (!label) { i++; continue; }
        var span = 1;
        while (i + span < rows.length) {
          var next = rows[i + span].cells[0];
          if (!next || next.children.length || next.textContent.trim() !== label) break;
          span++;
        }
        if (span > 1) {
          cell.rowSpan = span;
          for (var j = 1; j < span; j++) rows[i + j].deleteCell(0);
        }
        i += span;
      }
    });

    // 选项卡：同步 aria-selected 与 hidden，方向键 / Home / End 切换。
    document.querySelectorAll('[data-tabs]').forEach(function (list) {
      var tabs = Array.prototype.slice.call(list.querySelectorAll('[role="tab"]'));
      if (!tabs.length) return;
      var panels = tabs.map(function (t) { return document.getElementById(t.getAttribute('aria-controls')); });
      var select = function (idx, focus) {
        tabs.forEach(function (tab, i) {
          var on = i === idx;
          tab.setAttribute('aria-selected', on ? 'true' : 'false');
          tab.tabIndex = on ? 0 : -1;
          if (panels[i]) panels[i].hidden = !on;
        });
        if (focus) tabs[idx].focus();
      };
      tabs.forEach(function (tab, i) {
        tab.addEventListener('click', function () { select(i); });
        tab.addEventListener('keydown', function (e) {
          var next = null;
          if (e.key === 'ArrowRight') next = (i + 1) % tabs.length;
          else if (e.key === 'ArrowLeft') next = (i - 1 + tabs.length) % tabs.length;
          else if (e.key === 'Home') next = 0;
          else if (e.key === 'End') next = tabs.length - 1;
          if (next !== null) { e.preventDefault(); select(next, true); }
        });
      });
      select(0);
    });

    // 插件包下载：走 fetch 拿 blob，页面内提示“正在打包”，失败不把用户带到 502 页。
    var downloadBtn = document.querySelector('[data-download]');
    if (downloadBtn && window.fetch && window.URL && window.Blob) {
      downloadBtn.addEventListener('click', function (e) {
        e.preventDefault();
        if (downloadBtn.dataset.busy === '1') return;
        var hint = document.querySelector('[data-download-hint]');
        var original = downloadBtn.textContent;
        downloadBtn.dataset.busy = '1';
        downloadBtn.setAttribute('aria-disabled', 'true');
        downloadBtn.textContent = msg('packaging', 'Packaging…');
        if (hint) {
          hint.hidden = false;
          hint.textContent = msg('packagingHint', 'The first download is packaged on demand; this usually takes under a minute.');
        }
        fetch(downloadBtn.getAttribute('href')).then(function (res) {
          if (!res.ok) throw new Error('HTTP ' + res.status);
          var m = /filename="?([^";]+)"?/.exec(res.headers.get('Content-Disposition') || '');
          return res.blob().then(function (blob) {
            return { blob: blob, name: m ? m[1] : 'plugin.zip' };
          });
        }).then(function (x) {
          var url = URL.createObjectURL(x.blob);
          var a = document.createElement('a');
          a.href = url;
          a.download = x.name;
          document.body.appendChild(a);
          a.click();
          a.remove();
          window.setTimeout(function () { URL.revokeObjectURL(url); }, 5000);
          downloadBtn.textContent = original;
          downloadBtn.removeAttribute('aria-disabled');
          delete downloadBtn.dataset.busy;
          if (hint) hint.hidden = true;
        }).catch(function () {
          downloadBtn.textContent = original;
          downloadBtn.removeAttribute('aria-disabled');
          delete downloadBtn.dataset.busy;
          if (hint) {
            hint.hidden = false;
            hint.textContent = msg('packFailed', 'Packaging failed. Try again shortly.');
          }
        });
      });
    }

    if (reduced) return;

    // 滚动视差：按元素在视口中的位置写入 --py（默认 ±12px），只动 transform；rAF 节流、视口外跳过。
    // data-parallax 的值可以给一个像素幅度（如 "34"）：越靠前的装饰件幅度越大，才有前后层次。
    var pEls = Array.prototype.slice.call(document.querySelectorAll('[data-parallax]'));
    var pAmp = function (el) {
      var v = parseFloat(el.getAttribute('data-parallax'));
      return isFinite(v) && v > 0 ? v : 12;
    };
    if (pEls.length) {
      var pTicking = false;
      var pUpdate = function () {
        pTicking = false;
        var vh = window.innerHeight;
        pEls.forEach(function (el) {
          var r = el.getBoundingClientRect();
          if (r.bottom < -120 || r.top > vh + 120) return;
          var p = (r.top + r.height / 2 - vh / 2) / (vh / 2 + r.height / 2);
          el.style.setProperty('--py', (p * pAmp(el)).toFixed(1) + 'px');
        });
      };
      var pOnScroll = function () {
        if (!pTicking) {
          pTicking = true;
          requestAnimationFrame(pUpdate);
        }
      };
      onWin(window, 'scroll', pOnScroll, { passive: true });
      onWin(window, 'resize', pOnScroll, { passive: true });
      pUpdate();
    }


    // 产品示意窗口随鼠标轻微 3D 倾斜。
    var visual = document.querySelector('.hero-visual');
    var browser = document.querySelector('.hero-shot');
    if (visual && browser && fine) {
      var tiltPending = false, tx = 0, ty = 0;
      var applyTilt = function () {
        tiltPending = false;
        browser.style.setProperty('--ry', (tx * 1.8).toFixed(2) + 'deg');
        browser.style.setProperty('--rx', (-ty * 1.5).toFixed(2) + 'deg');
      };
      visual.addEventListener('mousemove', function (e) {
        var r = visual.getBoundingClientRect();
        tx = (e.clientX - r.left) / r.width - 0.5;
        ty = (e.clientY - r.top) / r.height - 0.5;
        if (!tiltPending) {
          tiltPending = true;
          requestAnimationFrame(applyTilt);
        }
      }, { passive: true });
      visual.addEventListener('mouseleave', function () {
        browser.style.setProperty('--rx', '0deg');
        browser.style.setProperty('--ry', '0deg');
      });
    }

    // 滚动漂移：data-drift 元素按视口进度写入 --dy（±16px），只动 transform。
    var dEls = Array.prototype.slice.call(document.querySelectorAll('[data-drift]'));
    if (dEls.length) {
      var dTicking = false;
      var dUpdate = function () {
        dTicking = false;
        var vh = window.innerHeight;
        dEls.forEach(function (el) {
          var r = el.getBoundingClientRect();
          if (r.bottom < -160 || r.top > vh + 160) return;
          var p = (r.top + r.height / 2 - vh / 2) / (vh / 2 + r.height / 2);
          el.style.setProperty('--dy', (p * 16).toFixed(1) + 'px');
        });
      };
      var dOnScroll = function () {
        if (!dTicking) { dTicking = true; requestAnimationFrame(dUpdate); }
      };
      onWin(window, 'scroll', dOnScroll, { passive: true });
      onWin(window, 'resize', dOnScroll, { passive: true });
      dUpdate();
    }

    // 磁吸主按钮：桌面细指针时，按钮朝光标方向轻微位移（±5px），离开回弹。
    if (fine) {
      document.querySelectorAll('.btn-primary').forEach(function (btn) {
        var raf = false, mx = 0, my = 0;
        var apply = function () {
          raf = false;
          btn.style.setProperty('--mag-x', mx.toFixed(1) + 'px');
          btn.style.setProperty('--mag-y', my.toFixed(1) + 'px');
        };
        btn.addEventListener('mousemove', function (e) {
          var r = btn.getBoundingClientRect();
          mx = Math.max(-5, Math.min(5, (e.clientX - r.left - r.width / 2) * 0.18));
          my = Math.max(-5, Math.min(5, (e.clientY - r.top - r.height / 2) * 0.3));
          if (!raf) { raf = true; requestAnimationFrame(apply); }
        }, { passive: true });
        btn.addEventListener('mouseleave', function () {
          mx = 0; my = 0;
          if (!raf) { raf = true; requestAnimationFrame(apply); }
        });
      });
    }

    // 统计数字 count-up：进场时从 0 计到目标值，千分位/小数/单位保持原样。
    var countUp = function (el) {
      var raw = el.textContent;
      var m = raw.match(/^([\d.,]+)\s*(.*)$/);
      if (!m) return;
      var target = parseFloat(m[1].replace(/,/g, ''));
      if (!isFinite(target)) return;
      var suffix = m[2];
      var grouped = m[1].indexOf(',') >= 0;
      var decimals = (m[1].split('.')[1] || '').length;
      var t0 = null;
      var fmt = function (v) {
        var s = decimals ? v.toFixed(decimals) : String(Math.round(v));
        if (grouped) {
          s = Number(s).toLocaleString('en-US', {
            minimumFractionDigits: decimals,
            maximumFractionDigits: decimals
          });
        }
        return s + suffix;
      };
      var step = function (ts) {
        if (t0 === null) t0 = ts;
        var p = Math.min((ts - t0) / 900, 1);
        var eased = 1 - Math.pow(1 - p, 3);
        el.textContent = fmt(target * eased);
        if (p < 1) requestAnimationFrame(step);
      };
      requestAnimationFrame(step);
    };
    var stats = document.querySelectorAll('.stat-value');
    if (stats.length && 'IntersectionObserver' in window) {
      var statIO = new IntersectionObserver(function (entries) {
        entries.forEach(function (entry) {
          if (entry.isIntersecting) {
            statIO.unobserve(entry.target);
            countUp(entry.target);
          }
        });
      }, { threshold: 0.4 });
      stats.forEach(function (el) { statIO.observe(el); });
    }
  }

  // ---- 在线留言：fetch 提交并就地反馈；脚本不可用时表单整页 POST，服务端 303 回联系页带结果 ----
  function initContactForm() {
    var form = document.querySelector('[data-contact-form]');
    if (!form || !window.fetch || !window.FormData) return;
    var status = form.querySelector('[data-contact-status]');
    var submit = form.querySelector('button[type="submit"]');
    var show = function (text, ok) {
      if (!status) return;
      status.hidden = false;
      status.className = 'contact-status ' + (ok ? 'contact-status-ok' : 'contact-status-err');
      status.textContent = text;
    };
    var errText = function (code) {
      if (code === 'name') return msg('contactErrName', '请填写称呼');
      if (code === 'email') return msg('contactErrEmail', '请填写有效的邮箱地址');
      if (code === 'content') return msg('contactErrContent', '留言内容至少 5 个字');
      if (code === 'consent') return msg('contactErrConsent', '请先阅读并同意隐私政策');
      if (code === 'rate') return msg('contactRate', '发送太频繁，请稍后再试');
      return msg('contactFailed', '发送失败，请稍后重试');
    };
    form.addEventListener('submit', function (e) {
      e.preventDefault();
      var fd = new FormData(form);
      var name = String(fd.get('name') || '').trim();
      var email = String(fd.get('email') || '').trim();
      var content = String(fd.get('content') || '').trim();
      if (!name) { show(errText('name'), false); return; }
      if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) { show(errText('email'), false); return; }
      if (content.length < 5) { show(errText('content'), false); return; }
      var consent = form.querySelector('[name="consent"]');
      if (consent && !consent.checked) { show(errText('consent'), false); return; }
      if (submit) submit.disabled = true;
      show(msg('contactSending', '正在发送…'), true);
      fetch(form.action, {
        method: 'POST',
        headers: { Accept: 'application/json' },
        body: new URLSearchParams(fd),
        credentials: 'same-origin'
      })
        .then(function (res) {
          if (res.status === 429) throw { code: 'rate' };
          return res.json().catch(function () { return { ok: false, error: 'fail' }; }).then(function (body) {
            if (!res.ok || !body || !body.ok) throw { code: body && body.error };
            return body;
          });
        })
        .then(function () {
          form.reset();
          show(msg('contactSent', '已收到，我们会尽快回复'), true);
        })
        .catch(function (err) {
          show(errText(err && err.code), false);
        })
        .then(function () { if (submit) submit.disabled = false; });
    });
  }

  // ---- 插件评分：fetch 提交并就地更新星星/文案/顶部汇总；脚本不可用时表单整页 POST，服务端 303 回评分区 ----
  function initRatingForm() {
    var form = document.querySelector('[data-rating-form]');
    if (!form || !window.fetch || !window.FormData) return;
    var note = document.querySelector('[data-rating-note]');
    var topRating = document.querySelector('[data-plugin-rating]');
    var stars = form.querySelectorAll('.rating-star');
    var buttons = form.querySelectorAll('button[name="score"]');
    var clear = form.querySelector('[data-rating-clear]');

    function scoreButton(el) {
      return el && el.closest ? el.closest('button[name="score"]') : null;
    }
    // submitter 在老浏览器里没有；记一下最近点过的按钮，键盘/鼠标都能兜住。
    var clicked = null;
    form.addEventListener('click', function (e) { clicked = scoreButton(e.target); }, true);

    form.addEventListener('submit', function (e) {
      // submit 事件的 target 永远是 form，被点的按钮在 submitter（或焦点/最近点击）上。
      var btn = scoreButton(e.submitter) || clicked || scoreButton(document.activeElement);
      clicked = null;
      // 有脚本时一律就地提交，绝不让浏览器整页 POST 后再 303 跳 #rating（那会先回顶再滚下来）。
      e.preventDefault();
      if (!btn || !form.contains(btn) || btn.disabled) return;
      buttons.forEach(function (b) { b.disabled = true; });
      var score = parseInt(btn.value, 10);
      var fd = new FormData(form);
      fd.set('score', btn.value);
      fetch(form.action, {
        method: 'POST',
        headers: { Accept: 'application/json' },
        body: new URLSearchParams(fd),
        credentials: 'same-origin'
      })
        .then(function (res) { return res.json(); })
        .then(function (body) {
          if (!body || !body.ok) throw new Error('rate failed');
          var current = parseInt(body.score, 10);
          if (isNaN(current)) current = 0; // 0 = 已取消
          if (note) note.textContent = body.note || '';
          stars.forEach(function (s) {
            var on = current > 0 && parseInt(s.value, 10) <= current;
            s.classList.toggle('on', on);
            s.setAttribute('aria-pressed', on ? 'true' : 'false');
          });
          if (clear) clear.hidden = current === 0;
          if (topRating) {
            var count = parseInt(body.count, 10) || 0;
            if (count > 0) {
              topRating.classList.remove('muted');
              topRating.textContent = '★ ' + (body.avg || '') + ' ';
              var small = document.createElement('small');
              small.textContent = '(' + count + ')';
              topRating.appendChild(small);
            } else {
              topRating.classList.add('muted');
              topRating.textContent = topRating.getAttribute('data-rating-none') || '';
            }
          }
        })
        .catch(function () {
          // 失败也留在当前位置就地提示，别整页跳转回 #rating（同样会先回顶再滚下来）。
          if (note) note.textContent = note.getAttribute('data-rating-failed') || '评分保存失败';
        })
        .then(function () {
          buttons.forEach(function (b) { b.disabled = false; });
        });
    });
  }

  // ---- 商详「数量」步进器：下限是起订量，上限只挡溢出；输入框允许直接敲数 ----
  // 数量控件是 -/数字/+ 形态，但把最小值钉在 MOQ 上，避免出现
  // 「填了 1 件去询价、起订量却是 5 件」这种自相矛盾的询盘。
  function initProductQty() {
    var box = document.querySelector('[data-pd-qty]');
    if (!box) return;
    var input = box.querySelector('[data-pd-qty-input]');
    var minus = box.querySelector('[data-pd-qty-minus]');
    var plus = box.querySelector('[data-pd-qty-plus]');
    if (!input || !minus || !plus) return;
    var min = Math.max(1, parseInt(box.getAttribute('data-pd-qty-min'), 10) || 1);
    var MAX = 1000000;

    function read() {
      var v = parseInt(String(input.value).replace(/[^\d]/g, ''), 10);
      return isNaN(v) ? 0 : v;
    }
    function sync() {
      minus.disabled = read() <= min;
      plus.disabled = read() >= MAX;
    }
    function set(v) {
      if (isNaN(v) || v < min) v = min;
      if (v > MAX) v = MAX;
      input.value = String(v);
      sync();
    }
    minus.addEventListener('click', function () { set(read() - 1); });
    plus.addEventListener('click', function () { set(read() + 1); });
    input.addEventListener('input', function () {
      // 打字过程中只滤掉非数字，不即时钳制：否则输「12」第一下就被改成起订量
      var clean = String(input.value).replace(/[^\d]/g, '');
      if (clean !== input.value) input.value = clean;
      sync();
    });
    input.addEventListener('blur', function () { set(read()); });
    input.addEventListener('keydown', function (e) {
      if (e.key !== 'ArrowUp' && e.key !== 'ArrowDown') return;
      e.preventDefault();
      set(read() + (e.key === 'ArrowUp' ? 1 : -1));
    });
    sync();
  }

  // ---- 商详小计卡：数量 × 单价 → 商品小计 / 运费 / 合计 ----
  function initProductSubtotal() {
    var sum = document.querySelector('[data-pd-sum]');
    var legacy = document.querySelector('[data-pd-subtotal]');
    var cur = document.querySelector('[data-pd-qty-input]');
    if (!sum && !legacy) return;
    var currency = sum ? sum.getAttribute('data-pd-currency') || '' : '';
    // 模板把起价写在 data-pd-price 上兜底
    var basePrice = sum ? parseFloat(sum.getAttribute('data-pd-price')) || 0 : 0;
    // 运费按商品配置计入合计：0 = 未报价，模板里仍显示「待定」，合计先按商品小计走。
    var baseFreight = sum ? parseFloat(sum.getAttribute('data-pd-freight')) || 0 : 0;
    var freightCell = sum ? sum.querySelector('[data-pd-sum-freight]') : null;

    function money(v) {
      // 口径必须和模板里的 money 函数一致（整数不带小数、不加千分位），
      // 否则同一屏上会出现 USD1200 与 USD1,200 两种写法。
      var n = Math.round(v * 100) / 100;
      return currency + (n === Math.trunc(n) ? String(n) : n.toFixed(2));
    }
    function apply() {
      var qty = cur ? parseInt(String(cur.value).replace(/[^\d]/g, ''), 10) || 0 : 0;
      var amount = qty > 0 ? qty * basePrice : 0;
      // 结算小结：商品小计 / 运费总计 / 小计。运费 0 = 未报价（模板里是「待定」），
      // 合计先按商品小计走；有报价时才计入，和后台商品字段同一口径。
      if (sum) {
        sum.hidden = !amount;
        if (amount) {
          var cell = sum.querySelector('[data-pd-sum-products]');
          var total = sum.querySelector('[data-pd-sum-total]');
          if (cell) cell.textContent = money(amount);
          if (freightCell && baseFreight > 0) freightCell.textContent = money(baseFreight);
          if (total) total.textContent = money(amount + baseFreight);
        }
      }
      // 老模板沿用的一行式「小计：N × 单价 = 金额」，有就照旧填，没有就不管
      if (legacy) {
        legacy.hidden = !amount;
        if (amount) legacy.textContent = msg('subtotal', '小计') + '：' + qty + ' × ' + money(basePrice) + ' = ' + money(amount);
      }
    }

    // 数量控件（输入框 / 加减号）改完再算：数值更新发生在它自己的处理器里
    function later() { window.setTimeout(apply, 0); }
    if (cur) {
      cur.addEventListener('input', apply);
      cur.addEventListener('change', apply);
    }
    var minus = document.querySelector('[data-pd-qty-minus]');
    var plus = document.querySelector('[data-pd-qty-plus]');
    if (minus) minus.addEventListener('click', later);
    if (plus) plus.addEventListener('click', later);
    apply();
  }

  // ---- 移动端底部询盘条：滚过右栏 CTA 才出现，避免和页面里的按钮重复打架 ----
  function initStickyInquiry() {
    var bar = document.querySelector('[data-pd-sticky]');
    var cta = document.getElementById('pd-cta');
    if (!bar || !cta) return;
    var go = bar.querySelector('[data-pd-sticky-go]');
    if (go) go.addEventListener('click', function () { cta.click(); });
    if (!window.IntersectionObserver) { bar.classList.add('is-on'); return; }
    // 只有把 CTA 滚到视口上方之后才浮出来：页面顶部 CTA 本来就在下方，
    // 那时显示会平白挡住内容；滚回去了也自动收起。点它等同点页面里的「发送询盘」
    new IntersectionObserver(function (entries) {
      var e = entries[0];
      bar.classList.toggle('is-on', !e.isIntersecting && e.boundingClientRect.top < 0);
    }, { threshold: 0 }).observe(cta);
  }

  // ---- 右栏 sticky 的高度守卫 ----
  // 信息多的商品（属性 + 详情 + 运输 + 认证…）右栏会比视口还高：
  // 这时还 sticky 的话底部按钮永远在屏幕外，所以超过视口就让它回到文档流跟着页面走。
  function initInquirySticky() {
    var box = document.querySelector('.pd-inquiry');
    if (!box) return;
    function check() {
      // 80px 是顶栏 + 内边距的余量；窄屏右栏本来就不 sticky，不用管
      var tall = box.scrollHeight > window.innerHeight - 96;
      box.classList.toggle('is-tall', tall);
    }
    check();
    window.addEventListener('resize', check);
    // 阶梯价 / 样品等由数据决定，字体加载完高度会变，再量一次
    window.setTimeout(check, 400);
  }

  // ---- 发送询盘弹窗：结构与文案由服务端出（render.go 的 inquiryDialog），这里只开合 + 提交 ----
  // 提交走站点自带的 POST /contact：Accept: application/json 时后端回 {ok:true}/{ok:false,error}，
  // 和联系页是同一个入库口（含限流、蜜罐、隐私同意校验），不另开一套。
  var INQ_CSS = [
    '.inq{position:fixed;inset:0;z-index:9998;display:flex;align-items:center;justify-content:center;padding:20px}',
    '.inq[hidden]{display:none}',
    '.inq-mask{position:absolute;inset:0;background:rgba(0,0,0,.45)}',
    '.inq-card{position:relative;display:flex;flex-direction:column;width:min(560px,100%);max-height:min(90dvh,820px);overflow:hidden;box-sizing:border-box;padding:22px 24px 20px;border-radius:6px;background:var(--panel-2,Canvas);color:var(--text,CanvasText);box-shadow:0 24px 70px rgba(0,0,0,.3)}',
        // 表单主体滚动、发送按钮常驻底部：加了附件与共享名片后内容接近一屏，按钮不能跟着滚走。
        '.inq-form{display:flex;flex-direction:column;min-height:0;flex:1 1 auto}',
        '.inq-body{overflow:auto;min-height:0;flex:1 1 auto;padding-inline-end:2px;margin-inline-end:-2px}',
    '.inq-title{margin:0 0 14px;font-size:18px;font-weight:700;line-height:1.3}',
    '.inq-x{position:absolute;inset-block-start:12px;inset-inline-end:12px;display:inline-flex;align-items:center;justify-content:center;width:44px;height:44px;padding:0;border:0;border-radius:4px;background:transparent;color:inherit;opacity:.55;cursor:pointer}',
    '.inq-x:hover{opacity:1;background:rgba(128,128,128,.12)}',
    '.inq-x svg{width:16px;height:16px}',
    '.inq-prod{display:flex;align-items:flex-start;gap:10px;margin:0 0 14px;padding:8px;border:1px solid rgba(128,128,128,.22);border-radius:4px}',
    '.inq-prod-cover{width:44px;height:44px;flex:none;object-fit:cover;border-radius:3px;background:rgba(128,128,128,.1)}',
    '.inq-prod-text{display:flex;flex-direction:column;gap:2px;min-width:0;flex:1;font-size:12.5px}',
    '.inq-prod-text strong{font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}',
    '.inq-prod-price{opacity:.72}',
    '.inq-to{display:flex;align-items:baseline;gap:10px;margin:0 0 10px;padding:0 2px;font-size:12.5px}',
    '.inq-to-label{flex:none;opacity:.65}',
    '.inq-to-body{display:flex;flex-wrap:wrap;align-items:baseline;gap:4px 8px;min-width:0}',
    '.inq-to-body strong{font-weight:600}',
    '.inq-to-mail{font-size:11.5px;opacity:.72;overflow-wrap:anywhere}',
    '.inq-qty{display:flex;flex-wrap:wrap;align-items:center;gap:8px;margin-top:6px;font-size:12px}',
    '.inq-qty-label{opacity:.65}',
    '.inq-stepper{display:inline-flex;align-items:center;gap:2px;padding:2px;border:1px solid rgba(128,128,128,.32);border-radius:999px;background:var(--input-bg,Canvas)}',
    '.inq-stepper:focus-within{border-color:var(--pc-accent,currentColor)}',
    '.inq-step{display:flex;align-items:center;justify-content:center;width:40px;height:40px;padding:0;border:0;border-radius:50%;background:transparent;color:inherit;font:inherit;font-size:14px;line-height:1;cursor:pointer}',
    '.inq-step:hover:not(:disabled){background:rgba(128,128,128,.16)}',
    '.inq-step:disabled{opacity:.3;cursor:not-allowed}',
    '.inq-step-input{box-sizing:border-box;width:52px;height:40px;padding:0;border:0;background:transparent;color:inherit;font:inherit;font-size:16px;text-align:center;appearance:textfield}',
    '.inq-step-input:focus{outline:0}',
    '.inq-qty-unit{font-size:11.5px;opacity:.7}',
    '.inq-field{display:block;margin:0 0 10px;font-size:12.5px}',
    '.inq-label{display:block;margin-bottom:5px;font-weight:600}',
    '.inq-req{font-style:normal;margin-inline-end:2px;color:#d5232b}',
    '.inq-field textarea,.inq-field input{display:block;width:100%;box-sizing:border-box;padding:9px 10px;border:1px solid rgba(128,128,128,.4);border-radius:4px;background:transparent;color:inherit;font:inherit;font-size:13px}',
    '.inq-field input{min-height:44px}',
    '.inq-field textarea{min-height:104px;resize:vertical;line-height:1.6}',
    '.inq-field textarea:focus,.inq-field input:focus{outline:0;border-color:var(--pc-accent,currentColor)}',
    '.inq-hint{margin:-4px 0 12px;font-size:11.5px;line-height:1.6;opacity:.6}',
    '.inq-attach{margin:0 0 14px}',
    '.inq-attach-btn{display:inline-flex;align-items:center;gap:6px;padding:6px 10px;border:1px dashed rgba(128,128,128,.45);border-radius:4px;background:transparent;color:inherit;font:inherit;font-size:12px;cursor:pointer}',
    '.inq-attach-btn:hover{border-color:currentColor}',
    '.inq-attach-btn svg{width:14px;height:14px;opacity:.7}',
    '.inq-attach-hint{margin:6px 0 0;font-size:11px;line-height:1.6;opacity:.6}',
    '.inq-attach-list{display:flex;flex-direction:column;gap:6px;margin:8px 0 0;padding:0;list-style:none}',
    '.inq-attach-list[hidden]{display:none}',
    '.inq-att-item{display:flex;align-items:center;gap:8px;padding:6px 8px;border:1px solid rgba(128,128,128,.22);border-radius:4px;font-size:11.5px;line-height:1.4}',
    '.inq-att-item.is-uploading{opacity:.6}',
    '.inq-att-name{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}',
    '.inq-att-size{flex:none;opacity:.6}',
    '.inq-att-x{flex:none;display:inline-flex;align-items:center;justify-content:center;width:18px;height:18px;padding:0;border:0;border-radius:3px;background:transparent;color:inherit;font:inherit;font-size:14px;line-height:1;opacity:.55;cursor:pointer}',
    '.inq-att-x:hover{opacity:1;background:rgba(128,128,128,.16)}',
    '.inq-share{display:flex;align-items:flex-start;gap:8px;margin:0 0 10px;font-size:11.5px;line-height:1.6}',
    '.inq-share input{margin:2px 0 0;flex:none}',
    '.inq-suggest{padding:12px;margin:0 0 14px;border-radius:4px;background:rgba(128,128,128,.07)}',
    '.inq-suggest-title{margin:0 0 10px;font-size:11.5px;opacity:.75}',
    '.inq-chips{display:flex;flex-wrap:wrap;gap:8px}',
    '.inq-chip{padding:6px 10px;border:1px solid rgba(128,128,128,.3);border-radius:3px;background:var(--input-bg,Canvas);color:inherit;font:inherit;font-size:11.5px;line-height:1.4;cursor:pointer;text-align:start}',
    '.inq-chip:hover{border-color:currentColor}',
    '.inq-row{display:flex;gap:10px}',
    '.inq-row>.inq-field{flex:1;min-width:0}',
    '.inq-hp{position:absolute;inset-inline-start:-9999px;width:1px;height:1px;overflow:hidden}',
    '.inq-consent{display:flex;align-items:flex-start;gap:8px;margin:6px 0 10px;font-size:11.5px;line-height:1.6}',
    '.inq-consent input{margin:2px 0 0;flex:none}',
    '.inq-err{margin:0 0 8px;font-size:12px;color:#c0392b}',
    '.inq-note{margin:0 0 12px;font-size:11px;line-height:1.65;opacity:.62}',
    '.inq-send{flex:none;margin-top:12px;width:100%;padding:12px;border:1px solid var(--pc-accent,currentColor);border-radius:4px;background:var(--pc-accent,currentColor);color:#fff;font:inherit;font-size:14px;font-weight:600;cursor:pointer}',
    '.inq-send:disabled{opacity:.6;cursor:default}',
    '.inq-done{padding:26px 0;text-align:center;font-size:14px;line-height:1.7}',
    'body.inq-lock{overflow:hidden}',
    '@media (max-width:560px){.inq{padding:0;align-items:flex-end}.inq-card{width:100%;max-height:92dvh;border-radius:10px 10px 0 0;padding:18px 16px 20px}.inq-row{flex-direction:column;gap:0}.inq-field input{font-size:16px}}',
    '@media (hover:none){.inq-step{width:44px;height:44px}.inq-step-input{width:60px;height:44px}.inq-attach-btn,.inq-chip{min-height:44px}.inq-att-x{width:44px;height:44px}}'
  ].join('');

  function initInquiry() {
    var modal = document.querySelector('[data-inquiry]');
    if (!modal) return;
    // 挂到 body 上：详情页外层有 transform/层叠上下文时，fixed 定位的 z-index 会被那个上下文
    // 限制住，右侧浮动工具条之类的元素就会盖到弹窗上面。
    if (modal.parentNode !== document.body) document.body.appendChild(modal);
    injectCSS('site-inq-style', INQ_CSS);
    var form = modal.querySelector('[data-inquiry-form]');
    var box = modal.querySelector('[data-inquiry-content]');
    var err = modal.querySelector('[data-inquiry-err]');
    var send = form && form.querySelector('.inq-send');
    var qtyBox = modal.querySelector('[data-inq-qty]');
    var qtyInput = qtyBox && qtyBox.querySelector('[data-inq-qty-input]');
    var qtyMin = qtyBox ? Math.max(1, parseInt(qtyBox.getAttribute('data-inq-qty-min'), 10) || 1) : 1;
    var qtyUnit = qtyBox ? (qtyBox.getAttribute('data-inq-qty-unit') || '') : '';
    var lastFocus = null;
    var done = false;

    // 数量下限是起订量：输入框只收整数，减到底就禁用减号
    function setQty(n) {
      if (!qtyInput) return;
      n = Math.floor(n);
      if (!isFinite(n) || n < qtyMin) n = qtyMin;
      if (n > 999999) n = 999999;
      qtyInput.value = String(n);
      var minus = qtyBox.querySelector('[data-inq-qty-minus]');
      if (minus) minus.disabled = n <= qtyMin;
    }
    if (qtyInput) {
      var qtyNow = function () { return parseInt(qtyInput.value, 10) || qtyMin; };
      var minus = qtyBox.querySelector('[data-inq-qty-minus]');
      var plus = qtyBox.querySelector('[data-inq-qty-plus]');
      if (minus) minus.addEventListener('click', function () { setQty(qtyNow() - 1); });
      if (plus) plus.addEventListener('click', function () { setQty(qtyNow() + 1); });
      qtyInput.addEventListener('input', function () { this.value = this.value.replace(/[^\d]/g, ''); });
      qtyInput.addEventListener('blur', function () { setQty(qtyNow()); });
      setQty(qtyNow());
    }

    function say(text) {
      if (!err) return;
      err.textContent = text || '';
      err.hidden = !text;
    }

    // ---- 附件：选中即上传，拿 token；提交时把 token 列表随表单发走 ----
    // 文件先落库（message_id = 0），留言提交时才绑定，所以买到一半关掉页面不会留下半条询盘。
    var attachBox = modal.querySelector('[data-inq-attach]');
    var attachInput = attachBox && attachBox.querySelector('[data-inq-attach-input]');
    var attachList = attachBox && attachBox.querySelector('[data-inq-attach-list]');
    var attachBtn = attachBox && attachBox.querySelector('[data-inq-attach-btn]');
    var attachMax = attachBox ? parseInt(attachBox.getAttribute('data-max-files'), 10) || 0 : 0;
    var attachMaxBytes = attachBox ? parseInt(attachBox.getAttribute('data-max-bytes'), 10) || 0 : 0;
    var attachFiles = [];

    function sizeText(n) {
      if (n >= 1048576) return (Math.round(n / 1048576 * 10) / 10) + 'MB';
      if (n >= 1024) return Math.round(n / 1024) + 'KB';
      return n + 'B';
    }
    function clearAttachments() {
      attachFiles = [];
      if (attachList) {
        attachList.innerHTML = '';
        attachList.hidden = true;
      }
      if (attachInput) attachInput.value = '';
    }
    function dropAttach(item) {
      var i = attachFiles.indexOf(item);
      if (i >= 0) attachFiles.splice(i, 1);
      if (item.li && item.li.parentNode) item.li.parentNode.removeChild(item.li);
      if (attachList && !attachFiles.length) attachList.hidden = true;
    }
    function uploadAttach(file) {
      if (!attachInput || attachFiles.length >= attachMax) {
        say(msg('inqAttachLimit', '最多只能添加 %s 个附件').replace('%s', String(attachMax)));
        return;
      }
      if (attachMaxBytes && file.size > attachMaxBytes) {
        say(msg('inqAttachTooLarge', '附件超过上限（%s）').replace('%s', sizeText(attachMaxBytes)));
        return;
      }
      var item = { token: '', pending: true, li: null };
      var li = document.createElement('li');
      li.className = 'inq-att-item is-uploading';
      var nm = document.createElement('span');
      nm.className = 'inq-att-name';
      nm.textContent = file.name;
      var sz = document.createElement('span');
      sz.className = 'inq-att-size';
      sz.textContent = sizeText(file.size);
      var state = document.createElement('span');
      state.className = 'inq-att-size';
      state.textContent = msg('inqAttachUploading', '上传中…');
      var x = document.createElement('button');
      x.type = 'button';
      x.className = 'inq-att-x';
      x.textContent = '×';
      x.setAttribute('aria-label', msg('inqAttachRemove', '移除附件'));
      x.addEventListener('click', function () { dropAttach(item); });
      li.appendChild(nm);
      li.appendChild(sz);
      li.appendChild(state);
      li.appendChild(x);
      item.li = li;
      attachFiles.push(item);
      attachList.hidden = false;
      attachList.appendChild(li);
      say('');

      var fd = new FormData();
      fd.append('file', file);
      fetch('/website/inquiry/attachments', {
        method: 'POST', body: fd, credentials: 'same-origin', headers: { Accept: 'application/json' }
      }).then(function (r) {
        return r.json().catch(function () { return {}; }).then(function (b) { return { ok: r.ok, body: b }; });
      }).then(function (res) {
        if (!res.ok || !res.body || !res.body.ok || !res.body.file) {
          var code = res.body && res.body.error;
          dropAttach(item);
          if (code === 'tooLarge') say(msg('inqAttachTooLarge', '附件超过上限（%s）').replace('%s', sizeText(attachMaxBytes)));
          else if (code === 'type') say(msg('inqAttachType', '不支持这种文件类型'));
          else say(msg('inqAttachFailed', '附件上传失败，请重试'));
          return;
        }
        item.token = res.body.file.token;
        item.pending = false;
        li.classList.remove('is-uploading');
        if (state.parentNode) state.parentNode.removeChild(state);
      }).catch(function () {
        dropAttach(item);
        say(msg('inqAttachFailed', '附件上传失败，请重试'));
      });
    }
    if (attachBtn && attachInput) {
      attachBtn.addEventListener('click', function () { attachInput.click(); });
      attachInput.addEventListener('change', function () {
        Array.prototype.forEach.call(attachInput.files || [], function (f) { uploadAttach(f); });
        attachInput.value = '';
      });
    }

    function open() {
      if (done) return;
      // 页面上的数量步进器是买家刚调的：带进弹窗，免得再输一遍
      if (qtyInput) {
        var pageQty = document.querySelector('[data-pd-qty-input]');
        var n = pageQty ? parseInt(pageQty.value, 10) : NaN;
        if (n > 0) setQty(n);
      }
      lastFocus = document.activeElement;
      modal.hidden = false;
      document.body.classList.add('inq-lock');
      if (box) window.setTimeout(function () { box.focus(); }, 30);
    }
    function close() {
      modal.hidden = true;
      document.body.classList.remove('inq-lock');
      if (lastFocus && lastFocus.focus) lastFocus.focus();
      // 附件 token 是一次性的：关掉就清空，下次打开是一张干净的表单（旧行由上传时顺手清理）。
      clearAttachments();
      // 发过之后再打开要是一张干净的表单，否则第二次点「发送询盘」会看到上次的成功提示
      if (done) {
        done = false;
        var note = modal.querySelector('.inq-done');
        if (note) note.parentNode.removeChild(note);
        if (form) {
          form.hidden = false;
          form.reset();
        }
        say('');
        if (send) { send.disabled = false; send.textContent = msg('inquiry.send', '确认发送'); }
      }
    }
    function failed(code) {
      var map = {
        name: 'contactErrName', email: 'contactErrEmail', content: 'contactErrContent',
        consent: 'contactErrConsent', rate: 'contactRate'
      };
      say(msg(map[code] || 'contactFailed', '发送失败，请稍后重试'));
    }

    // 入口：商详的「发送询盘」/ 左栏「联系供应商」（都带 data-inquiry-open）
    document.addEventListener('click', function (e) {
      var trigger = e.target.closest ? e.target.closest('[data-inquiry-open]') : null;
      if (!trigger) return;
      e.preventDefault();
      open();
    });
    Array.prototype.forEach.call(modal.querySelectorAll('[data-inquiry-close]'), function (el) {
      el.addEventListener('click', close);
    });
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape' && !modal.hidden) close();
    });
    // 常见问题一点就填进「详细需求」，省得买家自己敲
    Array.prototype.forEach.call(modal.querySelectorAll('[data-inquiry-chip]'), function (chip) {
      chip.addEventListener('click', function () {
        if (!box) return;
        var text = (chip.textContent || '').trim();
        var cur = box.value.trim();
        box.value = cur ? cur + '\n' + text : text;
        box.focus();
        say('');
      });
    });

    if (!form) return;
    form.addEventListener('submit', function (e) {
      e.preventDefault();
      var pending = msg('contactSending', '正在发送…');
      // 原生校验被 novalidate 关掉了：这里按顺序给一条能照做的提示
      if ((box ? box.value.trim() : '').length < 5) { say(msg('contactErrContent', '请把需求写清楚一些')); if (box) box.focus(); return; }
      var name = form.querySelector('[name="name"]');
      if (name && !name.value.trim()) { say(msg('contactErrName', '请填写称呼')); name.focus(); return; }
      var email = form.querySelector('[name="email"]');
      if (email && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.value.trim())) { say(msg('contactErrEmail', '请填写有效的邮箱地址')); email.focus(); return; }
      var consent = form.querySelector('[name="consent"]');
      if (consent && !consent.checked) { say(msg('contactErrConsent', '请先阅读并同意隐私政策')); consent.focus(); return; }
      // 附件还在传就等它：带着半截 token 提交等于把附件丢了
      var uploading = false;
      attachFiles.forEach(function (f) { if (f.pending) uploading = true; });
      if (uploading) { say(msg('inqAttachUploading', '上传中…')); return; }
      say('');
      // 商品与数量拼进正文：POST /contact 只认 ContactForm 的固定字段，多传会被丢掉，
      // 供应商在后台也只能读到正文，所以把这两行连同买家描述一起发过去。
      var fd = new FormData(form);
      var meta = [];
      var prodName = modal.querySelector('.inq-prod-text strong');
      if (prodName && prodName.textContent.trim()) {
        meta.push(msg('inqMetaProduct', '商品：') + prodName.textContent.trim());
      }
      if (qtyInput) {
        var qtyVal = parseInt(qtyInput.value, 10) || qtyMin;
        meta.push(msg('inqMetaQty', '数量：') + qtyVal + (qtyUnit ? ' ' + qtyUnit : ''));
      }
      if (meta.length) {
        fd.set('content', meta.join('\n') + '\n\n' + (fd.get('content') || '').toString().trim());
      }
      // 附件 token 逐个追加（同名多值）：后端按 token 认领到这条留言
      attachFiles.forEach(function (f) { if (f.token) fd.append('fileTokens', f.token); });
      if (send) { send.disabled = true; send.textContent = pending; }
      fetch(form.getAttribute('action') || '/contact', {
        method: 'POST',
        body: fd,
        credentials: 'same-origin',
        headers: { Accept: 'application/json' }
      }).then(function (r) {
        return r.json().catch(function () { return {}; }).then(function (body) { return { ok: r.ok, body: body }; });
      }).then(function (res) {
        if (res.ok && res.body && res.body.ok) {
          done = true;
          var note = document.createElement('p');
          note.className = 'inq-done';
          note.textContent = msg('contactSent', '已收到，我们会尽快回复');
          form.hidden = true;
          modal.querySelector('.inq-card').appendChild(note);
          window.setTimeout(close, 1600);
          return;
        }
        if (send) { send.disabled = false; send.textContent = msg('inquiry.send', '确认发送'); }
        failed(res.body && res.body.error);
      }).catch(function () {
        if (send) { send.disabled = false; send.textContent = msg('inquiry.send', '确认发送'); }
        failed('fail');
      });
    });
  }

  // 左列供应商卡的「联系供应商」：复用右栏 CTA 的点击逻辑（商品上下文、聊天打开都在那边）
  function initSupplierCta() {
    var btn = document.querySelector('[data-pd-supplier-cta]');
    var cta = document.getElementById('pd-cta');
    if (!btn || !cta) return;
    btn.addEventListener('click', function () { cta.click(); });
  }

  // ---- 订单状态页：pending 时轮询状态端点直到终态。访客在收银台付款后跳回本页，
  //      SSR 渲染的是回跳那一刻的状态（通常还是待支付），不轮询就得手动刷新才能看到
  //      「支付成功」。状态文本用 JSStrings 里的 orderPending/… 翻译；终态停止轮询，
  //      pending 轮询期间有新收银台地址就同步「继续支付」链接。 ----
  function initOrderStatus() {
    var root = document.querySelector('[data-order-status]');
    if (!root) return;
    var url = root.getAttribute('data-order-status-url');
    var statusEl = root.querySelector('[data-order-status-text]');
    var payLink = root.querySelector('[data-order-pay]');
    var hint = root.querySelector('[data-order-poll-hint]');
    if (!url || !statusEl) return;
    var PAY_INTERVAL = 3000;
    var PAY_MAX_POLLS = 600; // 3s × 600 ≈ 30 分钟：支付会话一般远短于此，超限就停（页面刷新可重来）
    var polls = 0;
    var stopped = false;
    if (hint) hint.hidden = false;
    function statusText(st) {
      var key = st === 'paid' ? 'orderPaid' : st === 'failed' ? 'orderFailed' : st === 'closed' ? 'orderClosed' : 'orderPending';
      return msg(key, st === 'paid' ? '支付成功，感谢购买！' : st === 'failed' ? '支付未完成' : st === 'closed' ? '订单已关闭' : '待支付');
    }
    function finish(st, finalText) {
      if (stopped) return;
      stopped = true;
      if (finalText) statusEl.textContent = finalText;
      if (st === 'paid') statusEl.classList.add('is-paid');
      // 终态收掉「继续支付」：pending 时 SSR 渲染的按钮指向旧收银台地址，
      // paid/failed/closed 后继续挂着只会让人空点（failed 的引导文案是重新下单）。
      if (st && payLink) payLink.hidden = true;
      if (hint) hint.hidden = true;
    }
    function tick() {
      if (stopped) return;
      polls += 1;
      if (polls > PAY_MAX_POLLS) { finish('', ''); return; }
      fetch(url, { headers: { Accept: 'application/json' } }).then(function (r) { return r.json(); }).then(function (j) {
        if (stopped || !j || j.code !== 0 || !j.data) return; // 单次网络/响应异常交给下一轮
        var st = String(j.data.status || '');
        if (st && st !== 'pending') {
          finish(st, statusText(st));
          return;
        }
        // 仍是 pending：收银台地址可能已轮换（幂等取回的新地址），同步「继续支付」
        if (payLink && j.data.payUrl && payLink.getAttribute('href') !== j.data.payUrl) {
          payLink.setAttribute('href', j.data.payUrl);
        }
        setTimeout(tick, PAY_INTERVAL);
      }).catch(function () {
        // 网络抖动：不中断流程，下一轮再试
        if (!stopped) setTimeout(tick, PAY_INTERVAL);
      });
    }
    tick();
  }

  function initProductDetail() {
    if (!document.querySelector('[data-pd-page]')) return;
    // 商详样式不再在这里注入（原 PD_CSS）：运行时塞 <style> 会让 HTML 先按无样式渲染，
    // 主图以原图尺寸铺满一屏再跳回格子。样式已并入阻塞加载的 site.css 末尾「商品详情页」段。
    // 默认选中第一张「图片」缩略图（视频缩略图不算选中态：主图位一开始显示的是封面）
    // 视频缩略图拿商品封面当底图：只放一块灰底三角太素，也不像阿里的「视频帧 + 播放键」
    Array.prototype.forEach.call(document.querySelectorAll('.pd-thumb-video'), function (btn) {
      var poster = btn.getAttribute('data-video-poster');
      if (poster) btn.style.backgroundImage = 'url("' + poster + '")';
    });
    var first = document.querySelector('.pd-gallery .pd-thumb:not(.pd-thumb-video)');
    if (first && !document.querySelector('.pd-gallery .pd-thumb.active')) first.classList.add('active');
    initGalleryZoom();
    initProductQty();
  initProductSubtotal();
    initPurchase();
    initInquiry();
    initStickyInquiry();
    initInquirySticky();
    initSupplierCta();
  }

  // ---- 产品在线购买（PurchaseEnabled + 有定价才渲染）：数量同步、表单 fetch 提交。
  // 第一次点「立即购买」展开邮箱等字段（下单必须留联系方式），填完再提交；
  // 成功后跳收银台。金额与商品由服务端按产品定价计算，前端只传 slug 与数量。 ----
  function initPurchase() {
    var form = document.querySelector('[data-pd-buy]');
    if (!form) return;
    var fields = form.querySelector('[data-pd-buy-fields]');
    var btn = form.querySelector('[data-pd-buy-btn]');
    var qtyHidden = form.querySelector('[data-pd-buy-qty]');
    var qtyInput = document.querySelector('[data-pd-qty-input]');
    var revealed = false;
    var orderErrorShown = false;
    // 数量步进器联动：下单数量始终跟随页面上可见的数量控件
    if (qtyInput && qtyHidden) {
      qtyInput.addEventListener('input', function () {
        var n = parseInt(String(qtyInput.value).replace(/[^\d]/g, ''), 10);
        if (isNaN(n) || n < 1) n = 1;
        qtyHidden.value = String(n);
      });
    }
    // 无 JS 整页提交失败 303 回商品页带 order_error：翻成提示条只显示一次
    var m = /(?:^|[?&])order_error=([^&]*)/.exec(location.search);
    if (m && !orderErrorShown) {
      orderErrorShown = true;
      var box = document.createElement('p');
      box.className = 'pd-order-error';
      box.setAttribute('role', 'alert');
      box.textContent = msg('buyFailed', '下单失败，请稍后重试');
      form.parentNode.insertBefore(box, form);
      // 清掉查询参数，避免刷新页面后提示条一直挂着
      var u = location.pathname + location.search.replace(/[?&]order_error=[^&]*/, '').replace(/^&/, '?');
      window.history.replaceState({}, '', u);
    }
    form.addEventListener('submit', function (e) {
      e.preventDefault();
      if (!revealed && fields) {
        // 首次点击：展开联系方式字段（匿名下单必须留邮箱），按钮变成「提交订单」
        revealed = true;
        fields.hidden = false;
        var email = fields.querySelector('[name="email"]');
        if (email) email.focus();
        return;
      }
      var fd = new FormData(form);
      var email = String(fd.get('email') || '').trim();
      if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
        if (fields) fields.hidden = false;
        var em = fields ? fields.querySelector('[name="email"]') : null;
        if (em) em.focus();
        return;
      }
      var payload = {
        slug: String(fd.get('slug') || ''),
        qty: parseInt(String(fd.get('qty') || '1'), 10) || 1,
        email: email,
        phone: String(fd.get('phone') || '').trim(),
        remark: String(fd.get('remark') || '').trim()
      };
      var busy = true;
      if (btn) { btn.disabled = true; busy = true; }
      fetch(form.getAttribute('action'), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
        body: JSON.stringify(payload)
      }).then(function (r) { return r.json(); }).then(function (j) {
        busy = false;
        if (btn) btn.disabled = false;
        if (!j || j.code !== 0 || !j.data) throw new Error('failed');
        // 有收银台地址就跳渠道（mock 收银台 / 易支付网关 / Stripe Checkout）；
        // 只回了订单号（无跳转型渠道）时落订单状态页轮询。
        if (j.data.payUrl) { location.href = j.data.payUrl; return; }
        if (j.data.orderNo) { location.href = location.pathname.replace(/\/products\/[^/]+/, '/orders/') + j.data.orderNo; }
      }).catch(function () {
        busy = false;
        if (btn) btn.disabled = false;
        var box = document.createElement('p');
        box.className = 'pd-order-error';
        box.setAttribute('role', 'alert');
        box.textContent = msg('buyFailed', '下单失败，请稍后重试');
        form.parentNode.insertBefore(box, form);
      });
    });
  }

  // ---- 产品列表排序：自绘下拉（原生弹层跟不上主题）。选项本身就是提交按钮，
  //      点了带 ?sort=… 原生跳转、隐藏的分类与检索词一起提交，没有 JS 也能用；
  //      这里只补 <details> 自己不管的两件小事：点面板外收起、Esc 收起并回焦。 ----
  function initProductSort() {
    var menu = document.querySelector('[data-sort-menu]');
    if (!menu || menu.dataset.bound) return;
    menu.dataset.bound = '1';
    var trigger = menu.querySelector('.pc-sort-trigger');

    document.addEventListener('click', function (e) {
      if (menu.open && !menu.contains(e.target)) menu.open = false;
    });
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape' && menu.open) {
        menu.open = false;
        if (trigger) trigger.focus();
      }
    });
  }

  // ---- 详情页分享条 / 相邻导航：四套主题共用，样式随脚本注入（不必每套 CSS 抄一遍） ----
  // 颜色用 currentColor 与站点 token（var(--text) 等，带 Canvas 兜底）：
  // 系统色 Canvas 只在 color-scheme:normal 下解析成白色，深色主题里会变成白底白字。

  // 悬停提示气泡 + 键盘焦点圈：页脚社交图标、详情页分享按钮共用这一份（模板写 data-tip 即可）。
  // 以前图标靠浏览器原生 title（延迟约 1s、跟系统样式），分享条是自绘气泡，
  // 同一页两种提示观感，现在统一走 CSS，模板里不再写 title。
  var TIP_CSS = [
    '[data-tip]{position:relative}',
    '[data-tip]::after{content:attr(data-tip);position:absolute;inset-block-end:calc(100% + 8px);inset-inline-start:50%;transform:translateX(-50%);padding:4px 8px;border-radius:3px;background:var(--text,CanvasText);color:var(--bg,Canvas);box-shadow:0 4px 14px rgba(0,0,0,.16);font-size:11px;line-height:1.4;white-space:nowrap;opacity:0;pointer-events:none;transition:opacity .15s;z-index:3}',
    '[data-tip]:hover::after,[data-tip]:focus-visible::after{opacity:1}',
    '[data-tip]:focus-visible{outline:2px solid currentColor;outline-offset:2px}',
    // Viewer.js 的临时图片宿主：不能 display:none（浏览器会跳过图片加载），
    // 挪到视口外即可，翻页时由组件按 data-zoom-src 取原图。
    '.site-viewer-host{position:absolute;inset-inline-start:-9999px;top:0;width:1px;height:1px;overflow:hidden;pointer-events:none}',
    // 可点开放大的图给手势提示：商详主图（.pd-stage）自带，新闻封面 / 正文图在这里补
    'img.is-zoomable{cursor:zoom-in}'
  ].join('');

  // 样式表只注入一次；同一 id 已存在就跳过（局部刷新会重复进 initSite）。
  function injectCSS(id, css) {
    if (document.getElementById(id)) return;
    var st = document.createElement('style');
    st.id = id;
    st.textContent = css;
    document.head.appendChild(st);
  }

  var DT_CSS = [
    '.dt-share{display:flex;align-items:center;flex-wrap:wrap;gap:12px;margin:32px 0 18px;padding:15px 0;border-block:1px solid rgba(128,128,128,.28)}',
    '.dt-share-label{font-size:12px;font-weight:600;letter-spacing:.06em;opacity:.72}',
    '.dt-share-icons{display:flex;flex-wrap:wrap;gap:8px}',
    // 分享按钮的外观全部走 --dt-chip-* 变量（尺寸/图标/圆角/描边/悬停填充），
    // 四套主题各自把这组变量对齐自己的页脚社交图标，两处看起来是同一套控件；
    // hover 只换颜色与填充，不做位移/缩放，和页脚图标的手感一致。
    '.dt-share-btn{position:relative;display:inline-flex;align-items:center;justify-content:center;box-sizing:border-box;width:var(--dt-chip-size,34px);height:var(--dt-chip-size,34px);padding:0;border:1px solid var(--dt-chip-border,rgba(128,128,128,.38));border-radius:var(--dt-chip-radius,8px);background:transparent;color:var(--dt-chip-fg,inherit);font:inherit;cursor:pointer;transition:color .15s,background .15s,border-color .15s}',
    '.dt-share-btn:hover{color:var(--dt-chip-hover-fg,#fff);background:var(--dt-chip-hover,rgba(128,128,128,.12));border-color:var(--dt-chip-hover,rgba(128,128,128,.5))}',
    '.dt-share-btn.is-copied{border-color:currentColor;background:rgba(76,183,130,.16)}',
    '.dt-share-btn.is-failed{border-color:#c0392b;background:rgba(192,57,43,.12)}',
    '.dt-share-btn svg{width:var(--dt-chip-icon,16px);height:var(--dt-chip-icon,16px);flex:none}',
    // 复制按钮的三态图标常驻 DOM（服务端一次出图），这里只按状态 class 切换显示。
    '.dt-share-btn .dt-ico{display:none}',
    '.dt-share-btn .dt-ico-copy{display:inline-flex}',
    '.dt-share-btn.is-copied .dt-ico-copy,.dt-share-btn.is-failed .dt-ico-copy{display:none}',
    '.dt-share-btn.is-copied .dt-ico-check{display:inline-flex}',
    '.dt-share-btn.is-failed .dt-ico-fail{display:inline-flex}',
    '@media (hover:none){.dt-share-btn{width:44px !important;height:44px !important}}',
    // 上一篇/下一篇导航：细边框 + 透明底（对齐 linear 文档底部的卡片口径，灰底块太像按钮）；
    // 箭头挂在标题行（模板 .dt-t），hover 往对应方向滑 3px，比小字上的箭头更有点击引导；
    // 标题允许两行内换行（原来 nowrap 直接截断，中文长标题截半不可读）；
    // auto-fit 让只有一侧有文章时那一张占满整行，不留半个空卡片。
    '.dt-nav{display:grid;grid-template-columns:repeat(auto-fit,minmax(0,1fr));gap:14px;margin:24px 0 8px}',
    '.dt-nav-item{display:flex;flex-direction:column;gap:6px;min-width:0;padding:14px 16px;border:1px solid var(--border);border-radius:8px;background:transparent;color:inherit;text-decoration:none;transition:background .15s,border-color .15s}',
    'a.dt-nav-item:hover{background:var(--wash);border-color:var(--border-strong)}',
    '.dt-nav-item small{font-size:12px;letter-spacing:.04em;color:var(--text-3)}',
    '.dt-nav-item span{font-size:14px;font-weight:500;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden}',
    '.dt-nav-item .dt-t::before,.dt-nav-item .dt-t::after{display:inline-block;transition:transform .15s}',
    '.dt-prev .dt-t::before{content:"←";margin-inline-end:7px}',
    '.dt-next .dt-t::after{content:"→";margin-inline-start:7px}',
    'a.dt-prev:hover .dt-t::before{transform:translateX(-3px)}',
    'a.dt-next:hover .dt-t::after{transform:translateX(3px)}',
    '.dt-next{text-align:end;align-items:flex-end}',
    '.dt-empty{background:none}',
    '.dt-share-panel{position:fixed;inset:0;z-index:9999;display:flex;align-items:center;justify-content:center;padding:20px;background:rgba(0,0,0,.45)}',
    '.dt-share-card{width:min(420px,100%);box-sizing:border-box;padding:22px;border-radius:8px;background:var(--panel-2,Canvas);color:var(--text,CanvasText);box-shadow:0 24px 60px rgba(0,0,0,.28)}',
    '.dt-share-card h3{margin:0 0 10px;font-size:17px}',
    '.dt-share-card p{margin:0 0 14px;font-size:13px;line-height:1.7;opacity:.82}',
    '.dt-share-url{display:block;width:100%;box-sizing:border-box;padding:9px 10px;margin-bottom:14px;border:1px solid rgba(128,128,128,.45);border-radius:4px;background:transparent;color:inherit;font-size:12px}',
    '.dt-share-actions{display:flex;flex-wrap:wrap;gap:8px;justify-content:flex-end}',
    '.dt-share-actions button{padding:8px 14px;border:1px solid rgba(128,128,128,.45);border-radius:4px;background:transparent;color:inherit;font:inherit;font-size:12.5px;cursor:pointer}',
    '.dt-share-actions button.dt-primary{background:var(--text,CanvasText);color:var(--bg,Canvas);border-color:var(--text,CanvasText)}',
    '@media (max-width:560px){.dt-nav{grid-template-columns:1fr}.dt-next{text-align:start;align-items:flex-start}}'
  ].join('');

  // 分享条的图标不进 JS：由服务端 render.go 的 shareBar 出图（Tabler Icons outline，
  // 与页脚社交图标同一套、同一 stroke-width），这里只注入样式、绑事件，
  // 避免站上出现第二套图标语言，也免得图标路径在 Go 与 JS 各存一份后走样。

  function sharePageURL() {
    var link = document.querySelector('link[rel="canonical"]');
    return (link && link.href) || window.location.href;
  }
  function sharePageTitle() {
    var og = document.querySelector('meta[property="og:title"]');
    return (og && og.content) || document.title;
  }
  // 分享目标只在点击时拼：URL 全部走 encodeURIComponent，站点标题里带 & / 引号也不会串参数。
  function shareTarget(kind, url, title) {
    var u = encodeURIComponent(url), t = encodeURIComponent(title);
    if (kind === 'weibo') return 'https://service.weibo.com/share/share.php?url=' + u + '&title=' + t;
    if (kind === 'facebook') return 'https://www.facebook.com/sharer/sharer.php?u=' + u;
    if (kind === 'x') return 'https://twitter.com/intent/tweet?url=' + u + '&text=' + t;
    if (kind === 'linkedin') return 'https://www.linkedin.com/sharing/share-offsite/?url=' + u;
    return '';
  }

  function openWechatPanel(url, title) {
    var old = document.querySelector('.dt-share-panel');
    if (old && old.parentNode) old.parentNode.removeChild(old);
    var panel = document.createElement('div');
    panel.className = 'dt-share-panel';
    panel.setAttribute('role', 'dialog');
    panel.setAttribute('aria-modal', 'true');
    var card = document.createElement('div');
    card.className = 'dt-share-card';
    var h = document.createElement('h3');
    h.textContent = msg('shareWechatTitle', '分享到微信 / 朋友圈');
    var p = document.createElement('p');
    p.textContent = msg('shareWechatTip', '浏览器无法直接唤起微信，复制链接后在微信中打开并点右上角菜单分享。');
    var input = document.createElement('input');
    input.className = 'dt-share-url';
    input.readOnly = true;
    input.value = url;
    input.setAttribute('aria-label', msg('shareCopy', '复制链接'));
    var actions = document.createElement('div');
    actions.className = 'dt-share-actions';
    function action(label, primary, fn) {
      var b = document.createElement('button');
      b.type = 'button';
      if (primary) b.className = 'dt-primary';
      b.textContent = label;
      b.addEventListener('click', fn);
      return b;
    }
    function close() {
      if (panel.parentNode) panel.parentNode.removeChild(panel);
      document.removeEventListener('keydown', onKey);
    }
    function onKey(e) { if (e.key === 'Escape') close(); }
    var copyBtn = action(msg('shareCopy', '复制链接'), true, function () {
      function label(text, ok) {
        copyBtn.textContent = text;
        copyBtn.classList.toggle('dt-primary', ok !== false);
      }
      label('\u2713 ' + msg('shareCopiedShort', '已复制'), true);
      copyText(url).then(function () {
        toast(msg('shareCopied', '链接已复制'));
      }).catch(function () {
        label(msg('shareFailedShort', '复制失败'), false);
        toast(msg('shareCopyFailed', '请手动复制链接'));
      });
      window.clearTimeout(copyBtn._copyTimer);
      copyBtn._copyTimer = window.setTimeout(function () {
        label(msg('shareCopy', '复制链接'), true);
      }, 1800);
    });
    actions.appendChild(copyBtn);
    if (navigator.share) {
      actions.appendChild(action(msg('shareSystemShare', '系统分享'), false, function () {
        navigator.share({ title: title, url: url }).catch(function () { /* 用户取消 */ });
      }));
    }
    actions.appendChild(action(msg('shareClose', '关闭'), false, close));
    card.appendChild(h); card.appendChild(p); card.appendChild(input); card.appendChild(actions);
    panel.appendChild(card);
    panel.addEventListener('click', function (e) { if (e.target === panel) close(); });
    document.addEventListener('keydown', onKey);
    document.body.appendChild(panel);
    input.focus();
  }

  // 复制按钮的自身反馈：图标换对勾（或叉号）+ tooltip 变更，1.8s 后复原。
  // 三个图标是服务端一次出的（.dt-ico-copy / .dt-ico-check / .dt-ico-fail），
  // 这里只切状态 class 让 CSS 换显示，不再往按钮里塞 SVG。
  function flashCopyButton(btn, ok) {
    if (!btn) return;
    if (!btn._origTip) btn._origTip = btn.getAttribute('data-tip') || '';
    btn.classList.toggle('is-copied', !!ok);
    btn.classList.toggle('is-failed', !ok);
    var label = ok ? msg('shareCopiedShort', '已复制') : msg('shareFailedShort', '复制失败');
    btn.setAttribute('data-tip', label);
    btn.setAttribute('aria-label', label);
    window.clearTimeout(btn._copyTimer);
    btn._copyTimer = window.setTimeout(function () {
      btn.classList.remove('is-copied', 'is-failed');
      btn.setAttribute('data-tip', btn._origTip);
      btn.setAttribute('aria-label', btn._origTip);
    }, 1800);
  }

  function initShare() {
    var bar = document.querySelector('[data-share-bar]');
    if (!bar) return;
    // 按钮与文案都由服务端渲染（render.go 的 shareBar），这里只补样式和交互。
    var icons = bar.querySelector('.dt-share-icons');
    if (!icons) return;
    injectCSS('site-dt-style', DT_CSS);
    var url = sharePageURL(), title = sharePageTitle();
    icons.addEventListener('click', function (e) {
      var btn = e.target.closest ? e.target.closest('[data-share]') : null;
      if (!btn) return;
      var kind = btn.getAttribute('data-share');
      if (kind === 'wechat') { openWechatPanel(url, title); return; }
      if (kind === 'copy') {
        flashCopyButton(btn, true); // 先确认点击，复制在后台完成
        copyText(url).then(function () {
          flashCopyButton(btn, true);
          toast(msg('shareCopied', '链接已复制'));
        }).catch(function () {
          flashCopyButton(btn, false);
          toast(msg('shareCopyFailed', '请手动复制链接'));
        });
        return;
      }
      var target = shareTarget(kind, url, title);
      if (target) window.open(target, '_blank', 'noopener,noreferrer,width=720,height=560');
    });
  }

  // ---- 首页真实产品演示：页面切换、检索、操作策略与轻量演示反馈 ----
  function initProductDemo() {
    var root = document.querySelector('[data-product-demo]');
    if (!root || root.dataset.bound) return;
    root.dataset.bound = '1';

    var tabs = Array.prototype.slice.call(root.querySelectorAll('[data-demo-tab]'));
    var panels = Array.prototype.slice.call(root.querySelectorAll('[data-demo-panel]'));
    var quickButtons = Array.prototype.slice.call(root.querySelectorAll('[data-demo-quick-actions]'));
    var actionButtons = Array.prototype.slice.call(root.querySelectorAll('[data-demo-action]'));
    var modeMenu = root.querySelector('[data-demo-mode]');
    var modeOptions = Array.prototype.slice.call(root.querySelectorAll('[data-demo-mode-value]'));
    var modeCurrent = root.querySelector('[data-demo-mode-current]');
    var searchToggle = root.querySelector('[data-demo-search-toggle]');
    var search = root.querySelector('#demo-search');
    var searchInput = root.querySelector('[data-demo-search-input]');
    var searchItems = Array.prototype.slice.call(root.querySelectorAll('[data-demo-search-results] li'));
    var searchButtons = Array.prototype.slice.call(root.querySelectorAll('[data-demo-search-result]'));
    var searchEmpty = root.querySelector('[data-demo-search-empty]');
    var confirmBar = root.querySelector('[data-demo-confirm]');
    var confirmYes = root.querySelector('[data-demo-confirm-yes]');
    var confirmNo = root.querySelector('[data-demo-confirm-no]');
    var resetButton = root.querySelector('[data-demo-reset]');
    var status = root.querySelector('[data-demo-status]');
    var dialog = root.parentNode.querySelector('#demo-user-dialog');
    var userForm = dialog && dialog.querySelector('[data-demo-user-form]');
    var dialogCloseButtons = dialog ? Array.prototype.slice.call(dialog.querySelectorAll('[data-demo-dialog-close]')) : [];
    var activeScreen = 'dashboard';
    var modeValue = 'interactive';
    var pendingAction = null;
    var lastActionButton = null;
    var toggled = false;
    var licensed = false;

    function showStatus(text) {
      if (status) status.textContent = text;
    }

    function hideConfirm() {
      pendingAction = null;
      if (confirmBar) confirmBar.hidden = true;
    }

    function closeSearch(restoreFocus) {
      if (!search) return;
      search.hidden = true;
      if (searchToggle) searchToggle.setAttribute('aria-expanded', 'false');
      if (restoreFocus && searchToggle) searchToggle.focus();
    }

    function openSearch() {
      if (!search) return;
      search.hidden = false;
      if (searchToggle) searchToggle.setAttribute('aria-expanded', 'true');
      if (searchInput) {
        searchInput.value = '';
        filterSearch('');
        searchInput.focus();
      }
    }

    function selectScreen(name, focusTab) {
      var selected = tabs.find(function (tab) { return tab.dataset.demoTab === name; });
      if (!selected) return;
      activeScreen = name;
      hideConfirm();
      closeSearch(false);
      tabs.forEach(function (tab) {
        var active = tab === selected;
        tab.setAttribute('aria-selected', active ? 'true' : 'false');
        tab.tabIndex = active ? 0 : -1;
      });
      panels.forEach(function (panel) { panel.hidden = panel.dataset.demoPanel !== name; });
      quickButtons.forEach(function (button) { button.hidden = button.dataset.demoQuickActions !== name; });
      showStatus(selected.dataset.demoTabStatus || '');
      if (focusTab) selected.focus();
    }

    function setActionState(action, pressed) {
      actionButtons.filter(function (button) { return button.dataset.demoAction === action; }).forEach(function (button) {
        button.setAttribute('aria-pressed', pressed ? 'true' : 'false');
        var label = pressed ? button.dataset.labelOn : button.dataset.labelOff;
        if (label) {
          button.setAttribute('aria-label', label);
          var labelEl = button.querySelector('.hp-hotspot-label');
          if (labelEl) labelEl.textContent = label;
          if (button.classList.contains('hp-demo-quick-btn')) button.textContent = label;
        }
      });
    }

    function closeDialog() {
      if (dialog && dialog.open) dialog.close();
    }

    function executeAction(action) {
      if (action === 'users' || action === 'plugins') {
        selectScreen(action, false);
        return;
      }
      if (action === 'create') {
        if (dialog) {
          if (typeof dialog.showModal === 'function') dialog.showModal();
          else dialog.setAttribute('open', '');
          var firstInput = dialog.querySelector('input');
          if (firstInput) firstInput.focus();
        }
        return;
      }
      if (action === 'toggle') {
        toggled = !toggled;
        setActionState('toggle', toggled);
        var text = toggled
          ? root.querySelector('[data-demo-action="toggle"]').dataset.statusOn
          : root.querySelector('[data-demo-action="toggle"]').dataset.statusOff;
        showStatus(text || '');
        return;
      }
      if (action === 'license') {
        licensed = true;
        setActionState('license', licensed);
        showStatus(root.querySelector('[data-demo-action="license"]').dataset.statusOn || '');
        return;
      }
      if (action === 'layout') {
        showStatus(root.querySelector('[data-demo-action="layout"]').dataset.status || '');
      }
    }

    function requestAction(action) {
      if (modeValue === 'readonly') {
        showStatus(root.dataset.readonlyStatus || '');
        return;
      }
      var mutation = action === 'toggle' || action === 'license' || action === 'layout' || action === 'create';
      if (modeValue === 'confirm' && mutation) {
        pendingAction = action;
        if (confirmBar) confirmBar.hidden = false;
        if (confirmNo) confirmNo.focus();
        return;
      }
      executeAction(action);
    }

    function filterSearch(query) {
      var needle = query.trim().toLocaleLowerCase();
      var visible = 0;
      searchItems.forEach(function (item) {
        var match = item.textContent.toLocaleLowerCase().indexOf(needle) !== -1;
        item.hidden = !match;
        if (match) visible += 1;
      });
      if (searchEmpty) searchEmpty.hidden = visible !== 0;
    }

    tabs.forEach(function (tab, index) {
      tab.addEventListener('click', function () { selectScreen(tab.dataset.demoTab, false); });
      tab.addEventListener('keydown', function (event) {
        var targetIndex = index;
        if (event.key === 'ArrowRight') targetIndex = (index + 1) % tabs.length;
        else if (event.key === 'ArrowLeft') targetIndex = (index - 1 + tabs.length) % tabs.length;
        else if (event.key === 'Home') targetIndex = 0;
        else if (event.key === 'End') targetIndex = tabs.length - 1;
        else return;
        event.preventDefault();
        selectScreen(tabs[targetIndex].dataset.demoTab, true);
      });
    });

    root.addEventListener('click', function (event) {
      var actionButton = event.target.closest('[data-demo-action]');
      if (actionButton) {
        lastActionButton = actionButton;
        requestAction(actionButton.dataset.demoAction);
        return;
      }
      var searchResult = event.target.closest('[data-demo-search-result]');
      if (searchResult) {
        selectScreen(searchResult.dataset.demoSearchResult, true);
        return;
      }
    });

    modeOptions.forEach(function (option) {
      option.addEventListener('click', function () {
        modeValue = option.dataset.demoModeValue;
        modeOptions.forEach(function (item) { item.setAttribute('aria-selected', item === option ? 'true' : 'false'); });
        if (modeCurrent) modeCurrent.textContent = option.querySelector('span').textContent;
        if (modeMenu) modeMenu.open = false;
        hideConfirm();
        showStatus(option.dataset.demoModeStatus || '');
      });
      option.addEventListener('keydown', function (event) {
        if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return;
        event.preventDefault();
        var index = modeOptions.indexOf(option);
        var next = event.key === 'ArrowDown'
          ? (index + 1) % modeOptions.length
          : (index - 1 + modeOptions.length) % modeOptions.length;
        modeOptions[next].focus();
      });
    });

    if (searchToggle) searchToggle.addEventListener('click', function () {
      if (search && !search.hidden) closeSearch(true);
      else openSearch();
    });
    if (searchInput) {
      searchInput.addEventListener('input', function () { filterSearch(searchInput.value); });
      searchInput.addEventListener('keydown', function (event) {
        if (event.key === 'Enter') {
          var first = searchItems.find(function (item) { return !item.hidden; });
          if (first) {
            event.preventDefault();
            var button = first.querySelector('[data-demo-search-result]');
            if (button) selectScreen(button.dataset.demoSearchResult, true);
          }
        } else if (event.key === 'Escape') {
          event.preventDefault();
          closeSearch(true);
        }
      });
    }

    if (confirmNo) confirmNo.addEventListener('click', hideConfirm);
    if (confirmYes) confirmYes.addEventListener('click', function () {
      var action = pendingAction;
      hideConfirm();
      if (action) executeAction(action);
    });

    if (resetButton) resetButton.addEventListener('click', function () {
      toggled = false;
      licensed = false;
      setActionState('toggle', false);
      setActionState('license', false);
      modeValue = 'interactive';
      modeOptions.forEach(function (option) {
        var active = option.dataset.demoModeValue === 'interactive';
        option.setAttribute('aria-selected', active ? 'true' : 'false');
        if (active && modeCurrent) modeCurrent.textContent = option.querySelector('span').textContent;
      });
      if (modeMenu) modeMenu.open = false;
      closeDialog();
      if (userForm) userForm.reset();
      selectScreen('dashboard', false);
      showStatus(resetButton.dataset.status || '');
    });

    if (dialog) {
      dialogCloseButtons.forEach(function (button) { button.addEventListener('click', closeDialog); });
      dialog.addEventListener('click', function (event) {
        if (event.target === dialog) {
          var rect = dialog.getBoundingClientRect();
          if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) closeDialog();
        }
      });
    }
    if (userForm) userForm.addEventListener('submit', function (event) {
      event.preventDefault();
      closeDialog();
      userForm.reset();
      showStatus(userForm.dataset.status || '');
      if (lastActionButton && lastActionButton.isConnected) lastActionButton.focus();
    });

    onWin(document, 'click', function (event) {
      if (modeMenu && modeMenu.open && !modeMenu.contains(event.target)) modeMenu.open = false;
    });
    onWin(document, 'keydown', function (event) {
      if (event.key !== 'Escape') return;
      if (modeMenu && modeMenu.open) {
        modeMenu.open = false;
        var summary = modeMenu.querySelector('summary');
        if (summary) summary.focus();
      } else if (!confirmBar.hidden) {
        hideConfirm();
      } else if (search && !search.hidden) {
        closeSearch(true);
      }
    });
  }

  // ---- 在线客服挂件：实现见 chat-widget.js（NexChat.mount），官网只是宿主之一 ----
  function initChat() {
    var root = document.querySelector('[data-chat]');
    if (!root || !window.NexChat) return;
    window.NexChat.mount(root, { t: msg });
  }

  window.__siteInit = initSite;
  initSite();
})();
