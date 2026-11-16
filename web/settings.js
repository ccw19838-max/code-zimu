"use strict";
window.catalogSettings = (() => {
  const escape = value => String(value ?? "").replace(/[&<>"']/g, c => ({"&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;"}[c]));
  const updated = value => {
    const date = new Date(value);
    return Number.isNaN(date.valueOf()) ? "尚未更新" : date.toLocaleString("zh-CN", { hour12: false });
  };
  function view(data, session) {
    const rows = data.projects || [], demo = data.mode === "demo", workbench = session.auth_mode === "workbench";
    const missing = rows.filter(p => !p.profile?.summary || !p.profile?.maintainer || !p.profile?.development?.entry_points?.length);
    return `<div class="catalog-settings" id="catalog-settings">
      <section class="catalog-settings-section"><h2>目录连接与更新</h2>
        <dl><dt>运行模式</dt><dd>${demo ? "演示目录" : data.mode === "live" ? "GitHub 组织目录" : "尚未连接组织"}</dd><dt>组织</dt><dd>${escape(data.organization || "未配置")}</dd><dt>最近更新</dt><dd>${escape(updated(data.updated_at))}</dd><dt>可见项目</dt><dd>${rows.length} 个 · 其中私有 ${rows.filter(p => p.visibility === "private").length} 个</dd></dl>
        <p>${demo ? "当前为示例项目，刷新不会访问 GitHub。" : "目录按需读取，缓存有效期为 10 分钟。更新会重新检查组织仓库。"}</p>
        ${data.can_refresh ? `<button type="button" class="primary" id="catalog-settings-refresh">${demo ? "刷新演示目录" : "从 GitHub 更新"}</button>` : "<p>请由部署管理员完成组织连接。</p>"}
        <p id="catalog-settings-error" role="alert" hidden></p>
      </section>
      <section class="catalog-settings-section"><h2>账号与访问</h2>
        <dl><dt>当前账号</dt><dd>${escape(session.name || "目录管理员")}</dd><dt>目录角色</dt><dd>管理员</dd><dt>登录方式</dt><dd>${workbench ? "工作台统一登录" : "独立目录登录"}</dd></dl>
        <p>${workbench ? "账号、密码和应用授权在工作台统一维护。" : "管理员密码由部署环境维护，更新后重启目录服务生效。"} GitHub 仓库访问权限仍由 GitHub 验证。</p>
        <div class="catalog-settings-actions">${workbench ? '<button type="button" id="catalog-settings-workbench">前往工作台</button>' : ""}<button type="button" id="catalog-settings-logout">${workbench ? "全局退出" : "退出管理"}</button></div>
      </section>
      <section class="catalog-settings-section"><h2>项目资料维护</h2><p>检查功能说明、维护人及代码入口；当前 ${missing.length} 个项目待完善。</p>
        ${missing.length ? `<ul class="catalog-settings-list">${missing.map(p => `<li>${escape(p.full_name)}：${[!p.profile?.summary && "功能说明", !p.profile?.maintainer && "维护人", !p.profile?.development?.entry_points?.length && "代码入口"].filter(Boolean).join("、")}</li>`).join("")}</ul>` : "<p>当前可见项目的基础说明均已填写。</p>"}
        <p>项目资料由维护者确认后更新目录配置；GitHub 更新只同步仓库基础信息。</p><div class="catalog-settings-actions"><a href="#projects">查看项目资料</a></div>
      </section>
      <section class="catalog-settings-section"><h2>团队协作</h2><p>统一查看协作规范，下载项目说明、贡献指南和发布检查模板。</p><div class="catalog-settings-actions"><a href="#rules">规范与模板</a><a href="/templates/RELEASE_CHECKLIST.md" download>下载发布检查表</a></div></section>
    </div>`;
  }
  function errorView(message) {
    return `<section class="catalog-settings-section"><h2>目录状态暂不可用</h2><p role="alert">${escape(message)}</p><button type="button" id="catalog-settings-retry">重新检查</button></section>`;
  }
  return { view, errorView };
})();
