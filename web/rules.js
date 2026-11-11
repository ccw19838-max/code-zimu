"use strict";
(() => {
  const escape = (value) => String(value ?? "").replace(/[&<>"']/g, (character) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[character]);
  const items = (value) => Array.isArray(value) ? value : [];
  function templateURL(value) {
    return typeof value === "string" && /^\/templates\/[A-Za-z0-9_.-]+$/.test(value) ? value : "";
  }
  function view(data) {
    if (!data || !Array.isArray(data.sections) || !Array.isArray(data.workflow)) throw Error("团队规范格式不正确，请检查维护文件。");
    const sections = data.sections.filter((section) => /^[a-z0-9-]+$/.test(section.id || ""));
    return `<div class="gh-rules" id="gh-rules-page">
      <section class="gh-rules-intro"><div><h2>让项目能够被理解、复现和继续开发</h2><p>${escape(data.summary)}</p></div><span class="gh-rules-version">建议 v${escape(data.version)}</span></section>
      <p class="gh-rules-status" role="note"><strong>${escape(data.status_label || "建议规则，待团队采纳")}</strong> · 本页为协作约定。组织权限、分支保护和必需检查需要在 GitHub 中另行配置并验证。</p>
      <ol class="gh-rules-flow" aria-label="从任务到交付">${data.workflow.map((step, index) => `<li><span class="gh-rule-step">${index + 1}</span><h3>${escape(step.title)}</h3><p>${escape(step.description)}</p></li>`).join("")}</ol>
      <div class="gh-rules-body"><nav class="gh-rules-toc" aria-label="规范章节"><strong>规范目录</strong>${sections.map((section, index) => `<button type="button" data-rule-jump="rule-${escape(section.id)}"><span>${String(index + 1).padStart(2, "0")}</span>${escape(section.title)}</button>`).join("")}<button type="button" data-rule-jump="gh-rule-templates"><span>↓</span>项目模板</button></nav>
      <div class="gh-rules-sections">${sections.map((section, index) => `<section class="gh-rule-item" id="rule-${escape(section.id)}" data-rule-section="${escape(section.id)}"><div class="gh-rule-title"><span>${String(index + 1).padStart(2, "0")}</span><h3>${escape(section.title)}</h3></div><p>${escape(section.summary)}</p><ul>${items(section.items).map((item) => `<li>${escape(item)}</li>`).join("")}</ul></section>`).join("")}
      <section class="gh-rule-section" id="gh-rule-templates"><h2>项目模板</h2><p>复制后填写实际负责人、命令和路径，再交由项目维护者确认。</p><div class="gh-template-grid">${items(data.templates).filter((template) => templateURL(template.url)).map((template) => `<a class="gh-template-card" data-rule-template href="${escape(templateURL(template.url))}" download><strong>${escape(template.label)}</strong><p>${escape(template.description)}</p><span>下载模板 ↗</span></a>`).join("")}</div></section></div></div>
    </div>`;
  }
  function errorView(message) {
    return `<section class="gh-empty"><h2>暂时无法读取团队规范</h2><p role="alert">${escape(message)}</p><button type="button" id="gh-rules-retry">重新加载规范</button></section>`;
  }
  document.addEventListener("click", (event) => {
    const target = event.target.closest("[data-rule-jump]");
    if (!target || !/^rule-[a-z0-9-]+$|^gh-rule-templates$/.test(target.dataset.ruleJump)) return;
    document.getElementById(target.dataset.ruleJump)?.scrollIntoView({ block: "start", behavior: matchMedia("(prefers-reduced-motion: reduce)").matches ? "instant" : "smooth" });
  });
  window.catalogRules = { view, errorView };
})();
