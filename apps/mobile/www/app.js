// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Drives the MusubeeCore plugin (android/app/src/main/java/app/musubee/core)
// without a bundler: the native bridge that Capacitor injects into the web
// view exposes nativePromise and addListener. The real UI will use
// registerPlugin from @capacitor/core instead.
//
// Nothing here writes to the console: Capacitor forwards it to the system
// log, and message content must never reach a log.
"use strict";

const PLUGIN = "MusubeeCore";
const CONVERSATION_NAME = "Instant Echo";

const status = document.getElementById("status");
const loginButton = document.getElementById("login");
const composer = document.getElementById("composer");
const text = document.getElementById("text");
const sendButton = document.getElementById("send");
const log = document.getElementById("log");

let nextId = 0;
let loggedIn = false;
let conversationId = null;

function show(line) {
  const item = document.createElement("li");
  item.textContent = line;
  log.prepend(item);
}

// Commands and events are those of the core API (core/api/schema).
async function request(command, params) {
  const response = await window.Capacitor.nativePromise(PLUGIN, "call", {
    request: { id: ++nextId, command, params },
  });
  if (response.error) {
    throw new Error(`${command}: ${response.error.code}: ${response.error.message}`);
  }
  return response.result;
}

function describe(event) {
  const data = event.data;
  switch (event.type) {
    case "message.added":
      return `${data.message.from_me ? "Sent" : "Received"}: ${data.message.text}`;
    case "message.updated":
      return `Status: ${data.message.status}${data.message.error ? ` (${data.message.error})` : ""}`;
    case "account.updated":
      return `Account ${data.account.name}: ${data.account.state}`;
    case "conversation.updated":
      return `Conversation: ${data.conversation.name}`;
    default:
      return `Event: ${event.type}`;
  }
}

// The composer opens once the account is added and its conversation exists,
// in whichever order the login response and the event arrive.
function openComposer() {
  if (!loggedIn || !conversationId) {
    return;
  }
  status.textContent = `Logged in, conversation: ${CONVERSATION_NAME}`;
  text.disabled = false;
  sendButton.disabled = false;
}

window.Capacitor.addListener(PLUGIN, "event", (event) => {
  show(describe(event));
  if (event.type === "conversation.updated" && event.data.conversation.name === CONVERSATION_NAME) {
    conversationId = event.data.conversation.conversation_id;
    openComposer();
  }
});

loginButton.addEventListener("click", async () => {
  loginButton.disabled = true;
  status.textContent = "Logging in…";
  try {
    const step = await request("login.start", { network_id: "echo", flow_id: "username" });
    const done = await request("login.submit", {
      process_id: step.process_id,
      values: { [step.fields[0].field_id]: "alice" },
    });
    if (done.type !== "complete") {
      throw new Error(`unexpected login step ${done.type}`);
    }
    // The conversation may exist already, from an earlier start.
    const { conversations } = await request("conversations.list", { account_id: done.account_id });
    const conversation = conversations.find((c) => c.name === CONVERSATION_NAME);
    if (conversation) {
      conversationId = conversation.conversation_id;
    }
    loggedIn = true;
    status.textContent = "Logged in, waiting for the conversation…";
    openComposer();
  } catch (err) {
    status.textContent = `Login failed: ${err.message}`;
    loginButton.disabled = false;
  }
});

composer.addEventListener("submit", async (event) => {
  event.preventDefault();
  const body = text.value.trim();
  if (!body || !conversationId) {
    return;
  }
  text.value = "";
  try {
    await request("messages.send", { conversation_id: conversationId, text: body });
  } catch (err) {
    show(`Send failed: ${err.message}`);
  }
});
