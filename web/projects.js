/* GitHub catalog: discovery stays here; repository collaboration stays on GitHub. */
"use strict";
(() => {
  const find = (selector) => document.querySelector(selector);
  const escape = (value) => String(value ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
  const symbols = {
    repo: '<path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20M6.5 3H20v19H6.5A2.5 2.5 0 0 1 4 19.5v-14A2.5 2.5 0 0 1 6.5 3Z"/><path d="M8 7h8M8 11h5"/>',
    arrow: '<path d="M7 17 17 7M7 7h10v10"/>',
    search: '<circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 5 5"/>',
    branch: '<circle cx="6" cy="5" r="2"/><circle cx="6" cy="19" r="2"/><circle cx="18" cy="6" r="2"/><path d="M6 7v10M18 8a9 9 0 0 1-9 9H6"/>',
    star: '<path d="m12 3 2.8 5.7 6.3.9-4.6 4.5 1.1 6.3-5.6-3-5.6 3 1.1-6.3L3 9.6l6.2-.9Z"/>',
    lock: '<rect x="5" y="10" width="14" height="11" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3M12 14v3"/>',
    globe: '<circle cx="12" cy="12" r="9"/><ellipse cx="12" cy="12" rx="4" ry="9"/><path d="M3 12h18"/>',
    refresh: '<path d="M20 7v5h-5M4 17v-5h5M6 6a8 8 0 0 1 13 2l1 4M4 12l1 4a8 8 0 0 0 13 2"/>',
    check: '<path d="m5 12 4 4L19 6"/>',
    close: '<path d="m6 6 12 12M6 18 18 6"/>',
    people: '<circle cx="9" cy="8" r="3"/><path d="M3 21v-3a6 6 0 0 1 12 0v3M16 5a3 3 0 0 1 0 6M18 15a5 5 0 0 1 3 4v2"/>',
  };
  const svg = (name) => `<svg class="gh-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${symbols[name] || symbols.repo}</svg>`;
  const defaults = () => ({ search: "", language: "", visibility: "", archived: "", kind: "", sort: "updated" });
  let catalog = null, callbacks = null, filters = defaults(), generation = 0, refreshing = false, flowStage = 0;
  const projects = () => Array.isArray(catalog?.projects) ? catalog.projects : [];
  const current = () => !!catalog && !!callbacks?.isCurrent();
  function githubURL(value) {
    try {
      const u = new URL(value);
      return u.protocol === "https:" && u.hostname === "github.com" && !u.username && !u.password && !u.port ? u.href : "";
    } catch { return ""; }
  }
  function date(value, includeTime = false) {
    const d = new Date(value);
    return Number.isNaN(d.getTime()) ? "暂无记录" : d.toLocaleDateString("zh-CN", { year: "numeric", month: "short", day: "numeric", ...(includeTime ? { hour: "2-digit", minute: "2-digit" } : {}) });
  }
  function languageClass(language) {
    return ({ JavaScript: "javascript", TypeScript: "typescript", Python: "python", Go: "go", Vue: "vue", Rust: "rust", HTML: "html", CSS: "css", Shell: "shell" })[language] || "other";
  }
  function visibleProjects() {
    const query = filters.search.trim().toLocaleLowerCase();
    return projects().filter((p) => {
      const profile = p.profile || {};
      const haystack = [p.name, p.full_name, p.description, ...(p.topics || []), profile.title, profile.summary, ...(profile.scenarios || []), ...(profile.capabilities || []), ...(profile.outputs || []).flatMap((output) => [output.title, output.description]), ...(profile.development?.extension_points || [])].join(" ").toLocaleLowerCase();
      return (!query || haystack.includes(query)) && (!filters.language || p.language === filters.language) && (!filters.visibility || p.visibility === filters.visibility) && (!filters.archived || String(!!p.archived) === filters.archived) && (!filters.kind || profile.kind === filters.kind);
    }).sort((a, b) => {
      if (filters.sort === "name") return a.name.localeCompare(b.name, "zh-CN");
      if (filters.sort === "stars") return (b.stars || 0) - (a.stars || 0) || a.name.localeCompare(b.name);
      return (Date.parse(b.updated_at) || 0) - (Date.parse(a.updated_at) || 0) || a.name.localeCompare(b.name);
    });
  }
  function screenshotURL(value) {
    try {
      const u = new URL(value, location.origin);
      return u.origin === location.origin && /^\/images\/[a-z0-9_.-]+\.(png|jpe?g|webp)$/i.test(u.pathname) && !u.search && !u.hash ? u.pathname : "";
    } catch { return ""; }
  }
  function documentURL(value) {
    try {
      const u = new URL(value);
      return u.protocol === "https:" && u.host === "github.com" && !u.username && !u.password && !u.search && !u.hash && /^\/[A-Za-z0-9_.-]+\/[A-Za-z0-9_.\/-]+$/.test(u.pathname) ? u.href : "";
    } catch { return ""; }
  }
  const pending = (label = "说明") => `<p class="gh-pending">${label}待补充</p>`;
  const list = (values, label) => Array.isArray(values) && values.length ? `<ul>${values.map((value) => `<li>${escape(value)}</li>`).join("")}</ul>` : pending(label);
  function card(p) {
    const href = githubURL(p.html_url), isPrivate = p.visibility === "private";
    const profile = p.profile, title = profile?.title || p.name;
    const screenshot = profile?.screenshots?.find((item) => screenshotURL(item.src));
    const kind = profile?.kind === "team" ? "团队项目" : profile?.kind === "reference" ? "参考项目" : "说明待补";
    return `<article class="gh-card${p.archived ? " gh-card-archived" : ""}" data-gh-card data-name="${escape(p.name)}">
      <button type="button" class="gh-card-preview" data-gh-detail="${escape(p.full_name)}" aria-label="查看${escape(title)}的成果与开发说明">${screenshot ? `<img src="${escape(screenshotURL(screenshot.src))}" alt="${escape(screenshot.alt || title + "成果界面")}" loading="lazy">` : `<span class="gh-card-placeholder">${svg("repo")}<span>${profile?.kind === "reference" ? "开源参考 · 查看项目说明" : "成果截图待补充"}</span></span>`}<span class="gh-card-kind">${kind}</span></button>
      <div class="gh-card-body"><div class="gh-card-top"><p class="gh-owner">${escape(p.full_name)}</p><span class="gh-visibility${isPrivate ? " gh-private" : ""}">${svg(isPrivate ? "lock" : "globe")}${isPrivate ? "私有" : "公开"}</span>${p.archived ? '<span class="gh-archived">已归档</span>' : ""}</div>
      <h3><button type="button" data-gh-detail="${escape(p.full_name)}">${escape(title)}</button></h3>
      <p class="gh-description">${escape(profile?.summary || p.description || "项目用途与功能说明待补充。")}</p>
      <div class="gh-card-scenarios"><span>适用场景</span><p>${escape(profile?.scenarios?.slice(0, 2).join(" / ") || "待补充")}</p></div>
      <div class="gh-card-capabilities" aria-label="主要功能">${profile?.capabilities?.length ? profile.capabilities.slice(0, 3).map((capability) => `<span>${escape(capability)}</span>`).join("") : '<span class="gh-pending">功能说明待补充</span>'}</div>
      <div class="gh-card-outcome"><span>已有成果</span><p>${escape(profile?.outputs?.[0]?.title || "成果说明待补充")}</p></div>
      <div class="gh-card-details"><span class="gh-language"><i class="gh-dot gh-dot-${languageClass(p.language)}"></i>${escape(p.language || "未标注语言")}</span><span title="默认分支">${svg("branch")}<span>${escape(p.default_branch || "—")}</span></span><span aria-label="${escape(Number(p.stars || 0))} 个星标">${svg("star")}${Number(p.stars || 0).toLocaleString("zh-CN", { notation: "compact", maximumFractionDigits: 1 })}</span></div>
      </div><div class="gh-card-footer"><time datetime="${escape(p.updated_at)}">${escape(date(p.updated_at))}</time><button type="button" class="gh-detail-open" data-gh-detail="${escape(p.full_name)}">项目详情 ${svg("arrow")}</button>${href ? `<a data-gh-link class="gh-open" href="${escape(href)}" target="_blank" rel="noopener noreferrer" aria-label="在 GitHub 打开 ${escape(p.full_name)}（新窗口）">GitHub ${svg("arrow")}</a>` : '<span class="gh-unavailable">链接不可用</span>'}</div>
    </article>`;
  }
  function results() {
    const rows = visibleProjects();
    return rows.length ? rows.map(card).join("") : `<div class="gh-empty">${svg("search")}<h3>${projects().length ? "没有找到匹配的项目" : "目录里还没有项目"}</h3><p>${projects().length ? "试试其他关键词，或清除筛选再看看。" : "组织仓库更新后，就会在这里展示。"}</p>${projects().length ? '<button type="button" data-gh-action="reset">清除筛选</button>' : ""}</div>`;
  }
  function options(values, chosen, all) {
    return `<option value="">${all}</option>` + values.map(([value, text]) => `<option value="${escape(value)}"${chosen === value ? " selected" : ""}>${escape(text)}</option>`).join("");
  }
  function view(data, actions) {
    find("#gh-project-dialog")?.remove();
    catalog = data;
    callbacks = actions;
    const rows = projects(), demo = data.mode === "demo", disabled = data.mode === "disabled";
    const languages = [...new Set(rows.map((p) => p.language).filter(Boolean))].sort();
    if (filters.language && !languages.includes(filters.language)) filters.language = "";
    const orgURL = githubURL(data.organization_url);
    return `<div class="gh-catalog" data-mode="${escape(data.mode)}">
      ${disabled ? "" : `<div class="gh-stats" aria-label="目录概览"><div><span>可见项目</span><strong>${rows.length}</strong>${svg("repo")}</div><div><span>开发语言</span><strong>${languages.length}</strong>${svg("branch")}</div><div><span>团队项目</span><strong>${rows.filter((p) => p.profile?.kind === "team").length}</strong>${svg("people")}</div><div><span>参考项目</span><strong>${rows.filter((p) => p.profile?.kind === "reference").length}</strong>${svg("globe")}</div></div>`}
      <div class="gh-notice${data.stale ? " gh-notice-stale" : ""}" role="status"><span class="gh-notice-label">${demo ? "演示目录" : disabled ? "尚未连接" : data.stale ? "目录待更新" : "GitHub 目录"}</span><p>${demo ? "3 个团队项目与 3 个开源参考项目；组织尚未连接，仓库状态为示例。" : disabled ? "管理员连接 GitHub 组织后，项目会汇集到这里。" : escape(data.message || "展示当前可见项目，代码与协作记录请前往 GitHub。")}</p>${orgURL && !demo ? `<a href="${escape(orgURL)}" target="_blank" rel="noopener noreferrer">访问组织 ${svg("arrow")}</a>` : ""}<button type="button" id="gh-flow-open">加入组织流程 ${svg("arrow")}</button></div>
      ${disabled ? `<section class="gh-disabled">${svg("repo")}<h3>项目目录等待连接</h3><p>由管理员连接组织、整理项目说明并邀请团队成员。</p><div><span>01 创建组织</span><span>02 连接目录</span><span>03 邀请团队</span></div></section>` : `<section class="gh-directory" aria-labelledby="gh-directory-title"><div class="gh-directory-heading"><div><h2 id="gh-directory-title">全部项目 <span id="gh-count">${visibleProjects().length}</span></h2><p>按使用场景查找项目，查看成果与二次开发说明</p></div>${data.can_refresh ? `<button type="button" id="gh-refresh"${refreshing ? " disabled" : ""}>${svg("refresh")}${refreshing ? "正在更新…" : demo ? "刷新演示目录" : "从 GitHub 更新"}</button>` : '<span class="gh-cache-hint">项目访问权限由 GitHub 验证</span>'}</div>
      <div class="gh-controls"><label class="gh-search" for="gh-search">${svg("search")}<span class="gh-sr-only">搜索项目、场景、功能或开发方向</span><input type="search" id="gh-search" value="${escape(filters.search)}" placeholder="搜索项目、场景、功能或开发方向…" autocomplete="off"></label><div class="gh-filters"><label><span>项目类型</span><select id="gh-kind">${options([["team", "团队项目"], ["reference", "参考项目"]], filters.kind, "全部项目")}</select></label><label><span>语言</span><select id="gh-language">${options(languages.map((l) => [l, l]), filters.language, "所有语言")}</select></label><label><span>可见性</span><select id="gh-visibility">${options([["public", "公开"], ["private", "私有"]], filters.visibility, "全部可见性")}</select></label><label><span>状态</span><select id="gh-archived">${options([["false", "未归档"], ["true", "已归档"]], filters.archived, "全部状态")}</select></label><label><span>排序</span><select id="gh-sort">${[["updated", "最近更新"], ["name", "名称 A–Z"], ["stars", "星标最多"]].map(([value, label]) => `<option value="${value}"${filters.sort === value ? " selected" : ""}>${label}</option>`).join("")}</select></label></div></div>
      <div class="gh-results-line"><span id="gh-result-summary" role="status">显示 ${visibleProjects().length} / ${rows.length} 个项目</span><button type="button" class="gh-reset" data-gh-action="reset">清除筛选</button></div><p id="gh-refresh-error" role="alert" hidden></p><div class="gh-grid" id="gh-results">${results()}</div><div class="gh-directory-footer"><span>${svg("refresh")}${demo ? "示例目录" : "目录更新于"} · ${escape(date(data.updated_at, true))}</span><span>缺少的说明会标记为待补充</span></div></section>`}
    </div>`;
  }
  function updateResults() {
    if (!current() || !find("#gh-results")) return;
    find("#gh-results").innerHTML = results();
    find("#gh-count").textContent = visibleProjects().length;
    find("#gh-result-summary").textContent = `显示 ${visibleProjects().length} / ${projects().length} 个项目`;
  }
  function openDetail(fullName) {
    const p = projects().find((project) => project.full_name === fullName);
    if (!p || !current()) return;
    find("#gh-project-dialog")?.remove();
    const profile = p.profile || {}, development = profile.development || {}, href = githubURL(p.html_url);
    const screenshots = (profile.screenshots || []).filter((item) => screenshotURL(item.src));
    const docs = (profile.docs || []).filter((item) => documentURL(item.url));
    const section = (title, body, id = "") => `<section class="gh-detail-section"${id ? ` id="${id}"` : ""}><h3>${title}</h3>${body}</section>`;
    const commands = (values, label) => Array.isArray(values) && values.length ? `<pre class="gh-code-block"><code>${escape(values.join("\n"))}</code></pre>` : pending(label);
    const dialog = document.createElement("dialog");
    dialog.id = "gh-project-dialog";
    dialog.dataset.project = p.full_name;
    dialog.setAttribute("aria-labelledby", "gh-detail-title");
    dialog.innerHTML = `<header class="gh-detail-header"><div><p class="gh-owner">${escape(p.full_name)}</p><h2 id="gh-detail-title">${escape(profile.title || p.name)}</h2></div><button type="button" data-gh-action="close-detail" aria-label="关闭项目详情">${svg("close")}</button></header>
      <div class="gh-detail-content" id="gh-detail-scroll"><div class="gh-detail-badges"><span>${profile.kind === "team" ? "团队项目" : profile.kind === "reference" ? "参考项目" : "项目说明待补"}</span><span>${p.visibility === "private" ? "私有仓库" : "公开仓库"}</span><span>${escape(profile.maturity || "成熟度待补充")}</span></div><p class="gh-detail-summary">${escape(profile.summary || p.description || "该仓库的用途、成果和开发说明需要维护人补充确认。")}</p>${!p.profile ? '<p class="gh-detail-missing">项目说明待补充：当前仅有 GitHub 仓库信息，以下内容尚待维护人确认。</p>' : ""}
      <div class="gh-detail-shortcuts"><button type="button" data-gh-detail-jump="gh-detail-development">二次开发指南 ${svg("arrow")}</button>${href ? `<a href="${escape(href)}" target="_blank" rel="noopener noreferrer">GitHub 代码仓库 ${svg("arrow")}</a>` : ""}</div>
      ${section("项目成果", screenshots.length ? `<div class="gh-detail-gallery">${screenshots.map((item) => `<figure><img data-gh-screenshot src="${escape(screenshotURL(item.src))}" alt="${escape(item.alt || profile.title || p.name)}"><figcaption>${escape(item.caption || "成果界面")}</figcaption></figure>`).join("")}</div>` : pending(profile.kind === "reference" ? "参考项目成果截图" : "成果截图"), "gh-detail-results")}
      <div class="gh-detail-columns">${section("使用场景", list(profile.scenarios, "场景"))}${section("主要功能", list(profile.capabilities, "功能"), "gh-detail-capabilities")}</div>
      ${section("输出与结果", profile.outputs?.length ? `<div class="gh-output-list">${profile.outputs.map((item) => `<article class="gh-output-item"><h4>${escape(item.title)}</h4><p>${escape(item.description || "说明待补充")}</p></article>`).join("")}</div>` : pending("成果说明"), "gh-detail-outputs")}
      <div id="gh-detail-development"><div class="gh-detail-section-heading"><h2>二次开发指南</h2><p>从运行环境、代码入口到验证方式</p></div><div class="gh-detail-columns">${section("运行要求", list(development.requirements, "运行要求"))}${section("启动项目", commands(development.quick_start, "启动步骤"), "gh-detail-quick-start")}</div>
      ${section("代码入口", development.entry_points?.length ? `<div class="gh-entry-list">${development.entry_points.map((item) => `<article><code>${escape(item.path)}</code><p>${escape(item.description || "入口说明待补充")}</p></article>`).join("")}</div>` : pending("代码入口"), "gh-detail-entry-points")}
      <div class="gh-detail-columns">${section("可扩展方向", list(development.extension_points, "扩展方向"), "gh-detail-extension-points")}${section("验证方法", commands(development.validation, "验证方法"), "gh-detail-validation")}</div>${section("当前局限", list(development.limitations, "局限说明"), "gh-detail-limitations")}</div>
      ${section("项目文档", docs.length ? `<div class="gh-detail-links">${docs.map((item) => `<a href="${escape(documentURL(item.url))}" target="_blank" rel="noopener noreferrer">${escape(item.label || "项目文档")}${svg("arrow")}</a>`).join("")}</div>` : pending("文档链接"), "gh-detail-docs")}
      <div class="gh-detail-footer"><p><strong>维护人</strong><span>${escape(profile.maintainer || "待补充")}</span></p><p><strong>说明来源</strong><span>${escape(profile.source_note || "待补充")}</span></p><p><strong>使用许可</strong><span>${escape(profile.license_note || "待维护人确认，请查看仓库许可文件。")}</span></p></div></div>`;
    document.body.append(dialog);
    dialog.showModal();
  }
  function errorView(message, actions) {
    generation++;
    catalog = { mode: "error", projects: [] };
    callbacks = actions;
    filters = defaults();
    refreshing = false;
    flowStage = 0;
    find("#github-flow-dialog")?.remove();
    find("#gh-project-dialog")?.remove();
    return `<div class="gh-catalog" data-mode="error"><section class="gh-empty gh-load-error"><span class="gh-repo-icon">${svg("repo")}</span><h2>暂时无法读取项目目录</h2><p id="gh-catalog-error" role="alert">${escape(message)}</p><p>请重新加载，获取当前可访问的项目。</p><button type="button" id="gh-error-retry">${svg("refresh")}重新加载目录</button></section></div>`;
  }
  const steps = [
    ["确认 GitHub 身份", "用户主动前往 GitHub 授权，系统识别对应账号。授权不会直接改变组织成员身份。"],
    ["检查加入资格", "组织管理员或已配置的审批策略确认是否允许加入，并确定分配到哪个团队。"],
    ["发出组织邀请", "获组织授权的服务为该 GitHub 账号发送邀请。此时用户仍处于待加入状态。"],
    ["用户接受邀请", "用户在 GitHub 接受邀请；组织要求的身份验证等条件仍需满足。"],
    ["获得预设团队权限", "按组织的基础权限与 Team 配置授予访问范围；拥有令牌本身不等于拥有仓库权限。"],
  ];
  function renderFlow() {
    const dialog = find("#github-flow-dialog");
    dialog.dataset.stage = String(flowStage);
    dialog.innerHTML = `<div class="gh-flow-heading"><div><span class="gh-eyebrow">MEMBERSHIP PREVIEW</span><h2 id="gh-flow-title">加入团队，会经历什么？</h2></div><button type="button" data-gh-action="close-flow" aria-label="关闭加入组织流程">${svg("close")}</button></div><p class="gh-flow-warning">本地流程演示：不会连接 GitHub、生成令牌或发送邀请。</p><ol class="gh-flow-steps">${steps.map(([title, description], i) => `<li data-gh-stage="${i}" class="${i < flowStage ? "complete" : i === flowStage ? "current" : ""}"${i === flowStage ? ' aria-current="step"' : ""}><span class="gh-step-number">${i < flowStage ? svg("check") : i + 1}</span><div><h3>${title}${i < flowStage ? '<small>已演示</small>' : ""}</h3><p>${description}</p></div></li>`).join("")}</ol><div id="gh-flow-status" role="status">${flowStage === 5 ? "流程演示完成。实际接入后，邀请与权限以 GitHub 的处理结果为准。" : `当前演示：第 ${flowStage + 1} 步 / 共 5 步`}</div><div class="gh-flow-footer"><button type="button" id="gh-flow-reset">重新演示</button><button type="button" class="primary" id="gh-flow-next"${flowStage === 5 ? " disabled" : ""}>${["模拟授权确认", "模拟审核通过", "模拟发出邀请", "模拟接受邀请", "模拟授予团队权限", "演示完成"][flowStage]}${flowStage === 5 ? svg("check") : svg("arrow")}</button></div>`;
  }
  function openFlow() {
    let dialog = find("#github-flow-dialog");
    if (!dialog) {
      dialog = document.createElement("dialog");
      dialog.id = "github-flow-dialog";
      dialog.setAttribute("aria-labelledby", "gh-flow-title");
      document.body.append(dialog);
    }
    flowStage = 0;
    renderFlow();
    dialog.showModal();
  }
  async function refresh() {
    if (!current() || refreshing || !catalog.can_refresh) return;
    const epoch = generation, actions = callbacks;
    refreshing = true;
    const button = find("#gh-refresh");
    button.disabled = true;
    button.innerHTML = `${svg("refresh")}正在更新…`;
    find("#gh-refresh-error").hidden = true;
    try {
      await actions.request("/api/github/refresh", "POST", {});
      if (generation === epoch && actions.isCurrent()) {
        refreshing = false;
        await actions.reload();
      }
    } catch (err) {
      if (generation === epoch && actions.isCurrent()) {
        find("#content").innerHTML = errorView(err.message, actions);
      }
    } finally {
      if (generation === epoch) refreshing = false;
      if (generation === epoch && actions.isCurrent() && find("#gh-refresh")) {
        find("#gh-refresh").disabled = false;
        find("#gh-refresh").innerHTML = `${svg("refresh")}${catalog.mode === "demo" ? "刷新演示目录" : "从 GitHub 更新"}`;
      }
    }
  }
  document.addEventListener("input", (event) => {
    if (event.target.id === "gh-search" && current()) { filters.search = event.target.value; updateResults(); }
  });
  document.addEventListener("change", (event) => {
    const field = { "gh-language": "language", "gh-visibility": "visibility", "gh-archived": "archived", "gh-kind": "kind", "gh-sort": "sort" }[event.target.id];
    if (field && current()) { filters[field] = event.target.value; updateResults(); }
  });
  document.addEventListener("click", (event) => {
    const target = event.target.closest("button");
    if (!target || !current()) return;
    if (target.hasAttribute("data-gh-detail")) openDetail(target.dataset.ghDetail);
    if (target.dataset.ghAction === "close-detail") find("#gh-project-dialog")?.remove();
    if (target.dataset.ghDetailJump) find("#" + target.dataset.ghDetailJump)?.scrollIntoView({ block: "start" });
    if (target.id === "gh-flow-open") openFlow();
    if (target.id === "gh-refresh") refresh();
    if (target.id === "gh-error-retry") callbacks.reload();
    if (target.dataset.ghAction === "close-flow") find("#github-flow-dialog").close();
    if (target.dataset.ghAction === "reset") {
      filters = defaults();
      for (const id of ["search", "language", "visibility", "archived", "kind", "sort"]) find(`#gh-${id}`).value = filters[id];
      updateResults();
      find("#gh-search").focus();
    }
    if (target.id === "gh-flow-next" || target.id === "gh-flow-reset") {
      flowStage = target.id === "gh-flow-reset" ? 0 : Math.min(flowStage + 1, 5);
      renderFlow();
      find(flowStage === 5 ? "#gh-flow-reset" : "#gh-flow-next").focus();
    }
  });
  function reset({ preserveFilters = false } = {}) {
    generation++;
    catalog = null;
    callbacks = null;
    if (!preserveFilters) filters = defaults();
    refreshing = false;
    flowStage = 0;
    find("#github-flow-dialog")?.remove();
    find("#gh-project-dialog")?.remove();
  }
  window.githubCatalog = { view, errorView, reset, generation: () => generation };
})();
