<?xml version="1.0" encoding="UTF-8"?>
<!--
  feed.xsl —— /rss.xml 与 /atom.xml 的浏览器展示层。
  浏览器 2018 年起不再内置订阅预览，裸 XML 打开就是一屏标签；行业做法（Hugo/Astro 主题、
  18F 等）是给 feed 挂一张 XSL：浏览器渲染成「这是什么、怎么订阅」的说明页，阅读器照常
  解析 XML，互不影响。rss 与 atom 共用本表，按根元素分流。
-->
<xsl:stylesheet version="1.0"
  xmlns:xsl="http://www.w3.org/1999/XSL/Transform"
  xmlns:atom="http://www.w3.org/2005/Atom"
  exclude-result-prefixes="atom">
  <xsl:output method="html" version="5.0" encoding="utf-8" omit-xml-declaration="yes"/>

  <xsl:template match="/">
    <xsl:variable name="isAtom" select="boolean(/atom:feed)"/>
    <xsl:variable name="feedTitle">
      <xsl:choose>
        <xsl:when test="$isAtom"><xsl:value-of select="/atom:feed/atom:title"/></xsl:when>
        <xsl:otherwise><xsl:value-of select="/rss/channel/title"/></xsl:otherwise>
      </xsl:choose>
    </xsl:variable>
    <xsl:variable name="feedDesc">
      <xsl:choose>
        <xsl:when test="$isAtom"><xsl:value-of select="/atom:feed/atom:subtitle"/></xsl:when>
        <xsl:otherwise><xsl:value-of select="/rss/channel/description"/></xsl:otherwise>
      </xsl:choose>
    </xsl:variable>
    <xsl:variable name="itemCount">
      <xsl:choose>
        <xsl:when test="$isAtom"><xsl:value-of select="count(/atom:feed/atom:entry)"/></xsl:when>
        <xsl:otherwise><xsl:value-of select="count(/rss/channel/item)"/></xsl:otherwise>
      </xsl:choose>
    </xsl:variable>
    <xsl:variable name="buildDate">
      <xsl:choose>
        <xsl:when test="$isAtom"><xsl:value-of select="substring(/atom:feed/atom:updated,1,10)"/></xsl:when>
        <xsl:otherwise><xsl:value-of select="substring(/rss/channel/lastBuildDate,1,16)"/></xsl:otherwise>
      </xsl:choose>
    </xsl:variable>
    <html lang="zh-CN">
      <head>
        <meta charset="utf-8"/>
        <meta name="viewport" content="width=device-width, initial-scale=1"/>
        <meta name="robots" content="noindex"/>
        <title><xsl:value-of select="$feedTitle"/> · 订阅源</title>
        <style>
          :root {
            --bg: #08090a; --panel: #0d0e10; --border: rgba(255, 255, 255, 0.07);
            --text: #f7f8f8; --text-2: #d0d6e0; --text-3: #8a8f98; --accent: #5e6ad2;
          }
          * { box-sizing: border-box; }
          body {
            margin: 0; background: var(--bg); color: var(--text-2);
            font: 15px/1.7 -apple-system, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif;
            -webkit-font-smoothing: antialiased;
          }
          .wrap { max-width: 720px; margin: 0 auto; padding: 72px 20px 96px; }
          .eyebrow {
            margin: 0 0 10px; font-size: 11.5px; letter-spacing: 0.14em; text-transform: uppercase;
            color: var(--text-3);
          }
          h1 { margin: 0 0 10px; font-size: 34px; line-height: 1.15; letter-spacing: -0.02em; color: var(--text); font-weight: 500; }
          .desc { margin: 0; color: var(--text-3); }
          .howto {
            margin: 28px 0 0; padding: 18px 20px; border: 1px solid var(--border);
            border-radius: 10px; background: var(--panel);
          }
          .howto p { margin: 0; color: var(--text-2); }
          .howto .en { margin-top: 4px; font-size: 12.5px; color: var(--text-3); }
          .howto code {
            display: block; margin-top: 12px; padding: 9px 12px; border-radius: 6px;
            background: rgba(255, 255, 255, 0.04); border: 1px solid var(--border);
            font: 12.5px/1.5 ui-monospace, SFMono-Regular, Consolas, Menlo, monospace;
            color: var(--text-3); overflow-x: auto; white-space: nowrap;
          }
          .meta { margin: 18px 0 0; font-size: 12.5px; color: var(--text-3); }
          .items { list-style: none; margin: 40px 0 0; padding: 0; border-top: 1px solid var(--border); }
          .items li { padding: 20px 0; border-bottom: 1px solid var(--border); }
          .items a {
            color: var(--text); text-decoration: none; font-size: 16.5px; font-weight: 500;
            letter-spacing: -0.01em; line-height: 1.4;
          }
          .items a:hover { color: #ffffff; text-decoration: underline; text-underline-offset: 3px; }
          .items p { margin: 8px 0 0; color: var(--text-3); font-size: 14px; }
          time { display: block; margin-top: 8px; font-size: 12.5px; color: var(--text-3); font-variant-numeric: tabular-nums; }
          a.btn {
            display: inline-block; margin-top: 16px; padding: 7px 16px; border-radius: 999px;
            background: var(--accent); color: #ffffff; text-decoration: none; font-size: 13.5px;
          }
          footer { margin-top: 56px; font-size: 12px; color: var(--text-3); }
          @media (prefers-color-scheme: light) {
            :root {
              --bg: #f7f8f8; --panel: #ffffff; --border: rgba(8, 9, 10, 0.08);
              --text: #08090a; --text-2: #6b6f76; --text-3: #6f747d;
            }
            .howto code { background: rgba(8, 9, 10, 0.04); }
            .items a:hover { color: #000000; }
          }
        </style>
      </head>
      <body>
        <main class="wrap">
          <p class="eyebrow">订阅源 · Feed</p>
          <h1><xsl:value-of select="$feedTitle"/></h1>
          <xsl:if test="normalize-space($feedDesc) != ''">
            <p class="desc"><xsl:value-of select="$feedDesc"/></p>
          </xsl:if>
          <div class="howto">
            <p><strong style="color: var(--text)">这是一个 RSS/Atom 订阅源。</strong>把下面这串地址粘贴到你的阅读器（Feedly、Inoreader、NetNewsWire 等），新内容会自动推送给你。</p>
            <p class="en">This is an RSS/Atom feed — paste the URL below into your feed reader to subscribe.</p>
            <code id="feed-url">打开本页时浏览器地址栏里的地址</code>
            <script>
              document.getElementById('feed-url').textContent = document.location.href;
            </script>
          </div>
          <p class="meta">
            最近更新 <xsl:value-of select="$buildDate"/> · 共 <xsl:value-of select="$itemCount"/> 篇
            <xsl:if test="not(boolean($isAtom))"> · 语言 <xsl:value-of select="/rss/channel/language"/></xsl:if>
          </p>
          <ol class="items">
            <xsl:choose>
              <xsl:when test="$isAtom">
                <xsl:for-each select="/atom:feed/atom:entry">
                  <li>
                    <a href="{atom:link[@rel='self']/@href | atom:link[not(@rel)]/@href[1]}"><xsl:value-of select="atom:title"/></a>
                    <xsl:if test="normalize-space(atom:summary) != ''">
                      <p><xsl:value-of select="atom:summary"/></p>
                    </xsl:if>
                    <time><xsl:value-of select="substring(atom:updated,1,10)"/></time>
                  </li>
                </xsl:for-each>
              </xsl:when>
              <xsl:otherwise>
                <xsl:for-each select="/rss/channel/item">
                  <li>
                    <a href="{link}"><xsl:value-of select="title"/></a>
                    <xsl:if test="normalize-space(description) != ''">
                      <p><xsl:value-of select="description"/></p>
                    </xsl:if>
                    <time><xsl:value-of select="substring(pubDate,1,16)"/></time>
                  </li>
                </xsl:for-each>
              </xsl:otherwise>
            </xsl:choose>
          </ol>
          <footer>由 <xsl:value-of select="$feedTitle"/> 站点生成 · RSS 2.0 与 Atom 并行提供：/rss.xml · /atom.xml</footer>
        </main>
      </body>
    </html>
  </xsl:template>
</xsl:stylesheet>
