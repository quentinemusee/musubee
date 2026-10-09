// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

import { createContext, use, useSyncExternalStore } from "react";
import type { State, Store } from "../store";

export const StoreContext = createContext<Store | null>(null);

export function useStore(): Store {
  const store = use(StoreContext);
  if (store === null) {
    throw new Error("useStore outside StoreContext");
  }
  return store;
}

/**
 * Subscribes to a part of the state. The selector must return a value of the
 * state itself (or a primitive), not a new object, so that components render
 * again only when that part changes.
 */
export function useAppState<T>(selector: (state: State) => T): T {
  const store = useStore();
  return useSyncExternalStore(store.subscribe, () => selector(store.getState()));
}
