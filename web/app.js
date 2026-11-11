"use strict";
(() => {
  const $ = (selector) => document.querySelector(selector);
  let session = null, generation = 0, loadSequence = 0, authBusy = false, checkingSession = false, authOperation = 0;
  let selectedPage = location.hash === "#rules" ? "rules" : "projects";
  // This key belongs only to the standalone catalog origin. It contains no user data.
  const logoutKey = "github-catalog-session-ended-v1";
  let lastLogoutNotice = "", channel = null;
  try { channel = new BroadcastChannel("github-catalog-session-v1"); } catch {}
  const active = (epoch) => generation === epoch && session !== null;
  function navigationURL(value) {
    try { const u = new URL(value, location.origin); return ["http:", "https:"].includes(u.protocol) && !u.username && !u.password ? u.href : ""; } catch { return ""; }
  }
  function updateHeader() {
    const busy = authBusy || checkingSession;
    $("#catalog-admin-login").hidden = !!session?.authenticated;
    $("#catalog-admin-logout").hidden = !session?.authenticated;
    $("#catalog-admin-status").hidden = !session?.authenticated;
    $("#catalog-mode").hidden = session?.mode !== "demo";
    $("#catalog-admin-login").disabled = busy || !session;
    $("#catalog-admin-logout").disabled = busy;
    $("#catalog-reload").disabled = busy;
    const rules = selectedPage === "rules";
    $("#catalog-page-title").textContent = rules ? "团队规范" : "项目目录";
    $("#catalog-page-subtitle").textContent = rules ? "团队协作、项目交付与维护约定" : "了解项目用途、已有成果和二次开发入口";
    $("#catalog-page-art").src = rules ? "/icons/history.svg" : "/icons/project-folder.svg";
    $("#catalog-reload-label").textContent = rules ? "刷新规范" : "刷新目录";
    document.querySelectorAll("[data-catalog-page]").forEach((button) => {
      const selected = button.dataset.catalogPage === selectedPage;
      button.classList.toggle("selected", selected);
      if (selected) button.setAttribute("aria-current", "page");
      else button.removeAttribute("aria-current");
    });
    const navigation = navigationURL(session?.navigation_url || "");
    $("#catalog-return").hidden = !session?.navigation_url || !navigation;
    if (navigation) $("#catalog-return").href = navigation;
  }
  function replaceSession(value) {
    generation++;
    loadSequence++;
    session = value;
    window.githubCatalog.reset();
    $("#content").replaceChildren();
    updateHeader();
  }
  function announceLogout() {
    const id = Date.now().toString(36) + "-" + Math.random().toString(36).slice(2);
    lastLogoutNotice = id;
    try { localStorage.setItem(logoutKey, id); } catch {}
    try { channel?.postMessage({ type: "session-ended", id }); } catch {}
  }
  function endSession(announce = false) {
    authOperation++;
    authBusy = false;
    checkingSession = false;
    replaceSession({ ...session, authenticated: false, csrf: "" });
    $("#catalog-login-dialog").close();
    $("#catalog-password").value = "";
    $("#catalog-login-submit").disabled = false;
    $("#catalog-updated").textContent = "已退出目录管理";
    if (announce) announceLogout();
    void load();
  }
  function receiveLogout(id) {
    if (typeof id !== "string" || !id || id.length > 100 || id === lastLogoutNotice) return;
    lastLogoutNotice = id;
    endSession();
  }
  window.addEventListener("storage", (event) => { if (event.key === logoutKey) receiveLogout(event.newValue); });
  if (channel) channel.onmessage = (event) => { if (event.data?.type === "session-ended") receiveLogout(event.data.id); };
  async function api(path, method = "GET", data) {
    const epoch = generation;
    const response = await fetch(path, { method, headers: { "Content-Type": "application/json", "X-CSRF-Token": session?.csrf || "" }, ...(data ? { body: JSON.stringify(data) } : {}) });
    let body;
    try { body = await response.json(); } catch { throw Error("服务响应异常，请稍后重试。"); }
    if (generation !== epoch) throw Error("登录状态已变化，请重新操作。");
    if (!response.ok) {
      if (response.status === 401 && path !== "/api/login" && session?.authenticated) {
        endSession(true);
      }
      throw Error(body.error || "服务暂时不可用，请稍后重试。");
    }
    return body;
  }
  function actions(epoch) { return { request: api, reload: session ? load : start, isCurrent: () => active(epoch) && selectedPage === "projects" && !authBusy && !checkingSession }; }
  async function load() {
    if (!session || authBusy || checkingSession) return;
    const epoch = generation, sequence = ++loadSequence, revision = window.githubCatalog.generation(), page = selectedPage;
    $("#catalog-error").hidden = true;
    $("#catalog-updated").textContent = page === "rules" ? "正在读取规范" : "正在读取目录";
    try {
      if (page === "rules") {
        const rules = await api("/team-rules.json");
        if (!active(epoch) || sequence !== loadSequence || selectedPage !== page || authBusy || checkingSession) return;
        $("#content").innerHTML = window.catalogRules.view(rules);
        $("#catalog-updated").textContent = `协作约定 · v${rules.version}`;
        return;
      }
      let data = await api("/api/github/projects");
      if (!active(epoch) || sequence !== loadSequence || selectedPage !== page || authBusy || checkingSession) return;
      if (revision !== window.githubCatalog.generation()) { void load(); return; }
      // A different tab can change the shared cookie before this tab rechecks its session.
      if (!session.authenticated) data = { ...data, can_refresh: false, projects: data.mode === "demo" ? data.projects : (data.projects || []).filter((project) => project.visibility === "public") };
      $("#content").innerHTML = window.githubCatalog.view(data, actions(epoch));
      $("#catalog-updated").textContent = data.mode === "demo" ? "示例目录 · 自由浏览" : "目录已更新";
    } catch (error) {
      if (!active(epoch) || sequence !== loadSequence || selectedPage !== page || authBusy || checkingSession) return;
      $("#content").innerHTML = page === "rules" ? window.catalogRules.errorView(error.message) : window.githubCatalog.errorView(error.message, actions(epoch));
      $("#catalog-updated").textContent = page === "rules" ? "规范暂不可用" : "目录暂不可用";
    }
  }
  async function start() {
    if (authBusy || checkingSession) return;
    const wasAdmin = !!session?.authenticated;
    checkingSession = true;
    if (wasAdmin) replaceSession({ ...session, authenticated: false, csrf: "" });
    else { generation++; loadSequence++; }
    const epoch = generation;
    let confirmed = false;
    updateHeader();
    $("#catalog-updated").textContent = "正在确认访问权限";
    try {
      const value = await api("/api/session");
      if (epoch !== generation) return;
      if (wasAdmin && !value.authenticated) announceLogout();
      if (wasAdmin || !session || value.authenticated !== session.authenticated) {
        window.githubCatalog.reset();
        $("#content").replaceChildren();
      }
      session = value;
      confirmed = true;
    } catch (error) {
      if (epoch !== generation) return;
      $("#content").innerHTML = window.githubCatalog.errorView(error.message, { request: api, reload: start, isCurrent: () => epoch === generation });
      $("#catalog-updated").textContent = "连接暂不可用";
    } finally {
      if (epoch === generation) {
        checkingSession = false;
        updateHeader();
        if (confirmed) void load();
      }
    }
  }
  $("#catalog-reload").onclick = () => session ? load() : start();
  function selectPage(next) {
    if (next === selectedPage) return;
    selectedPage = next;
    loadSequence++;
    window.githubCatalog.reset({ preserveFilters: true });
    $("#content").replaceChildren();
    $("#catalog-error").hidden = true;
    updateHeader();
    window.scrollTo(0, 0);
    void load();
  }
  document.querySelectorAll("[data-catalog-page]").forEach((button) => {
    button.onclick = () => {
      const next = button.dataset.catalogPage;
      if (next !== "projects" && next !== "rules") return;
      location.hash = next;
      selectPage(next);
    };
  });
  window.addEventListener("hashchange", () => selectPage(location.hash === "#rules" ? "rules" : "projects"));
  document.addEventListener("click", (event) => {
    if (event.target.closest("#gh-rules-retry")) void load();
  });
  $("#catalog-admin-login").onclick = () => {
    $("#catalog-login-form").reset();
    $("#catalog-login-error").textContent = "";
    $("#catalog-login-dialog").showModal();
    $("#catalog-password").focus();
  };
  $("#catalog-login-close").onclick = () => $("#catalog-login-dialog").close();
  $("#catalog-login-dialog").addEventListener("close", () => { $("#catalog-password").value = ""; });
  $("#catalog-login-form").onsubmit = async (event) => {
    event.preventDefault();
    if (authBusy || checkingSession) return;
    const operation = ++authOperation;
    authBusy = true;
    updateHeader();
    $("#catalog-login-submit").disabled = true;
    $("#catalog-login-error").textContent = "";
    const epoch = generation;
    try {
      const value = await api("/api/login", "POST", { username: "admin", password: $("#catalog-password").value });
      if (epoch !== generation) return;
      replaceSession(value);
      $("#catalog-login-dialog").close();
      authBusy = false;
      updateHeader();
      void load();
    } catch (error) {
      if (epoch === generation) $("#catalog-login-error").textContent = error.message;
    } finally {
      if (operation === authOperation) {
        authBusy = false;
        $("#catalog-password").value = "";
        $("#catalog-login-submit").disabled = false;
        updateHeader();
      }
    }
  };
  $("#catalog-admin-logout").onclick = async () => {
    if (!session?.authenticated || authBusy || checkingSession) return;
    const operation = ++authOperation;
    authBusy = true;
    generation++;
    loadSequence++;
    window.githubCatalog.reset();
    $("#content").replaceChildren();
    $("#catalog-updated").textContent = "正在退出管理";
    updateHeader();
    const epoch = generation;
    try {
      const value = await api("/api/logout", "POST", {});
      if (epoch !== generation) return;
      replaceSession({ ...session, ...value, authenticated: false, csrf: "" });
      announceLogout();
    } catch (error) {
      if (epoch === generation) {
        $("#catalog-error").textContent = "退出管理未完成：" + error.message;
        $("#catalog-error").hidden = false;
        $("#catalog-updated").textContent = "请重试退出管理";
      }
    } finally {
      if (operation === authOperation) {
        authBusy = false;
        updateHeader();
        if (session && !session.authenticated && !$("#content").firstChild) void load();
      }
    }
  };
  window.addEventListener("focus", () => { void start(); });
  window.addEventListener("pageshow", () => { void start(); });
  document.addEventListener("visibilitychange", () => { if (document.visibilityState === "visible") void start(); });
  setInterval(() => { if (document.visibilityState === "visible" && session?.authenticated) void start(); }, 60000);
  updateHeader();
  void start();
})();
