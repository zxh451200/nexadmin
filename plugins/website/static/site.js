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

  // ---- 页面级绑定 ----
  function initSite() {
    i18n = readI18n();
    initContactForm();
    initChat();
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

  // ---- 在线客服挂件：实现见 chat-widget.js（NexChat.mount），官网只是宿主之一 ----
  function initChat() {
    var root = document.querySelector('[data-chat]');
    if (!root || !window.NexChat) return;
    window.NexChat.mount(root, { t: msg });
  }

  window.__siteInit = initSite;
  initSite();
})();
