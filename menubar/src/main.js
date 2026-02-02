// ---------------------------------------------------------------------------
// meept menubar -- frontend logic
//
// Communicates with the Tauri backend via `window.__TAURI__.core.invoke()`.
// Polls daemon status every 5 seconds, handles chat send/receive, and
// updates the connection indicator.
// ---------------------------------------------------------------------------

(function () {
  "use strict";

  // -----------------------------------------------------------------------
  // Tauri invoke helper
  // -----------------------------------------------------------------------

  /**
   * Invoke a Tauri command.  Works with Tauri 2.x's `withGlobalTauri` mode.
   * @param {string} cmd  - command name registered on the Rust side
   * @param {object} args - arguments to pass
   * @returns {Promise<any>}
   */
  function invoke(cmd, args) {
    if (
      window.__TAURI__ &&
      window.__TAURI__.core &&
      typeof window.__TAURI__.core.invoke === "function"
    ) {
      return window.__TAURI__.core.invoke(cmd, args || {});
    }
    return Promise.reject(new Error("Tauri API not available"));
  }

  // -----------------------------------------------------------------------
  // DOM references
  // -----------------------------------------------------------------------

  const statusDot = document.getElementById("status-dot");
  const statusText = document.getElementById("status-text");
  const daemonInfo = document.getElementById("daemon-info");
  const messagesInner = document.getElementById("messages-inner");
  const messagesContainer = document.getElementById("messages");
  const chatForm = document.getElementById("chat-form");
  const chatInput = document.getElementById("chat-input");
  const sendBtn = document.getElementById("send-btn");

  // -----------------------------------------------------------------------
  // State
  // -----------------------------------------------------------------------

  let connected = false;
  let sending = false;

  // -----------------------------------------------------------------------
  // UI helpers
  // -----------------------------------------------------------------------

  /**
   * Append a message bubble to the chat area.
   * @param {"user"|"bot"|"error"} type
   * @param {string} text
   */
  function addMessage(type, text) {
    const wrapper = document.createElement("div");
    wrapper.className = "message " + type;

    const label = document.createElement("div");
    label.className = "message-label";
    if (type === "user") {
      label.textContent = "you";
    } else if (type === "bot") {
      label.textContent = "meept";
    } else {
      label.textContent = "error";
    }

    const bubble = document.createElement("div");
    bubble.className = "message-bubble";
    bubble.textContent = text;

    wrapper.appendChild(label);
    wrapper.appendChild(bubble);
    messagesInner.appendChild(wrapper);

    scrollToBottom();
  }

  /**
   * Show or remove the typing indicator.
   * @param {boolean} show
   */
  function setTyping(show) {
    let el = document.getElementById("typing");
    if (show && !el) {
      el = document.createElement("div");
      el.id = "typing";
      el.className = "typing-indicator";
      el.innerHTML = "<span></span><span></span><span></span>";
      messagesInner.appendChild(el);
      scrollToBottom();
    } else if (!show && el) {
      el.remove();
    }
  }

  function scrollToBottom() {
    requestAnimationFrame(function () {
      messagesContainer.scrollTop = messagesContainer.scrollHeight;
    });
  }

  /**
   * Update the status indicator UI.
   * @param {boolean} isConnected
   * @param {string}  [infoText]
   */
  function updateStatus(isConnected, infoText) {
    connected = isConnected;

    statusDot.className = isConnected ? "connected" : "disconnected";
    statusText.textContent = isConnected ? "Connected" : "Disconnected";

    if (infoText) {
      daemonInfo.textContent = infoText;
    } else if (!isConnected) {
      daemonInfo.textContent = "";
    }

    chatInput.disabled = !isConnected;
    sendBtn.disabled = !isConnected || sending;

    if (!isConnected) {
      chatInput.placeholder = "Daemon not connected...";
    } else {
      chatInput.placeholder = "Send a message...";
    }
  }

  // -----------------------------------------------------------------------
  // Daemon polling
  // -----------------------------------------------------------------------

  async function pollStatus() {
    try {
      const isConnected = await invoke("get_daemon_connected");

      if (isConnected) {
        try {
          const statusJson = await invoke("get_status");
          const status = JSON.parse(statusJson);
          const uptime = status.uptime_seconds;
          let info = "";
          if (typeof uptime === "number") {
            const h = Math.floor(uptime / 3600);
            const m = Math.floor((uptime % 3600) / 60);
            info = "up " + h + "h " + m + "m";
          }
          updateStatus(true, info);
        } catch (_) {
          // Connected but status call failed -- still mark as connected.
          updateStatus(true, "");
        }
      } else {
        updateStatus(false);
      }
    } catch (_) {
      updateStatus(false);
    }
  }

  // -----------------------------------------------------------------------
  // Chat handling
  // -----------------------------------------------------------------------

  async function handleSend(e) {
    e.preventDefault();

    const text = chatInput.value.trim();
    if (!text || sending || !connected) {
      return;
    }

    // Display user message
    addMessage("user", text);
    chatInput.value = "";
    chatInput.focus();

    // Disable input while waiting
    sending = true;
    sendBtn.disabled = true;
    chatInput.disabled = true;
    setTyping(true);

    try {
      const reply = await invoke("send_chat", { message: text });
      setTyping(false);
      addMessage("bot", reply);
    } catch (err) {
      setTyping(false);
      const msg = err && err.message ? err.message : String(err);
      addMessage("error", msg);
    } finally {
      sending = false;
      sendBtn.disabled = !connected;
      chatInput.disabled = !connected;
      chatInput.focus();
    }
  }

  // -----------------------------------------------------------------------
  // Keyboard shortcut: Escape to close the window
  // -----------------------------------------------------------------------

  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape") {
      if (
        window.__TAURI__ &&
        window.__TAURI__.window &&
        typeof window.__TAURI__.window.getCurrentWindow === "function"
      ) {
        window.__TAURI__.window.getCurrentWindow().close();
      }
    }
  });

  // -----------------------------------------------------------------------
  // Init
  // -----------------------------------------------------------------------

  chatForm.addEventListener("submit", handleSend);

  // Initial poll, then every 5 seconds.
  pollStatus();
  setInterval(pollStatus, 5000);
})();
