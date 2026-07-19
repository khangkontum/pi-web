/* pi-web client: renders pi sessions from the snapshot + event stream.
   All state lives server-side (pi's session files); this file only draws.
   No frameworks, no build step. */
"use strict";

(function () {
  var state = {
    sessionId: null,
    source: null,          // EventSource
    streaming: false,
    toolOutputs: {},       // toolCallId -> <pre> element for live tool output
    streamingMsgEl: null,  // container for the in-flight assistant message
  };

  var $ = function (id) { return document.getElementById(id); };
  var feed = $("feed");
  var input = $("input");

  // ---------- helpers ----------

  function el(tag, className, text) {
    var node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text;
    return node;
  }

  function fmtTime(ms) {
    if (!ms) return "";
    var d = new Date(ms);
    return d.toLocaleTimeString([], { hour12: false });
  }

  function fmtAgo(iso) {
    var t = new Date(iso).getTime();
    if (!t) return "";
    var s = Math.max(0, (Date.now() - t) / 1000);
    if (s < 60) return "just now";
    if (s < 3600) return Math.floor(s / 60) + "m ago";
    if (s < 86400) return Math.floor(s / 3600) + "h ago";
    return Math.floor(s / 86400) + "d ago";
  }

  function fmtTokens(n) {
    if (n === null || n === undefined) return "—";
    if (n >= 1000) return (n / 1000).toFixed(1) + "K";
    return String(n);
  }

  function api(path, opts) {
    return fetch(path, opts).then(function (resp) {
      if (!resp.ok) {
        return resp.json().catch(function () { return {}; }).then(function (body) {
          throw new Error(body.error || (path + ": HTTP " + resp.status));
        });
      }
      return resp.json();
    });
  }

  function scrollFeed() {
    feed.scrollTop = feed.scrollHeight;
  }

  function nearBottom() {
    return feed.scrollHeight - feed.scrollTop - feed.clientHeight < 160;
  }

  // ---------- session rail ----------

  function refreshSessions() {
    return api("/api/sessions").then(function (data) {
      var list = $("session-list");
      list.textContent = "";
      (data.sessions || []).forEach(function (s) {
        var btn = el("button", "sess" + (s.id === state.sessionId ? " active" : ""));
        btn.appendChild(el("div", "t", s.title));
        var meta = el("div", "m", (s.live ? "● live · " : "") + fmtAgo(s.updatedAt));
        btn.appendChild(meta);
        btn.addEventListener("click", function () { openSession(s.id, s.title); });
        list.appendChild(btn);
      });
    }).catch(function () { /* rail refresh is best-effort */ });
  }

  function refreshGit() {
    api("/api/git").then(function (info) {
      var panel = $("repo-panel");
      if (!info.repo) { panel.hidden = true; return; }
      panel.hidden = false;
      $("repo-branch").textContent = "⎇ " + (info.branch || "?");
      $("repo-dirty").textContent = info.dirtyCount ? info.dirtyCount + " modified" : "clean";
      $("repo-graph").textContent = info.graph || "";
    }).catch(function () { /* best-effort */ });
  }

  function setStats(stats) {
    if (!stats) return;
    var model = $("stat-model");
    if (stats.tokens) {
      $("stat-tokens").textContent = "↑" + fmtTokens(stats.tokens.input) + " ↓" + fmtTokens(stats.tokens.output);
    }
    if (stats.contextUsage && stats.contextUsage.percent !== null && stats.contextUsage.percent !== undefined) {
      $("stat-context").textContent = Math.round(stats.contextUsage.percent) + "%";
    }
    if (model.textContent === "—" && stats.sessionId) {
      // model is set from state; stats keeps tokens fresh
    }
  }

  function setModelFromState(st) {
    if (st && st.model && st.model.id) $("stat-model").textContent = st.model.id;
  }

  function setStreaming(on) {
    state.streaming = on;
    $("working-chip").hidden = !on;
    $("stop").disabled = !on;
    $("stat-link").innerHTML = "";
    var lamp = el("span", "lamp " + (on ? "lamp-run" : "lamp-ok"));
    $("stat-link").appendChild(lamp);
    $("stat-link").appendChild(document.createTextNode(on ? "streaming" : "sse"));
  }

  // ---------- message rendering ----------

  function messageContainer(role, ts) {
    var kind = role === "user" ? "user" : "agent";
    var msg = el("article", "msg " + kind);
    var head = el("div", "msg-head stamp");
    head.appendChild(el("span", "who", role === "user" ? "operator" : "pi"));
    if (ts) head.appendChild(el("time", "", fmtTime(ts)));
    msg.appendChild(head);
    msg.appendChild(el("div", "msg-body"));
    return msg;
  }

  function contentText(content) {
    if (typeof content === "string") return content;
    if (!Array.isArray(content)) return "";
    return content.filter(function (b) { return b.type === "text"; })
      .map(function (b) { return b.text; }).join("\n");
  }

  function toolSummaryText(name, args) {
    args = args || {};
    if (args.command) return String(args.command);
    if (args.path) return String(args.path);
    if (args.file_path) return String(args.file_path);
    try { return JSON.stringify(args); } catch (e) { return ""; }
  }

  function toolPathOf(args) {
    args = args || {};
    return args.path || args.file_path || null;
  }

  function renderToolBlock(call) {
    var details = el("details", "tool" + (call.name === "write" || call.name === "edit" ? " writing" : ""));
    details.open = true;
    details.dataset.toolCallId = call.id;
    var summary = el("summary", "stamp");
    summary.appendChild(el("span", "tname", call.name));
    var cmd = el("span", "tcmd");
    var path = toolPathOf(call.arguments);
    if (path) {
      var link = el("a", "", toolSummaryText(call.name, call.arguments));
      link.href = "#";
      link.addEventListener("click", function (ev) {
        ev.preventDefault();
        ev.stopPropagation();
        openFile(path);
      });
      cmd.appendChild(link);
    } else {
      cmd.textContent = toolSummaryText(call.name, call.arguments);
    }
    summary.appendChild(cmd);
    summary.appendChild(el("span", "texit", "running…"));
    details.appendChild(summary);
    var pre = el("pre", "", "");
    details.appendChild(pre);
    state.toolOutputs[call.id] = details;
    return details;
  }

  function finishToolBlock(toolCallId, resultText, isError) {
    var details = state.toolOutputs[toolCallId];
    if (!details) return;
    var pre = details.querySelector("pre");
    if (resultText !== null && resultText !== undefined) pre.textContent = resultText;
    var exit = details.querySelector(".texit");
    exit.textContent = isError ? "error" : "done";
    exit.className = "texit" + (isError ? " err" : "");
    if (!isError) details.open = false;
  }

  function renderAssistantInto(msgEl, message, streaming) {
    var body = msgEl.querySelector(".msg-body");
    body.textContent = "";
    (message.content || []).forEach(function (block) {
      if (block.type === "text") {
        body.appendChild(el("div", "msg-text", block.text));
      } else if (block.type === "thinking") {
        var t = el("details", "thinking");
        t.appendChild(el("summary", "", "thinking"));
        var pre = el("pre", "", block.thinking || "");
        t.appendChild(pre);
        body.appendChild(t);
      } else if (block.type === "toolCall") {
        var existing = state.toolOutputs[block.id];
        body.appendChild(existing || renderToolBlock(block));
      }
    });
    if (streaming) {
      var last = body.lastChild;
      var caretHost = last && last.classList && last.classList.contains("msg-text") ? last : body.appendChild(el("div", "msg-text", ""));
      caretHost.appendChild(el("span", "caret"));
    }
    if (message.stopReason === "error" && message.errorMessage) {
      body.appendChild(el("div", "msg-error", message.errorMessage));
    }
  }

  function appendMessage(message) {
    if (!message || !message.role) return;
    switch (message.role) {
      case "user": {
        var m = messageContainer("user", message.timestamp);
        m.querySelector(".msg-body").appendChild(el("div", "msg-text", contentText(message.content)));
        feed.appendChild(m);
        break;
      }
      case "assistant": {
        var a = messageContainer("assistant", message.timestamp);
        feed.appendChild(a);
        renderAssistantInto(a, message, false);
        break;
      }
      case "toolResult": {
        finishToolBlock(message.toolCallId, contentText(message.content), message.isError);
        break;
      }
      case "bashExecution": {
        appendBashRow(message.command, message.output, message.exitCode);
        break;
      }
      default:
        break;
    }
  }

  function appendBashRow(command, output, exitCode) {
    var details = el("details", "tool exec");
    details.open = true;
    var summary = el("summary", "stamp");
    summary.appendChild(el("span", "tname", "! shell"));
    summary.appendChild(el("span", "tcmd", command));
    var exit = el("span", "texit" + (exitCode === 0 ? "" : " err"), "operator · exit " + exitCode);
    summary.appendChild(exit);
    details.appendChild(summary);
    details.appendChild(el("pre", "", output || ""));
    feed.appendChild(details);
  }

  function clearFeed() {
    feed.textContent = "";
    state.toolOutputs = {};
    state.streamingMsgEl = null;
  }

  // ---------- snapshot + live events ----------

  function applySnapshot(snap) {
    clearFeed();
    var st = snap.state || {};
    setModelFromState(st);
    setStreaming(!!st.isStreaming);
    if (st.sessionName) $("session-title").textContent = st.sessionName;
    $("session-chip").hidden = false;
    $("session-chip").textContent = (snap.id || "").slice(0, 8);
    var msgs = (snap.messages && snap.messages.messages) || [];
    msgs.forEach(appendMessage);
    setStats(snap.stats);
    scrollFeed();
  }

  function handlePiEvent(ev) {
    var stick = nearBottom();
    switch (ev.type) {
      case "agent_start":
        setStreaming(true);
        break;
      case "agent_settled":
        setStreaming(false);
        state.streamingMsgEl = null;
        refreshGit();
        refreshSessions();
        break;
      case "message_start":
        if (ev.message && ev.message.role === "assistant") {
          state.streamingMsgEl = messageContainer("assistant", ev.message.timestamp || Date.now());
          feed.appendChild(state.streamingMsgEl);
        } else if (ev.message && ev.message.role === "user") {
          appendMessage(ev.message);
        }
        break;
      case "message_update":
        if (ev.message && ev.message.role === "assistant") {
          if (!state.streamingMsgEl) {
            state.streamingMsgEl = messageContainer("assistant", Date.now());
            feed.appendChild(state.streamingMsgEl);
          }
          renderAssistantInto(state.streamingMsgEl, ev.message, true);
        }
        break;
      case "message_end":
        if (ev.message && ev.message.role === "assistant" && state.streamingMsgEl) {
          renderAssistantInto(state.streamingMsgEl, ev.message, false);
          if (ev.message.usage) {
            $("stat-tokens").textContent = "↑" + fmtTokens(ev.message.usage.input) + " ↓" + fmtTokens(ev.message.usage.output);
          }
          state.streamingMsgEl = null;
        } else if (ev.message && ev.message.role !== "assistant") {
          appendMessage(ev.message);
        }
        break;
      case "tool_execution_start":
        // Block already exists from the assistant message toolCall render;
        // create standalone if not (defensive).
        if (!state.toolOutputs[ev.toolCallId]) {
          feed.appendChild(renderToolBlock({ id: ev.toolCallId, name: ev.toolName, arguments: ev.args }));
        }
        break;
      case "tool_execution_update":
        if (ev.partialResult) {
          var details = state.toolOutputs[ev.toolCallId];
          if (details) details.querySelector("pre").textContent = contentText(ev.partialResult.content);
        }
        break;
      case "tool_execution_end":
        finishToolBlock(ev.toolCallId, ev.result ? contentText(ev.result.content) : null, ev.isError);
        break;
      case "piweb_bash":
        if (ev.result) appendBashRow(ev.command, ev.result.output, ev.result.exitCode);
        break;
      default:
        break;
    }
    if (stick) scrollFeed();
  }

  // ---------- session lifecycle ----------

  function openSession(id, title) {
    if (state.source) { state.source.close(); state.source = null; }
    state.sessionId = id;
    $("empty-state") && $("empty-state").remove();
    $("session-title").textContent = title || "session";
    clearFeed();

    var source = new EventSource("/api/sessions/" + encodeURIComponent(id) + "/events");
    state.source = source;
    source.addEventListener("snapshot", function (msg) {
      applySnapshot(JSON.parse(msg.data));
    });
    source.addEventListener("pi", function (msg) {
      handlePiEvent(JSON.parse(msg.data));
    });
    source.addEventListener("error", function (msg) {
      if (msg.data) {
        var err = el("div", "msg-error", "session error: " + msg.data);
        feed.appendChild(err);
      }
    });
    refreshSessions();
  }

  function createSession(message) {
    var body = {};
    if (message) body.message = message;
    return api("/api/sessions", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    }).then(function (resp) {
      openSession(resp.id, message ? message.slice(0, 60) : "new session");
      return refreshSessions();
    });
  }

  // ---------- composer ----------

  function send() {
    var text = input.value.trim();
    if (!text) return;
    input.value = "";
    autoGrow();

    if (text[0] === "!") {
      var command = text.slice(1).trim();
      if (!command) return;
      if (!state.sessionId) {
        // A shell command needs a session for context; create one first.
        createSession(null).then(function () { runBash(command); }).catch(showSendError);
        return;
      }
      runBash(command);
      return;
    }

    if (!state.sessionId) {
      createSession(text).catch(showSendError);
      return;
    }
    api("/api/sessions/" + encodeURIComponent(state.sessionId) + "/message", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ message: text }),
    }).catch(showSendError);
  }

  function runBash(command) {
    api("/api/sessions/" + encodeURIComponent(state.sessionId) + "/bash", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ command: command }),
    }).then(function () {
      refreshGit();
    }).catch(showSendError);
  }

  function showSendError(err) {
    var msg = el("div", "msg-error", String(err.message || err));
    feed.appendChild(msg);
    scrollFeed();
  }

  function autoGrow() {
    input.style.height = "auto";
    input.style.height = Math.min(input.scrollHeight, window.innerHeight * 0.4) + "px";
  }

  // ---------- file viewer ----------

  function openFile(path) {
    api("/api/file?path=" + encodeURIComponent(path)).then(function (view) {
      $("file-path").textContent = view.path + (view.truncated ? " (truncated)" : "");
      $("file-content").textContent = view.binary ? "(binary file, " + view.size + " bytes)" : view.content;
      $("file-overlay").hidden = false;
    }).catch(showSendError);
  }

  // ---------- wiring ----------

  $("send").addEventListener("click", send);
  $("stop").addEventListener("click", function () {
    if (!state.sessionId) return;
    api("/api/sessions/" + encodeURIComponent(state.sessionId) + "/abort", { method: "POST" }).catch(showSendError);
  });
  $("new-session").addEventListener("click", function () {
    if (state.source) { state.source.close(); state.source = null; }
    state.sessionId = null;
    clearFeed();
    $("session-title").textContent = "new session";
    $("session-chip").hidden = true;
    var hint = el("div", "empty");
    hint.appendChild(el("div", "empty-title", "New session"));
    hint.appendChild(el("div", "empty-sub", "Type a message below to start."));
    feed.appendChild(hint);
    input.focus();
  });
  $("file-close").addEventListener("click", function () { $("file-overlay").hidden = true; });
  $("file-overlay").addEventListener("click", function (ev) {
    if (ev.target === $("file-overlay")) $("file-overlay").hidden = true;
  });
  input.addEventListener("keydown", function (ev) {
    if (ev.key === "Enter" && !ev.shiftKey) {
      ev.preventDefault();
      send();
    }
  });
  input.addEventListener("input", autoGrow);

  // ---------- boot ----------

  api("/version").then(function (v) {
    $("workspace-label").textContent = "pi-web " + v.version;
  }).catch(function () {});
  refreshGit();
  refreshSessions().then(function () {
    var first = document.querySelector(".sess");
    if (first) first.click();
  });
  setInterval(refreshGit, 120000);
})();
