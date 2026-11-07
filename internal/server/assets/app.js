(() => {
  "use strict";

  const state = {
    view: "dashboard",
    filter: "all",
    containers: [],
    images: [],
    networks: [],
    volumes: [],
    projects: [],
    composeFiles: [],
    system: null,
    history: { cpu: [], mem: [], labels: [] },
    activeContainer: null,
    drawerTab: "overview",
    logStream: null,
    term: null,
    termFit: null,
    termSocket: null,
    termResizeObserver: null,
    engine: "docker",
    sysStream: null,
  };

  const $ = (sel, root = document) => root.querySelector(sel);
  const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));

  const fmtBytes = (bytes, digits = 1) => {
    if (!bytes || bytes <= 0) return "0 B";
    const units = ["B", "KB", "MB", "GB", "TB", "PB"];
    const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
    const value = bytes / Math.pow(1024, i);
    return `${value.toFixed(i === 0 ? 0 : digits)} ${units[i]}`;
  };

  const fmtRate = (bps) => {
    if (!bps || bps < 1) return "0 B/s";
    return fmtBytes(bps) + "/s";
  };

  const fmtPercent = (v) => (v == null || isNaN(v) ? "0.0" : Number(v).toFixed(1));

  const fmtDuration = (seconds) => {
    if (!seconds || seconds <= 0) return "—";
    const d = Math.floor(seconds / 86400);
    const h = Math.floor((seconds % 86400) / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    if (d > 0) return `${d} 天 ${h} 小时`;
    if (h > 0) return `${h} 小时 ${m} 分钟`;
    return `${m} 分钟`;
  };

  const fmtTime = (unix) => {
    if (!unix) return "—";
    return new Date(unix * 1000).toLocaleString("zh-CN", { hour12: false });
  };

  const esc = (s) =>
    String(s == null ? "" : s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");

  const shortId = (id) => (id || "").slice(0, 12);

  async function api(path, options = {}) {
    const res = await fetch(path, {
      headers: { "Content-Type": "application/json" },
      ...options,
    });
    if (!res.ok) {
      let message = res.statusText;
      try {
        const body = await res.json();
        if (body && body.error) message = body.error;
      } catch (_) {}
      throw new Error(message);
    }
    if (res.status === 204) return null;
    const text = await res.text();
    return text ? JSON.parse(text) : null;
  }

  function toast(message, kind = "info") {
    const wrap = $("#toast-wrap");
    const el = document.createElement("div");
    el.className = `toast ${kind === "ok" ? "ok" : kind === "err" ? "err" : ""}`;
    el.textContent = message;
    wrap.appendChild(el);
    setTimeout(() => {
      el.style.opacity = "0";
      el.style.transition = "opacity 0.3s";
      setTimeout(() => el.remove(), 320);
    }, 3600);
  }

  function setButtonLoading(btn, loading) {
    if (!btn) return;
    if (loading) {
      btn.dataset.label = btn.innerHTML;
      btn.innerHTML = `<span class="spinner"></span>`;
      btn.disabled = true;
    } else {
      if (btn.dataset.label) btn.innerHTML = btn.dataset.label;
      btn.disabled = false;
    }
  }

  const viewMeta = {
    dashboard: ["仪表盘", "宿主机与 Docker 资源实时概览"],
    containers: ["容器", "创建、启停、查看日志与进入终端"],
    images: ["镜像", "拉取与清理本地镜像"],
    networks: ["网络", "管理 Docker 网络"],
    volumes: ["数据卷", "管理持久化数据卷"],
    compose: ["Compose", "通过 Web 表单部署 docker-compose 项目"],
  };

  function switchView(view) {
    state.view = view;
    $$(".nav-item").forEach((b) => b.classList.toggle("active", b.dataset.view === view));
    $$(".view").forEach((v) => v.classList.toggle("hidden", v.id !== `view-${view}`));
    const meta = viewMeta[view] || ["", ""];
    $("#view-title").textContent = meta[0];
    $("#view-sub").textContent = meta[1];
    if (view === "dashboard") loadSystem();
    if (view === "containers") loadContainers();
    if (view === "images") loadImages();
    if (view === "networks") loadNetworks();
    if (view === "volumes") loadVolumes();
    if (view === "compose") loadCompose();
  }

  async function boot() {
    try {
      const meta = await api("/api/meta");
      state.engine = meta.engine;
    } catch (_) {}
    updateEnginePill();
    await Promise.all([loadSystem(true), loadContainers(true), loadImages(true), loadNetworks(true), loadVolumes(true), loadCompose(true)]);
    startSystemStream();
  }

  function updateEnginePill() {
    const pill = $("#engine-pill");
    const text = $("#engine-text");
    pill.classList.remove("ok", "demo", "err");
    if (state.engine === "demo") {
      pill.classList.add("demo");
      text.textContent = "演示模式";
    } else if (state.engine === "docker") {
      pill.classList.add("ok");
      text.textContent = "Docker 已连接";
    } else {
      pill.classList.add("err");
      text.textContent = "未连接";
    }
  }

  /* ---------------- System / dashboard ---------------- */

  async function loadSystem(initial = false) {
    try {
      const data = await api("/api/system");
      state.system = data;
      state.engine = data.engine || state.engine;
      updateEnginePill();
      renderSystem(data.host, data.docker, initial);
    } catch (err) {
      $("#host-meta").textContent = "监控数据不可用";
      if (initial) toast(`系统信息加载失败: ${err.message}`, "err");
    }
  }

  function renderSystem(host, docker, initial) {
    if (!host) return;
    if (host.hostname) {
      $("#host-meta").textContent = `${host.hostname} · ${host.kernel || ""}`;
    }

    $("#cpu-value").textContent = fmtPercent(host.cpuPercent);
    $("#cpu-bar").style.width = `${Math.min(100, host.cpuPercent || 0)}%`;
    $("#cpu-foot").textContent = `${host.cpuCores || 0} 核 · 负载 ${fmtPercent(host.load1)} / ${fmtPercent(host.load5)} / ${fmtPercent(host.load15)}`;

    $("#mem-value").textContent = fmtPercent(host.memPercent);
    $("#mem-bar").style.width = `${Math.min(100, host.memPercent || 0)}%`;
    $("#mem-foot").textContent = `${fmtBytes(host.memUsed)} / ${fmtBytes(host.memTotal)}`;

    const rootDisk = (host.disks || []).find((d) => d.mountpoint === "/") || (host.disks || [])[0];
    const diskPct = rootDisk ? rootDisk.usedPercent : 0;
    $("#disk-value").textContent = fmtPercent(diskPct);
    $("#disk-bar").style.width = `${Math.min(100, diskPct || 0)}%`;
    $("#disk-foot").textContent = rootDisk ? `${fmtBytes(rootDisk.used)} / ${fmtBytes(rootDisk.total)}（${rootDisk.mountpoint}）` : "无数据";

    $("#net-value").textContent = `↓ ${fmtRate(host.netRxRate)}  ↑ ${fmtRate(host.netTxRate)}`;
    const activeNets = (host.networks || []).filter((n) => n.rxBytes > 0 || n.txBytes > 0);
    $("#net-foot").textContent = `${activeNets.length} 个活动接口 · 运行 ${fmtDuration(host.uptime)}`;

    const info = [
      ["主机名", host.hostname || "—"],
      ["系统", host.platform || "—"],
      ["内核", host.kernel || "—"],
      ["CPU 型号", host.cpuModel || "—"],
      ["CPU 核心", `${host.cpuCores || 0}`],
      ["运行时间", fmtDuration(host.uptime)],
      ["进程数", `${host.processes || 0}`],
      ["Swap", `${fmtBytes(host.swapUsed)} / ${fmtBytes(host.swapTotal)}`],
    ];
    if (docker && docker.kind) {
      info.push(["Docker 版本", docker.serverVersion || "—"]);
      info.push(["API 版本", docker.apiVersion || "—"]);
      info.push(["存储驱动", docker.driver || "—"]);
      info.push(["容器/镜像", `${docker.containers ?? 0} / ${docker.images ?? 0}`]);
    }
    $("#sys-info").innerHTML = info.map(([k, v]) => `<div class="k">${esc(k)}</div><div class="v">${esc(v)}</div>`).join("");

    const diskBody = $("#disk-table tbody");
    diskBody.innerHTML = (host.disks || [])
      .map(
        (d) => `<tr>
          <td>${esc(d.mountpoint)}</td>
          <td class="mono">${esc(d.fstype || "")}</td>
          <td>${fmtBytes(d.used)}</td>
          <td>${fmtBytes(d.total)}</td>
          <td><div class="bar"><i style="width:${Math.min(100, d.usedPercent)}%"></i></div><span class="hint">${fmtPercent(d.usedPercent)}%</span></td>
        </tr>`
      )
      .join("") || `<tr><td colspan="5" class="empty-cell">无数据</td></tr>`;

    const netBody = $("#net-table tbody");
    netBody.innerHTML = (host.networks || [])
      .map(
        (n) => `<tr>
          <td>${esc(n.name)}</td>
          <td>${fmtBytes(n.rxBytes)}</td>
          <td>${fmtBytes(n.txBytes)}</td>
          <td class="mono">↓${fmtRate(n.rxRate)} ↑${fmtRate(n.txRate)}</td>
        </tr>`
      )
      .join("") || `<tr><td colspan="4" class="empty-cell">无数据</td></tr>`;

    pushHistory(host);
    drawChart();
    renderContainerSummary();
  }

  function pushHistory(host) {
    const h = state.history;
    h.cpu.push(host.cpuPercent || 0);
    h.mem.push(host.memPercent || 0);
    h.labels.push(new Date().toLocaleTimeString("zh-CN", { hour12: false }));
    if (h.cpu.length > 60) {
      h.cpu.shift();
      h.mem.shift();
      h.labels.shift();
    }
  }

  function drawChart() {
    const canvas = $("#resource-chart");
    if (!canvas) return;
    const dpr = window.devicePixelRatio || 1;
    const rect = canvas.getBoundingClientRect();
    if (rect.width === 0) return;
    canvas.width = rect.width * dpr;
    canvas.height = 220 * dpr;
    const ctx = canvas.getContext("2d");
    ctx.scale(dpr, dpr);
    const w = rect.width;
    const h = 220;
    ctx.clearRect(0, 0, w, h);

    ctx.strokeStyle = "#1b2436";
    ctx.lineWidth = 1;
    for (let i = 0; i <= 4; i++) {
      const y = (h / 4) * i;
      ctx.beginPath();
      ctx.moveTo(0, y);
      ctx.lineTo(w, y);
      ctx.stroke();
    }

    const series = [
      { data: state.history.cpu, color: "#2f8cf5" },
      { data: state.history.mem, color: "#38d6c4" },
    ];
    const max = 100;
    for (const s of series) {
      if (s.data.length < 2) continue;
      ctx.beginPath();
      ctx.lineWidth = 2;
      ctx.strokeStyle = s.color;
      s.data.forEach((v, i) => {
        const x = (i / (s.data.length - 1)) * w;
        const y = h - (Math.min(max, v) / max) * (h - 8) - 4;
        i === 0 ? ctx.moveTo(x, y) : ctx.lineTo(x, y);
      });
      ctx.stroke();

      const grad = ctx.createLinearGradient(0, 0, 0, h);
      grad.addColorStop(0, s.color + "33");
      grad.addColorStop(1, s.color + "00");
      ctx.lineTo(w, h);
      ctx.lineTo(0, h);
      ctx.closePath();
      ctx.fillStyle = grad;
      ctx.fill();
    }
  }

  function renderContainerSummary() {
    const host = $("#container-summary");
    if (!host) return;
    const list = state.containers.slice(0, 8);
    if (!list.length) {
      host.innerHTML = `<div class="empty">暂无容器</div>`;
      return;
    }
    host.innerHTML = list
      .map(
        (c) => `<div class="sum-card" data-id="${esc(c.id)}">
          <div class="s-name"><span>${esc(c.name)}</span><span class="badge ${c.state === "running" ? "green" : "red"}">${esc(c.state)}</span></div>
          <div class="s-image">${esc(c.image)}</div>
          <div class="s-meta">${esc(c.status || "")}</div>
        </div>`
      )
      .join("");
    $$(".sum-card", host).forEach((el) => el.addEventListener("click", () => openDrawer(el.dataset.id)));
  }

  function startSystemStream() {
    if (state.sysStream) state.sysStream.close();
    const es = new EventSource("/api/system/stream");
    state.sysStream = es;
    es.addEventListener("stats", (ev) => {
      try {
        const host = JSON.parse(ev.data);
        renderSystem(host, state.system ? state.system.docker : null, false);
      } catch (_) {}
    });
    es.onerror = () => {};
  }

  /* ---------------- Containers ---------------- */

  async function loadContainers(silent = false) {
    try {
      const list = await api("/api/containers?all=true");
      state.containers = list;
      $("#badge-containers").textContent = list.length;
      renderContainers();
      renderContainerSummary();
    } catch (err) {
      if (!silent) toast(`容器加载失败: ${err.message}`, "err");
    }
  }

  function filteredContainers() {
    const q = ($("#container-search").value || "").toLowerCase().trim();
    return state.containers.filter((c) => {
      if (state.filter === "running" && c.state !== "running") return false;
      if (state.filter === "exited" && c.state === "running") return false;
      if (!q) return true;
      const ports = (c.ports || []).map((p) => `${p.publicPort}:${p.privatePort}`).join(" ");
      return `${c.name} ${c.image} ${ports}`.toLowerCase().includes(q);
    });
  }

  function renderContainers() {
    const body = $("#container-table tbody");
    const list = filteredContainers();
    $("#container-empty").classList.toggle("hidden", list.length > 0);
    body.innerHTML = list
      .map((c) => {
        const ports = (c.ports || [])
          .filter((p) => p.publicPort)
          .map((p) => `${p.publicPort}→${p.privatePort}`)
          .join(", ");
        const nets = (c.networks || []).join(", ");
        return `<tr data-id="${esc(c.id)}">
          <td><span class="state ${esc(c.state)}"><span class="dot"></span>${esc(c.state)}</span></td>
          <td><div>${esc(c.name)}</div><div class="mono">${esc(shortId(c.id))}</div></td>
          <td class="mono">${esc(c.image)}</td>
          <td class="mono">${esc(ports || "—")}</td>
          <td class="mono">${esc(nets || "—")}</td>
          <td class="hint">${esc(fmtTime(c.created))}</td>
          <td class="right"><div class="row-actions">${containerActions(c)}</div></td>
        </tr>`;
      })
      .join("");
    $$("#container-table tbody tr").forEach((tr) => {
      tr.addEventListener("click", (e) => {
        if (e.target.closest("button")) return;
        openDrawer(tr.dataset.id);
      });
    });
    $$("#container-table tbody [data-action]").forEach((btn) => {
      btn.addEventListener("click", (e) => {
        e.stopPropagation();
        const id = btn.closest("tr").dataset.id;
        containerAction(id, btn.dataset.action);
      });
    });
  }

  function containerActions(c) {
    const running = c.state === "running";
    const btns = [];
    if (running) {
      btns.push(`<button class="btn small ghost" data-action="stop">停止</button>`);
      btns.push(`<button class="btn small ghost" data-action="restart">重启</button>`);
      btns.push(`<button class="btn small ghost" data-action="logs">日志</button>`);
    } else {
      btns.push(`<button class="btn small primary" data-action="start">启动</button>`);
      btns.push(`<button class="btn small ghost" data-action="logs">日志</button>`);
    }
    btns.push(`<button class="btn small danger" data-action="remove">删除</button>`);
    return btns.join("");
  }

  async function containerAction(id, action) {
    const c = state.containers.find((x) => x.id === id);
    if (action === "logs") {
      openDrawer(id, "logs");
      return;
    }
    if (action === "remove") {
      if (!confirm(`确认删除容器 ${c ? c.name : id}?`)) return;
      try {
        await api(`/api/containers/${encodeURIComponent(id)}?force=true&volumes=false`, { method: "DELETE" });
        toast("容器已删除", "ok");
        loadContainers();
      } catch (err) {
        toast(`删除失败: ${err.message}`, "err");
      }
      return;
    }
    try {
      await api(`/api/containers/${encodeURIComponent(id)}/${action}`, { method: "POST" });
      toast(`操作成功: ${action}`, "ok");
      loadContainers();
      if (state.activeContainer === id) refreshDrawer();
    } catch (err) {
      toast(`${action} 失败: ${err.message}`, "err");
    }
  }

  /* ---------------- Images ---------------- */

  async function loadImages(silent = false) {
    try {
      const list = await api("/api/images");
      state.images = list;
      $("#badge-images").textContent = list.length;
      renderImages();
    } catch (err) {
      if (!silent) toast(`镜像加载失败: ${err.message}`, "err");
    }
  }

  function renderImages() {
    const q = ($("#image-search").value || "").toLowerCase().trim();
    const list = state.images.filter((img) => {
      if (!q) return true;
      return (img.repoTags || []).join(" ").toLowerCase().includes(q) || img.id.toLowerCase().includes(q);
    });
    $("#image-empty").classList.toggle("hidden", list.length > 0);
    $("#image-table tbody").innerHTML = list
      .map((img) => {
        const tags = (img.repoTags || []).filter(Boolean);
        const tagText = tags.length ? tags.join(", ") : "<none>";
        return `<tr data-id="${esc(img.id)}">
          <td class="mono">${esc(tagText)}</td>
          <td class="mono">${esc(shortId(img.id))}</td>
          <td>${fmtBytes(img.size)}</td>
          <td class="hint">${esc(fmtTime(img.created))}</td>
          <td class="right"><button class="btn small danger" data-remove-image="${esc(img.id)}">删除</button></td>
        </tr>`;
      })
      .join("");
    $$("#image-table [data-remove-image]").forEach((btn) => {
      btn.addEventListener("click", async () => {
        if (!confirm("确认删除该镜像?")) return;
        try {
          await api(`/api/images/${encodeURIComponent(btn.dataset.removeImage)}?force=true`, { method: "DELETE" });
          toast("镜像已删除", "ok");
          loadImages();
        } catch (err) {
          toast(`删除失败: ${err.message}`, "err");
        }
      });
    });
  }

  /* ---------------- Networks ---------------- */

  async function loadNetworks(silent = false) {
    try {
      const list = await api("/api/networks");
      state.networks = list;
      $("#network-table tbody").innerHTML = list
        .map(
          (n) => `<tr data-id="${esc(n.id)}">
            <td>${esc(n.name)}</td>
            <td class="mono">${esc(n.driver)}</td>
            <td class="hint">${esc(n.scope)}</td>
            <td class="mono">${esc((n.containers || []).join(", ") || "—")}</td>
            <td class="right"><button class="btn small danger" data-remove-network="${esc(n.id)}">删除</button></td>
          </tr>`
        )
        .join("") || `<tr><td colspan="5" class="empty-cell">暂无网络</td></tr>`;
      $$("#network-table [data-remove-network]").forEach((btn) => {
        btn.addEventListener("click", async () => {
          if (!confirm("确认删除该网络?")) return;
          try {
            await api(`/api/networks/${encodeURIComponent(btn.dataset.removeNetwork)}`, { method: "DELETE" });
            toast("网络已删除", "ok");
            loadNetworks();
          } catch (err) {
            toast(`删除失败: ${err.message}`, "err");
          }
        });
      });
    } catch (err) {
      if (!silent) toast(`网络加载失败: ${err.message}`, "err");
    }
  }

  /* ---------------- Volumes ---------------- */

  async function loadVolumes(silent = false) {
    try {
      const list = await api("/api/volumes");
      state.volumes = list;
      $("#volume-table tbody").innerHTML = list
        .map(
          (v) => `<tr data-name="${esc(v.name)}">
            <td>${esc(v.name)}</td>
            <td class="mono">${esc(v.driver)}</td>
            <td class="mono">${esc(v.mountpoint)}</td>
            <td>${v.inUse ? `<span class="badge green">使用中</span>` : `<span class="badge">空闲</span>`}</td>
            <td class="right"><button class="btn small danger" data-remove-volume="${esc(v.name)}">删除</button></td>
          </tr>`
        )
        .join("") || `<tr><td colspan="5" class="empty-cell">暂无数据卷</td></tr>`;
      $$("#volume-table [data-remove-volume]").forEach((btn) => {
        btn.addEventListener("click", async () => {
          if (!confirm(`确认删除数据卷 ${btn.dataset.removeVolume}?`)) return;
          try {
            await api(`/api/volumes/${encodeURIComponent(btn.dataset.removeVolume)}?force=true`, { method: "DELETE" });
            toast("数据卷已删除", "ok");
            loadVolumes();
          } catch (err) {
            toast(`删除失败: ${err.message}`, "err");
          }
        });
      });
    } catch (err) {
      if (!silent) toast(`数据卷加载失败: ${err.message}`, "err");
    }
  }

  /* ---------------- Compose ---------------- */

  const templates = {
    web: `services:
  web:
    image: nginx:alpine
    container_name: demo-web
    ports:
      - "8088:80"
    restart: unless-stopped
  cache:
    image: redis:7-alpine
    container_name: demo-cache
    restart: unless-stopped
networks:
  default:
volumes: {}
`,
    stack: `services:
  web:
    image: nginx:alpine
    ports:
      - "8090:80"
    depends_on:
      - api
    restart: unless-stopped
  api:
    image: redis:7-alpine
    environment:
      - APP_ENV=production
      - LOG_LEVEL=info
    depends_on:
      - db
    restart: unless-stopped
  db:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: app
      POSTGRES_USER: app
      POSTGRES_PASSWORD: secret
    volumes:
      - dbdata:/var/lib/postgresql/data
    restart: unless-stopped
volumes:
  dbdata:
`,
    monitoring: `services:
  grafana:
    image: grafana/grafana:11.2.0
    ports:
      - "3001:3000"
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin
    volumes:
      - grafanadata:/var/lib/grafana
    restart: unless-stopped
volumes:
  grafanadata:
`,
  };

  async function loadCompose(silent = false) {
    try {
      const data = await api("/api/compose/projects");
      state.projects = data.projects || [];
      state.composeFiles = data.files || [];
      renderProjects();
    } catch (err) {
      if (!silent) toast(`Compose 加载失败: ${err.message}`, "err");
    }
  }

  function renderProjects() {
    const host = $("#compose-projects");
    if (!state.projects.length) {
      host.innerHTML = `<div class="empty">暂无 Compose 项目</div>`;
      return;
    }
    host.innerHTML = state.projects
      .map(
        (p) => `<div class="project-card">
          <div class="p-head">
            <span class="p-name">${esc(p.name)}</span>
            <span class="badge ${p.status === "running" ? "green" : p.status === "partial" ? "yellow" : "red"}">${esc(p.status)}</span>
          </div>
          <div class="p-meta">${p.services} 个服务 · ${p.running}/${p.containers} 容器运行</div>
          <div class="p-actions">
            <button class="btn small ghost" data-compose="${esc(p.name)}" data-act="start">启动</button>
            <button class="btn small ghost" data-compose="${esc(p.name)}" data-act="stop">停止</button>
            <button class="btn small ghost" data-compose="${esc(p.name)}" data-act="restart">重启</button>
            <button class="btn small ghost" data-compose="${esc(p.name)}" data-act="up">重新部署</button>
            <button class="btn small danger" data-compose="${esc(p.name)}" data-act="down">删除</button>
          </div>
        </div>`
      )
      .join("");
    $$("#compose-projects [data-compose]").forEach((btn) => {
      btn.addEventListener("click", () => composeAction(btn.dataset.compose, btn.dataset.act, btn));
    });
  }

  async function composeAction(name, action, btn) {
    if (action === "down" && !confirm(`确认删除项目 ${name} 及其容器?`)) return;
    setButtonLoading(btn, true);
    try {
      await api(`/api/compose/${encodeURIComponent(name)}/${action}`, { method: "POST" });
      toast(`项目 ${name}: ${action} 完成`, "ok");
      await Promise.all([loadCompose(), loadContainers()]);
    } catch (err) {
      toast(`${action} 失败: ${err.message}`, "err");
    } finally {
      setButtonLoading(btn, false);
    }
  }

  async function deployCompose(btn) {
    const name = $("#compose-name").value.trim();
    const content = $("#compose-content").value.trim();
    if (!content) {
      toast("请填写 docker-compose.yml 内容", "err");
      return;
    }
    setButtonLoading(btn, true);
    try {
      const res = await api("/api/compose/up", { method: "POST", body: JSON.stringify({ name, content }) });
      toast(`项目 ${res.project} 部署成功，共 ${res.services} 个服务`, "ok");
      await Promise.all([loadCompose(), loadContainers()]);
    } catch (err) {
      toast(`部署失败: ${err.message}`, "err");
    } finally {
      setButtonLoading(btn, false);
    }
  }

  /* ---------------- Drawer ---------------- */

  async function openDrawer(id, tab = "overview") {
    state.activeContainer = id;
    state.drawerTab = tab;
    $("#drawer-backdrop").classList.remove("hidden");
    $("#container-drawer").classList.remove("hidden");
    switchDrawerTab(tab);
    await refreshDrawer();
  }

  function closeDrawer() {
    stopLogs();
    stopTerminal();
    $("#drawer-backdrop").classList.add("hidden");
    $("#container-drawer").classList.add("hidden");
    state.activeContainer = null;
  }

  function switchDrawerTab(tab) {
    state.drawerTab = tab;
    $$("#drawer-tabs button").forEach((b) => b.classList.toggle("active", b.dataset.tab === tab));
    $$(".drawer-pane").forEach((p) => p.classList.toggle("hidden", p.id !== `pane-${tab}`));
    if (tab === "logs") startLogs();
    if (tab === "overview") stopLogs();
    if (tab === "terminal") startTerminal();
    else stopTerminal();
  }

  async function refreshDrawer() {
    if (!state.activeContainer) return;
    try {
      const d = await api(`/api/containers/${encodeURIComponent(state.activeContainer)}`);
      $("#drawer-title").textContent = d.name || "容器详情";
      $("#drawer-sub").textContent = `${shortId(d.id)} · ${d.image || ""}`;
      const info = [
        ["状态", d.status || d.state || "—"],
        ["镜像", d.image || "—"],
        ["容器 ID", d.id],
        ["创建时间", d.createdAt || fmtTime(d.created)],
        ["启动时间", d.startedAt || "—"],
        ["重启次数", `${d.restartCount || 0}`],
        ["重启策略", d.restartPolicy || "no"],
        ["IP 地址", d.ipAddress || "—"],
        ["主机名", d.hostname || "—"],
        ["工作目录", d.workingDir || "—"],
        ["用户", d.user || "root"],
        ["启动命令", (d.cmd || []).join(" ") || "—"],
        ["入口点", (d.entrypoint || []).join(" ") || "—"],
        ["网络", (d.networks || []).join(", ") || "—"],
      ];
      $("#drawer-kv").innerHTML = info.map(([k, v]) => `<div class="k">${esc(k)}</div><div class="v">${esc(v)}</div>`).join("");
      $("#drawer-mounts").innerHTML = (d.mounts || [])
        .map((m) => `<span class="chip">${esc(m.source)} → ${esc(m.destination)} (${esc(m.type)}${m.rw ? ", rw" : ""})</span>`)
        .join("") || `<span class="hint">无挂载</span>`;
      $("#drawer-env").textContent = (d.env || []).join("\n") || "无环境变量";
      const actions = [];
      if (d.state === "running") {
        actions.push(`<button class="btn ghost" data-dact="stop">停止</button>`, `<button class="btn ghost" data-dact="restart">重启</button>`, `<button class="btn ghost" data-dact="pause">暂停</button>`);
      } else if (d.state === "paused") {
        actions.push(`<button class="btn primary" data-dact="unpause">恢复</button>`);
      } else {
        actions.push(`<button class="btn primary" data-dact="start">启动</button>`);
      }
      actions.push(`<button class="btn danger" data-dact="remove">删除</button>`);
      $("#drawer-actions").innerHTML = actions.join("");
      $$("#drawer-actions [data-dact]").forEach((btn) => {
        btn.addEventListener("click", () => containerAction(d.id, btn.dataset.dact));
      });
      loadDrawerStats(d.state);
    } catch (err) {
      toast(`获取容器信息失败: ${err.message}`, "err");
    }
  }

  async function loadDrawerStats(containerState) {
    if (!state.activeContainer) return;
    if (containerState && containerState !== "running") {
      $("#dc-cpu").textContent = "—";
      $("#dc-mem").textContent = "—";
      $("#dc-net").textContent = "—";
      $("#dc-pids").textContent = "—";
      return;
    }
    try {
      const s = await api(`/api/containers/${encodeURIComponent(state.activeContainer)}/stats`);
      $("#dc-cpu").textContent = `${fmtPercent(s.cpuPercent)}%`;
      $("#dc-mem").textContent = `${fmtBytes(s.memUsage)} / ${fmtBytes(s.memLimit)}`;
      $("#dc-net").textContent = `↓${fmtBytes(s.netRx)} ↑${fmtBytes(s.netTx)}`;
      $("#dc-pids").textContent = `${s.pids || 0}`;
    } catch (_) {}
  }

  function stopLogs() {
    if (state.logStream) {
      state.logStream.close();
      state.logStream = null;
    }
  }

  function startLogs() {
    stopLogs();
    if (!state.activeContainer) return;
    const tail = parseInt($("#log-tail").value || "200", 10);
    const timestamps = $("#log-timestamps").checked;
    const out = $("#log-output");
    out.textContent = "";
    $("#log-status").textContent = "连接中…";
    const es = new EventSource(
      `/api/containers/${encodeURIComponent(state.activeContainer)}/logs?tail=${tail}&timestamps=${timestamps}`
    );
    state.logStream = es;
    es.onopen = () => ($("#log-status").textContent = "已连接");
    es.onmessage = (ev) => {
      if (!ev.data) return;
      try {
        const line = JSON.parse(ev.data);
        out.textContent += line + "\n";
        if (out.textContent.length > 400000) out.textContent = out.textContent.slice(-200000);
        out.scrollTop = out.scrollHeight;
      } catch (_) {}
    };
    es.onerror = () => {
      $("#log-status").textContent = "连接已关闭";
      es.close();
      if (state.logStream === es) state.logStream = null;
    };
  }

  function stopTerminal() {
    if (state.termResizeObserver) {
      state.termResizeObserver.disconnect();
      state.termResizeObserver = null;
    }
    if (state.termSocket) {
      try { state.termSocket.close(); } catch (_) {}
      state.termSocket = null;
    }
    if (state.term) {
      try { state.term.dispose(); } catch (_) {}
      state.term = null;
      state.termFit = null;
    }
    const host = $("#terminal-host");
    if (host) host.innerHTML = "";
  }

  function startTerminal() {
    stopTerminal();
    if (!state.activeContainer) return;
    if (typeof Terminal === "undefined") {
      $("#terminal-host").innerHTML = `<div class="hint" style="padding:16px">终端组件加载失败</div>`;
      return;
    }
    const term = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
      theme: { background: "#060b13", foreground: "#cfd9e6", cursor: "#2f8cf5" },
      convertEol: true,
      scrollback: 5000,
    });
    const fit = typeof FitAddon !== "undefined" && FitAddon.FitAddon ? new FitAddon.FitAddon() : null;
    if (fit) term.loadAddon(fit);
    const hostEl = $("#terminal-host");
    term.open(hostEl);
    if (fit) {
      try { fit.fit(); } catch (_) {}
    }
    state.term = term;
    state.termFit = fit;

    const cols = term.cols || 100;
    const rows = term.rows || 30;
    const proto = location.protocol === "https:" ? "wss" : "ws";
    const ws = new WebSocket(`${proto}://${location.host}/api/containers/${encodeURIComponent(state.activeContainer)}/terminal?cols=${cols}&rows=${rows}`);
    ws.binaryType = "arraybuffer";
    state.termSocket = ws;

    const encoder = new TextEncoder();
    const decoder = new TextDecoder();

    ws.onopen = () => {
      term.focus();
      sendResize();
    };
    ws.onmessage = (ev) => {
      if (typeof ev.data === "string") term.write(ev.data);
      else term.write(new Uint8Array(ev.data));
    };
    ws.onclose = () => {
      term.write("\r\n\x1b[1;31m[连接已关闭]\x1b[0m\r\n");
    };
    ws.onerror = () => {
      term.write("\r\n\x1b[1;31m[终端连接失败]\x1b[0m\r\n");
    };
    term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) ws.send(encoder.encode(data));
    });

    function sendResize() {
      if (!fit) return;
      const c = term.cols;
      const r = term.rows;
      if (ws.readyState === WebSocket.OPEN) ws.send(`\x00resize:${c}x${r}`);
    }

    if (typeof ResizeObserver !== "undefined") {
      const ro = new ResizeObserver(() => {
        if (!fit) return;
        try {
          fit.fit();
          sendResize();
        } catch (_) {}
      });
      ro.observe(hostEl);
      state.termResizeObserver = ro;
    }
    setTimeout(() => {
      if (fit) {
        try { fit.fit(); sendResize(); } catch (_) {}
      }
      term.focus();
    }, 60);
    void decoder;
  }

  /* ---------------- Modals ---------------- */

  function openModal(title, bodyHTML, footHTML) {
    $("#modal-title").textContent = title;
    $("#modal-body").innerHTML = bodyHTML;
    $("#modal-foot").innerHTML = footHTML;
    $("#modal-backdrop").classList.remove("hidden");
  }

  function closeModal() {
    $("#modal-backdrop").classList.add("hidden");
  }

  function openCreateContainer() {
    const netOptions = ["bridge", "host", "none", ...state.networks.map((n) => n.name)];
    const body = `
      <div class="form-grid">
        <div class="field"><label>容器名称</label><input class="input" id="c-name" placeholder="my-app" /></div>
        <div class="field"><label>镜像 *</label><input class="input" id="c-image" placeholder="nginx:alpine" list="image-list" />
          <datalist id="image-list">${state.images.map((i) => `<option value="${esc((i.repoTags || [])[0] || "")}">`).join("")}</datalist>
        </div>
        <div class="field full"><label>启动命令</label><input class="input" id="c-command" placeholder="nginx -g 'daemon off;'" /></div>
        <div class="field full"><label>入口点</label><input class="input" id="c-entrypoint" placeholder="可选" /></div>
        <div class="field full"><label>端口映射（每行一个，如 8080:80）</label><textarea class="code" id="c-ports" style="min-height:70px" placeholder="8080:80\n8443:443"></textarea></div>
        <div class="field full"><label>数据卷（每行一个，如 myvol:/data）</label><textarea class="code" id="c-volumes" style="min-height:70px" placeholder="/srv/app:/app\nmyvol:/data"></textarea></div>
        <div class="field full"><label>环境变量（每行一个 KEY=VALUE）</label><textarea class="code" id="c-env" style="min-height:70px" placeholder="APP_ENV=production"></textarea></div>
        <div class="field"><label>网络</label><select class="input" id="c-network">${netOptions.map((n) => `<option value="${esc(n)}">${esc(n)}</option>`).join("")}</select></div>
        <div class="field"><label>重启策略</label><select class="input" id="c-restart">
          <option value="no">no</option><option value="unless-stopped">unless-stopped</option><option value="always">always</option><option value="on-failure">on-failure</option>
        </select></div>
        <div class="field"><label>内存限制 (MB)</label><input class="input" id="c-memory" type="number" placeholder="512" min="0" /></div>
        <div class="field"><label>CPU 限制（核）</label><input class="input" id="c-cpu" type="number" step="0.1" placeholder="1.0" min="0" /></div>
        <div class="field"><label>工作目录</label><input class="input" id="c-workdir" placeholder="/app" /></div>
        <div class="field"><label>用户</label><input class="input" id="c-user" placeholder="root" /></div>
        <div class="field"><label><input type="checkbox" id="c-tty" /> 分配 TTY（可交互）</label></div>
        <div class="field"><label><input type="checkbox" id="c-privileged" /> 特权模式</label></div>
      </div>
      <div class="form-note">镜像不存在时会自动拉取。端口、卷、环境变量均按行解析。</div>
    `;
    openModal(
      "新建容器",
      body,
      `<button class="btn ghost" id="modal-cancel">取消</button><button class="btn primary" id="modal-submit">创建并启动</button>`
    );
    $("#modal-cancel").addEventListener("click", closeModal);
    $("#modal-submit").addEventListener("click", async (e) => {
      const btn = e.currentTarget;
      const splitLines = (v) => v.split("\n").map((s) => s.trim()).filter(Boolean);
      const payload = {
        name: $("#c-name").value.trim(),
        image: $("#c-image").value.trim(),
        command: $("#c-command").value.trim(),
        entrypoint: $("#c-entrypoint").value.trim(),
        ports: splitLines($("#c-ports").value),
        volumes: splitLines($("#c-volumes").value),
        env: splitLines($("#c-env").value),
        networks: [$("#c-network").value],
        restartPolicy: $("#c-restart").value,
        memoryMB: parseInt($("#c-memory").value || "0", 10) || 0,
        cpuLimit: parseFloat($("#c-cpu").value || "0") || 0,
        workingDir: $("#c-workdir").value.trim(),
        user: $("#c-user").value.trim(),
        tty: $("#c-tty").checked,
        privileged: $("#c-privileged").checked,
      };
      if (!payload.image) {
        toast("请填写镜像名称", "err");
        return;
      }
      setButtonLoading(btn, true);
      try {
        await api("/api/containers", { method: "POST", body: JSON.stringify(payload) });
        toast("容器创建成功", "ok");
        closeModal();
        loadContainers();
      } catch (err) {
        toast(`创建失败: ${err.message}`, "err");
      } finally {
        setButtonLoading(btn, false);
      }
    });
  }

  function openPullImage() {
    openModal(
      "拉取镜像",
      `<div class="field"><label>镜像引用</label><input class="input" id="pull-ref" placeholder="nginx:alpine" /></div>
       <pre class="code-block" id="pull-log" style="margin-top:12px;display:none"></pre>`,
      `<button class="btn ghost" id="modal-cancel">取消</button><button class="btn primary" id="modal-submit">开始拉取</button>`
    );
    $("#modal-cancel").addEventListener("click", closeModal);
    $("#modal-submit").addEventListener("click", async (e) => {
      const ref = $("#pull-ref").value.trim();
      if (!ref) {
        toast("请填写镜像引用", "err");
        return;
      }
      const btn = e.currentTarget;
      setButtonLoading(btn, true);
      const logEl = $("#pull-log");
      logEl.style.display = "block";
      logEl.textContent = "";
      try {
        const res = await fetch("/api/images/pull", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ reference: ref }),
        });
        if (!res.ok || !res.body) throw new Error(res.statusText);
        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";
        let done = false;
        while (!done) {
          const { value, done: rd } = await reader.read();
          done = rd;
          if (!value) continue;
          buffer += decoder.decode(value, { stream: true });
          const parts = buffer.split("\n\n");
          buffer = parts.pop() || "";
          for (const part of parts) {
            const line = part.replace(/^data: /, "").trim();
            if (!line) continue;
            let text = line;
            try {
              text = JSON.parse(line);
            } catch (_) {}
            if (typeof text === "string") {
              if (text === "DONE") {
                done = true;
                toast("镜像拉取完成", "ok");
                break;
              }
              logEl.textContent += text + "\n";
              logEl.scrollTop = logEl.scrollHeight;
            }
          }
        }
        loadImages();
      } catch (err) {
        toast(`拉取失败: ${err.message}`, "err");
      } finally {
        setButtonLoading(btn, false);
      }
    });
  }

  function openCreateNetwork() {
    openModal(
      "新建网络",
      `<div class="form-grid">
        <div class="field"><label>名称 *</label><input class="input" id="n-name" placeholder="my-network" /></div>
        <div class="field"><label>驱动</label><select class="input" id="n-driver"><option>bridge</option><option>host</option><option>overlay</option><option>macvlan</option></select></div>
        <div class="field"><label><input type="checkbox" id="n-internal" /> 内部网络</label></div>
      </div>`,
      `<button class="btn ghost" id="modal-cancel">取消</button><button class="btn primary" id="modal-submit">创建</button>`
    );
    $("#modal-cancel").addEventListener("click", closeModal);
    $("#modal-submit").addEventListener("click", async (e) => {
      const name = $("#n-name").value.trim();
      if (!name) {
        toast("请填写网络名称", "err");
        return;
      }
      setButtonLoading(e.currentTarget, true);
      try {
        await api("/api/networks", {
          method: "POST",
          body: JSON.stringify({ name, driver: $("#n-driver").value, internal: $("#n-internal").checked }),
        });
        toast("网络已创建", "ok");
        closeModal();
        loadNetworks();
      } catch (err) {
        toast(`创建失败: ${err.message}`, "err");
      } finally {
        setButtonLoading(e.currentTarget, false);
      }
    });
  }

  function openCreateVolume() {
    openModal(
      "新建数据卷",
      `<div class="form-grid">
        <div class="field"><label>名称 *</label><input class="input" id="v-name" placeholder="my-volume" /></div>
        <div class="field"><label>驱动</label><input class="input" id="v-driver" placeholder="local" value="local" /></div>
      </div>`,
      `<button class="btn ghost" id="modal-cancel">取消</button><button class="btn primary" id="modal-submit">创建</button>`
    );
    $("#modal-cancel").addEventListener("click", closeModal);
    $("#modal-submit").addEventListener("click", async (e) => {
      const name = $("#v-name").value.trim();
      if (!name) {
        toast("请填写数据卷名称", "err");
        return;
      }
      setButtonLoading(e.currentTarget, true);
      try {
        await api("/api/volumes", { method: "POST", body: JSON.stringify({ name, driver: $("#v-driver").value }) });
        toast("数据卷已创建", "ok");
        closeModal();
        loadVolumes();
      } catch (err) {
        toast(`创建失败: ${err.message}`, "err");
      } finally {
        setButtonLoading(e.currentTarget, false);
      }
    });
  }

  function openComposeFilePicker() {
    const files = state.composeFiles;
    const body = files.length
      ? `<div class="project-list">${files
          .map(
            (f) => `<div class="project-card">
              <div class="p-head"><span class="p-name">${esc(f.name)}</span><span class="badge">${f.services || 0} 服务</span></div>
              <div class="p-actions">
                <button class="btn small ghost" data-open="${esc(f.name)}">打开</button>
                <button class="btn small danger" data-del="${esc(f.name)}">删除</button>
              </div>
            </div>`
          )
          .join("")}</div>`
      : `<div class="empty">暂无已保存文件</div>`;
    openModal("已保存的 Compose 文件", body, `<button class="btn ghost" id="modal-cancel">关闭</button>`);
    $("#modal-cancel").addEventListener("click", closeModal);
    $$("[data-open]").forEach((btn) =>
      btn.addEventListener("click", async () => {
        try {
          const data = await api(`/api/compose/files/${encodeURIComponent(btn.dataset.open)}`);
          $("#compose-content").value = data.content || "";
          $("#compose-name").value = (data.name || "").replace(/\.ya?ml$/, "");
          closeModal();
          toast("已加载文件", "ok");
        } catch (err) {
          toast(`加载失败: ${err.message}`, "err");
        }
      })
    );
    $$("[data-del]").forEach((btn) =>
      btn.addEventListener("click", async () => {
        if (!confirm(`删除文件 ${btn.dataset.del}?`)) return;
        try {
          await api(`/api/compose/files/${encodeURIComponent(btn.dataset.del)}`, { method: "DELETE" });
          toast("文件已删除", "ok");
          await loadCompose();
          closeModal();
        } catch (err) {
          toast(`删除失败: ${err.message}`, "err");
        }
      })
    );
  }

  /* ---------------- Events ---------------- */

  function bindEvents() {
    $$(".nav-item").forEach((btn) => btn.addEventListener("click", () => switchView(btn.dataset.view)));
    $("#goto-containers").addEventListener("click", () => switchView("containers"));
    $("#refresh-btn").addEventListener("click", () => {
      loadSystem();
      loadContainers(true);
      loadImages(true);
      loadNetworks(true);
      loadVolumes(true);
      loadCompose(true);
      toast("已刷新", "ok");
    });
    $("#quick-create").addEventListener("click", openCreateContainer);

    $("#container-search").addEventListener("input", renderContainers);
    $$("#container-filter button").forEach((btn) =>
      btn.addEventListener("click", () => {
        state.filter = btn.dataset.filter;
        $$("#container-filter button").forEach((b) => b.classList.toggle("active", b === btn));
        renderContainers();
      })
    );
    $("#image-search").addEventListener("input", renderImages);
    $("#pull-image-btn").addEventListener("click", openPullImage);
    $("#create-network-btn").addEventListener("click", openCreateNetwork);
    $("#create-volume-btn").addEventListener("click", openCreateVolume);

    $("#compose-refresh").addEventListener("click", () => loadCompose());
    $("#compose-template").addEventListener("change", (e) => {
      const t = templates[e.target.value];
      if (t) {
        $("#compose-content").value = t;
        if (!$("#compose-name").value) $("#compose-name").value = e.target.value;
      }
    });
    $("#compose-load-file").addEventListener("click", openComposeFilePicker);
    $("#compose-deploy").addEventListener("click", (e) => deployCompose(e.currentTarget));

    $("#drawer-close").addEventListener("click", closeDrawer);
    $("#drawer-backdrop").addEventListener("click", closeDrawer);
    $$("#drawer-tabs button").forEach((btn) => btn.addEventListener("click", () => switchDrawerTab(btn.dataset.tab)));
    $("#log-restart").addEventListener("click", startLogs);
    $("#log-timestamps").addEventListener("change", startLogs);

    $("#modal-close").addEventListener("click", closeModal);
    $("#modal-backdrop").addEventListener("click", (e) => {
      if (e.target === $("#modal-backdrop")) closeModal();
    });

    document.addEventListener("keydown", (e) => {
      if (e.key === "Escape") {
        if (!$("#modal-backdrop").classList.contains("hidden")) closeModal();
        else if (!$("#container-drawer").classList.contains("hidden")) closeDrawer();
      }
    });

    window.addEventListener("resize", () => {
      drawChart();
      if (state.termFit) {
        try { state.termFit.fit(); } catch (_) {}
      }
    });

    setInterval(() => {
      if (state.view === "containers" && $("#auto-refresh-containers").checked) {
        loadContainers(true);
      }
    }, 5000);

    setInterval(() => {
      if (state.activeContainer && state.drawerTab === "overview" && !$("#container-drawer").classList.contains("hidden")) {
        loadDrawerStats();
      }
    }, 4000);
  }

  document.addEventListener("DOMContentLoaded", () => {
    bindEvents();
    boot();
  });
})();
