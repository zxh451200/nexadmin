// NexAdmin 官网默认模板脚本：滚动进场、导航状态与交互微效果。无外部依赖。
(function () {
  'use strict';

  // 文案由模板注入（<script id="site-i18n">），新增语言无需改脚本。
  var i18n = {};
  try {
    var i18nEl = document.getElementById('site-i18n');
    if (i18nEl) i18n = JSON.parse(i18nEl.textContent || '{}');
  } catch (e) { i18n = {}; }
  var msg = function (key, fallback) { return i18n[key] || fallback; };

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
        try { window.sessionStorage.setItem('nex_lang_banner_dismissed', '1'); } catch (e) { /* ignore */ }
      });
    }
  }

  var reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  var fine = window.matchMedia('(hover: hover) and (pointer: fine)').matches;

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
    window.addEventListener('scroll', onScroll, { passive: true });
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
    window.addEventListener('scroll', spy, { passive: true });
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
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape' && !mnav.hidden) {
        setMenu(false);
        toggle.focus();
      }
    });
    window.addEventListener('resize', function () {
      if (window.innerWidth > 760 && !mnav.hidden) setMenu(false);
    }, { passive: true });
  }

  // 语言菜单：点外部 / Esc 收起（<details> 原生负责开合，不依赖脚本）。
  document.querySelectorAll('.lang-switch').forEach(function (box) {
    document.addEventListener('click', function (e) {
      if (!box.contains(e.target)) box.removeAttribute('open');
    });
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape') box.removeAttribute('open');
    });
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
      if (!window.fetch || !window.URL || !window.Blob) return;
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

  // 复制按钮（安装命令）：优先 Clipboard API，HTTP 等非安全上下文退回 execCommand。
  var copyText = function (text) {
    if (navigator.clipboard && window.isSecureContext) return navigator.clipboard.writeText(text);
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
  };
  document.querySelectorAll('[data-copy]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var src = document.querySelector(btn.getAttribute('data-copy'));
      if (!src) return;
      var original = btn.textContent;
      copyText(src.textContent.trim()).then(function () {
        btn.textContent = msg('copied', 'Copied');
        btn.classList.add('copied');
        window.setTimeout(function () {
          btn.textContent = original;
          btn.classList.remove('copied');
        }, 1800);
      }).catch(function () {
        btn.textContent = msg('copyManual', 'Copy manually');
        window.setTimeout(function () { btn.textContent = original; }, 1800);
      });
    });
  });

  if (reduced) return;

  // Hero 光晕跟随鼠标：只更新 CSS 变量，绘制交给 .hero-spot 的渐变。
  var hero = document.querySelector('.hero');
  if (hero && fine) {
    var spotPending = false, sx = 0, sy = 0;
    var applySpot = function () {
      spotPending = false;
      var r = hero.getBoundingClientRect();
      hero.style.setProperty('--mx', (sx - r.left) + 'px');
      hero.style.setProperty('--my', (sy - r.top) + 'px');
    };
    hero.addEventListener('mousemove', function (e) {
      sx = e.clientX; sy = e.clientY;
      if (!spotPending) {
        spotPending = true;
        requestAnimationFrame(applySpot);
      }
    }, { passive: true });
  }

  // 产品示意窗口随鼠标轻微 3D 倾斜。
  var visual = document.querySelector('.hero-visual');
  var browser = document.querySelector('.hero-visual .browser');
  if (visual && browser && fine) {
    var tiltPending = false, tx = 0, ty = 0;
    var applyTilt = function () {
      tiltPending = false;
      browser.style.setProperty('--ry', (tx * 2.4).toFixed(2) + 'deg');
      browser.style.setProperty('--rx', (-ty * 2.4).toFixed(2) + 'deg');
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
})();
