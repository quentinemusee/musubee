// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// What the interface says about who can read a conversation (API 1.12,
// docs/ADR/0039). It says what the core knows, and no more: an unknown
// level is said to be unknown, never safe (CLAUDE.md §2).

import type { AccountConnection, ConversationPrivacy } from "./core-api/types.gen";

/** The two ways to say it: a short label and the explanation behind it. */
export interface PrivacyWords {
  /** Whether the label may reassure: only end-to-end encryption and the test network. */
  safe: boolean;
  label: string;
  detail: string;
}

function capitalized(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1);
}

/**
 * The words for a conversation's privacy, given its network's name and its
 * account's connection (on the device unless said otherwise).
 */
export function privacyWords(privacy: ConversationPrivacy | undefined, network: string, connection: AccountConnection | undefined): PrivacyWords {
  const fromDevice =
    connection === "hosted_bridge"
      ? "This account goes through a bridge on a server, which can read the messages it passes on."
      : "Musubee connects to it from this device, without a server of its own.";
  switch (privacy?.level) {
    case "end_to_end":
      return {
        safe: connection !== "hosted_bridge",
        label: "End-to-end encrypted",
        detail: `Only the people in this conversation can read its messages, on their own devices. ${fromDevice}`,
      };
    case "network_can_read": {
      const readers = privacy.readers ?? network;
      return {
        safe: false,
        label: "Not end-to-end encrypted",
        detail: `${capitalized(readers)} can read this conversation's messages, as in ${network}'s own app. ${fromDevice}`,
      };
    }
    case "bridge_can_read":
      return {
        safe: false,
        label: "Encrypted up to the bridge",
        detail: `${capitalized(privacy.readers ?? "The server of the bridge")} can read this conversation's messages: they are encrypted from this device to the bridge, and the bridge passes them on to ${network}.`,
      };
    case "device_only":
      return {
        safe: true,
        label: "Test conversation",
        detail: "A pretend contact of the echo network: the messages never leave this device.",
      };
    default:
      return {
        safe: false,
        label: "Encryption unknown",
        detail: `Musubee cannot tell who else can read this conversation's messages: take them as readable by ${network}.`,
      };
  }
}
