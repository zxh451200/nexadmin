/* SOLARIS 主题动效增强 —— 自包含，不改共享的 site.js。
   只做动效与阅读工具，不改布局、文案、内容结构。
   原则：内容在 HTML 里默认就是终值；即便本脚本不加载，页面照常可读。
   所有非必要动画在 prefers-reduced-motion 下关闭。 */
(function () {
  'use strict';

  var reduce = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  /* 1. 统计数字滚动：只改写数字所在的文本节点，保留 <em> 单位与 "+" 后缀。
     site.js 里的 count-up 监听 .stat-value（default 主题类），本主题用的是
     .sl-stat strong / .sl-sustainability-stats strong，此前从未接上。 */
  function countUp(el) {
    var textNode = null;
    for (var i = 0; i < el.childNodes.length; i++) {
      var n = el.childNodes[i];
      if (n.nodeType === 3 && /\d/.test(n.nodeValue)) { textNode = n; break; }
    }
    if (!textNode) return;
    var m = textNode.nodeValue.match(/^([\d.,]+)(.*)$/);
    if (!m) return;
    var target = parseFloat(m[1].replace(/,/g, ''));
    if (!isFinite(target)) return;
    var suffix = m[2];
    var decimals = (m[1].split('.')[1] || '').length;
    var grouped = m[1].indexOf(',') >= 0;
    var fmt = function (v) {
      var s = decimals ? v.toFixed(decimals) : String(Math.round(v));
      if (grouped) {
        s = Number(s).toLocaleString('en-US', { minimumFractionDigits: decimals, maximumFractionDigits: decimals });
      }
      return s + suffix;
    };
    var t0 = null;
    var step = function (ts) {
      if (t0 === null) t0 = ts;
      var p = Math.min((ts - t0) / 900, 1);
      var eased = 1 - Math.pow(1 - p, 3);
      textNode.nodeValue = fmt(target * eased);
      if (p < 1) requestAnimationFrame(step);
      else textNode.nodeValue = m[1] + suffix; // 精确收尾，避免浮点误差
    };
    requestAnimationFrame(step);
  }

  var stats = document.querySelectorAll('.sl-stat strong, .sl-sustainability-stats strong');
  if (stats.length && !reduce && 'IntersectionObserver' in window) {
    var statIO = new IntersectionObserver(function (entries) {
      entries.forEach(function (entry) {
        if (entry.isIntersecting) { statIO.unobserve(entry.target); countUp(entry.target); }
      });
    }, { threshold: 0.4 });
    stats.forEach(function (el) { statIO.observe(el); });
  }

  /* 2. 回到顶部（右下角客服挂件上方，避开重叠） */
  var toTop = document.createElement('button');
  toTop.type = 'button';
  toTop.className = 'sl-to-top';
  toTop.setAttribute('aria-label', '回到顶部');
  toTop.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 19V5M5 12l7-7 7 7"/></svg>';
  document.body.appendChild(toTop);
  toTop.addEventListener('click', function () {
    window.scrollTo({ top: 0, behavior: reduce ? 'auto' : 'smooth' });
  });

  var ticking = false;
  function onScroll() {
    if (ticking) return;
    ticking = true;
    requestAnimationFrame(function () {
      var y = window.pageYOffset || document.documentElement.scrollTop || 0;
      toTop.classList.toggle('is-on', y > 700);
      ticking = false;
    });
  }
  window.addEventListener('scroll', onScroll, { passive: true });
  onScroll();
})();
