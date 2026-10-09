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

// Telegram (T1.6): logs in with whichever flow the core offers but the QR
// code, which this page cannot draw, for the manual tests on a real phone.
// Values typed here go to the core only.
const telegram = document.getElementById("telegram");
const tgStart = document.getElementById("tg-start");
const tgFlow = document.getElementById("tg-flow");
const tgStep = document.getElementById("tg-step");
const tgInstructions = document.getElementById("tg-instructions");
const tgFields = document.getElementById("tg-fields");
const tgLogout = document.getElementById("tg-logout");
const FIELD_INPUT_TYPES = { password: "password", token: "password", phone_number: "tel", email: "email" };

let tgProcess = null;
let tgAccount = null;

function showTelegram(account) {
  tgAccount = account;
  tgStart.hidden = account !== null;
  tgStep.hidden = true;
  tgLogout.hidden = account === null;
}

function showStep(step) {
  tgProcess = step.process_id;
  tgInstructions.textContent = step.instructions;
  tgFields.textContent = "";
  for (const field of step.fields ?? []) {
    const label = document.createElement("label");
    label.textContent = field.name;
    const input = document.createElement(field.type === "select" ? "select" : "input");
    input.name = field.field_id;
    input.required = true;
    if (field.type === "select") {
      for (const option of field.options ?? []) {
        input.append(new Option(option, option));
      }
    } else {
      input.type = FIELD_INPUT_TYPES[field.type] ?? "text";
      input.autocomplete = "off";
      input.value = field.default_value ?? "";
    }
    label.append(input);
    tgFields.append(label);
  }
  tgStart.hidden = true;
  tgStep.hidden = false;
  tgFields.querySelector("input, select")?.focus();
}

async function followStep(step) {
  while (step.type === "display_and_wait") {
    tgInstructions.textContent = step.display?.data ? `${step.instructions} ${step.display.data}` : step.instructions;
    step = await request("login.wait", { process_id: step.process_id });
  }
  if (step.type === "complete") {
    tgProcess = null;
    status.textContent = "Logged in to Telegram";
    showTelegram(step.account_id);
  } else {
    showStep(step);
  }
}

async function setUpTelegram() {
  const { networks } = await request("networks.list", {});
  const network = networks.find((n) => n.network_id === "telegram");
  if (!network) {
    return;
  }
  for (const flow of network.login_flows.filter((f) => f.flow_id !== "qr")) {
    tgFlow.append(new Option(flow.name, flow.flow_id));
  }
  const { accounts } = await request("accounts.list", {});
  showTelegram(accounts.find((a) => a.network_id === "telegram")?.account_id ?? null);
  telegram.hidden = false;
}

tgStart.addEventListener("submit", async (event) => {
  event.preventDefault();
  try {
    await followStep(await request("login.start", { network_id: "telegram", flow_id: tgFlow.value }));
  } catch (err) {
    status.textContent = `Telegram login failed: ${err.message}`;
    showTelegram(null);
  }
});

tgStep.addEventListener("submit", async (event) => {
  event.preventDefault();
  const values = Object.fromEntries(new FormData(tgStep));
  tgFields.textContent = "";
  try {
    await followStep(await request("login.submit", { process_id: tgProcess, values }));
  } catch (err) {
    status.textContent = `Telegram login failed: ${err.message}`;
    showTelegram(null);
  }
});

tgLogout.addEventListener("click", async () => {
  try {
    await request("accounts.logout", { account_id: tgAccount });
    status.textContent = "Logged out of Telegram";
    showTelegram(null);
  } catch (err) {
    status.textContent = `Telegram logout failed: ${err.message}`;
  }
});

setUpTelegram().catch((err) => {
  status.textContent = `Telegram unavailable: ${err.message}`;
});
