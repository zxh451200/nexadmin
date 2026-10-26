// NexAdmin 官网默认模板脚本：滚动进场、导航状态、主题/语言切换与交互微效果。无外部依赖。
// 语言切换走 fetch 无刷新换页：整站脚本只注册一次文档级委托，页面级绑定集中在 initSite，
// 换页后重新执行 initSite 即可，不需要重新加载脚本。
(function () {
  'use strict';

  // ---- 模块级状态（跨换页存活） ----
  var i18n = {};
  var winHandlers = [];
  var swapped = false;

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

  // ---- 复制文本：优先 Clipboard API，HTTP 等非安全上下文退回 execCommand ----
  function copyText(text) {
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
    if (meta) meta.setAttribute('content', scheme === 'light' ? '#ffffff' : '#000000');
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

  // ---- 复制安装命令 / 复制邮箱（委托） ----
  document.addEventListener('click', function (e) {
    var copyBtn = e.target.closest('[data-copy]');
    if (copyBtn) {
      var src = document.querySelector(copyBtn.getAttribute('data-copy'));
      if (!src) return;
      var original = copyBtn.textContent;
      copyText(src.textContent.trim()).then(function () {
        copyBtn.textContent = msg('copied', 'Copied');
        copyBtn.classList.add('copied');
        window.setTimeout(function () {
          copyBtn.textContent = original;
          copyBtn.classList.remove('copied');
        }, 1800);
      }).catch(function () {
        copyBtn.textContent = msg('copyManual', 'Copy manually');
        window.setTimeout(function () { copyBtn.textContent = original; }, 1800);
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
  document.addEventListener('click', function (e) {
    document.querySelectorAll('details.lang-switch[open]').forEach(function (box) {
      if (!box.contains(e.target)) box.removeAttribute('open');
    });
  });
  document.addEventListener('keydown', function (e) {
    if (e.key !== 'Escape') return;
    document.querySelectorAll('details.lang-switch[open]').forEach(function (box) {
      box.removeAttribute('open');
    });
  });

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

  // ---- 语言切换：fetch 无刷新换页，失败回退整页跳转 ----
  function swapPage(url) {
    if (!window.fetch || !window.DOMParser || !(window.history && window.history.pushState)) {
      location.href = url;
      return;
    }
    var root = document.documentElement;
    if (root.getAttribute('data-swapping') === '1') return;
    root.setAttribute('data-swapping', '1');
    root.classList.add('lang-swapping');
    var scrollY = window.scrollY;
    fetch(url).then(function (res) {
      if (!res.ok) throw new Error('HTTP ' + res.status);
      return res.text();
    }).then(function (text) {
      var doc = new DOMParser().parseFromString(text, 'text/html');
      if (!doc.body || !doc.getElementById('main')) throw new Error('unexpected document');
      var lang = doc.documentElement.getAttribute('lang');
      var newHead = document.adoptNode(doc.head);
      var newBody = document.adoptNode(doc.body);
      document.head.parentNode.replaceChild(newHead, document.head);
      document.body.parentNode.replaceChild(newBody, document.body);
      if (lang) root.setAttribute('lang', lang);
      swapped = true;
      window.history.pushState({}, '', url);
      window.scrollTo(0, scrollY);
      initSite();
      root.removeAttribute('data-swapping');
      root.classList.remove('lang-swapping');
    }).catch(function () {
      location.href = url;
    });
  }
  // pushState 之后的浏览器前进/后退不做增量处理，直接整页加载目标状态。
  window.addEventListener('popstate', function () {
    if (swapped) location.reload();
  });
  document.addEventListener('click', function (e) {
    var a = e.target.closest('a[hreflang]');
    if (!a) return;
    if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    if (a.getAttribute('aria-current') === 'true') { e.preventDefault(); return; }
    var href = a.getAttribute('href') || '';
    if (href.charAt(0) !== '/') return;
    e.preventDefault();
    swapPage(href);
  });

  // ---- 页面级绑定：换页后整体重来一遍 ----
  function initSite() {
    i18n = readI18n();
    clearWinHandlers();

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
      document.addEventListener('keydown', function (e) {
        if (e.key === 'Escape' && !mnav.hidden) {
          setMenu(false);
          toggle.focus();
        }
      });
      onWin(window, 'resize', function () {
        if (window.innerWidth > 760 && !mnav.hidden) setMenu(false);
      }, { passive: true });
    }

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

    // 滚动视差：按元素在视口中的位置写入 --py（±12px），只动 transform；rAF 节流、视口外跳过。
    var pEls = Array.prototype.slice.call(document.querySelectorAll('[data-parallax]'));
    if (pEls.length) {
      var pTicking = false;
      var pUpdate = function () {
        pTicking = false;
        var vh = window.innerHeight;
        pEls.forEach(function (el) {
          var r = el.getBoundingClientRect();
          if (r.bottom < -120 || r.top > vh + 120) return;
          var p = (r.top + r.height / 2 - vh / 2) / (vh / 2 + r.height / 2);
          el.style.setProperty('--py', (p * 12).toFixed(1) + 'px');
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
  }

  window.__siteInit = initSite;
  initSite();
})();
