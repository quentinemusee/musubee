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
const ROOM_NAME = "Instant Echo";

const status = document.getElementById("status");
const loginButton = document.getElementById("login");
const composer = document.getElementById("composer");
const text = document.getElementById("text");
const sendButton = document.getElementById("send");
const log = document.getElementById("log");

let nextId = 0;
let roomId = null;

function show(line) {
  const item = document.createElement("li");
  item.textContent = line;
  log.prepend(item);
}

async function request(command, params) {
  const response = await window.Capacitor.nativePromise(PLUGIN, "call", {
    request: { id: ++nextId, command, params },
  });
  if (response.error) {
    throw new Error(`${command}: ${response.error}`);
  }
  return response.result;
}

function describe(event) {
  switch (event.type) {
    case "message":
      return `${event.from_me ? "Sent" : "Received"}: ${event.body}`;
    case "message_status":
      return `Status: ${event.status}${event.message ? ` (${event.message})` : ""}`;
    case "network_state":
      return `Network ${event.network}: ${event.state}`;
    default:
      return `Event: ${event.type}`;
  }
}

window.Capacitor.addListener(PLUGIN, "event", (event) => show(describe(event)));

loginButton.addEventListener("click", async () => {
  loginButton.disabled = true;
  status.textContent = "Logging in…";
  try {
    const result = await request("login", { username: "alice" });
    const room = result.rooms.find((r) => r.name === ROOM_NAME);
    if (!room) {
      throw new Error(`no "${ROOM_NAME}" room`);
    }
    roomId = room.room_id;
    status.textContent = `Logged in, room: ${ROOM_NAME}`;
    text.disabled = false;
    sendButton.disabled = false;
  } catch (err) {
    status.textContent = `Login failed: ${err.message}`;
    loginButton.disabled = false;
  }
});

composer.addEventListener("submit", async (event) => {
  event.preventDefault();
  const body = text.value.trim();
  if (!body || !roomId) {
    return;
  }
  text.value = "";
  try {
    await request("send", { room_id: roomId, text: body });
  } catch (err) {
    show(`Send failed: ${err.message}`);
  }
});
