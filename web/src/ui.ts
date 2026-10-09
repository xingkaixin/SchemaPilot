import { create } from "zustand";

/** Fields a new connection starts with, e.g. one named by an imported arrangement. */
export interface ConnectionPreset {
  name: string;
  driver?: string;
}

interface UiState {
  connectionDialog: { open: boolean; editing?: string; preset?: ConnectionPreset };
  openConnectionDialog: (editing?: string, preset?: ConnectionPreset) => void;
  closeConnectionDialog: () => void;
}

export const useUi = create<UiState>()((set) => ({
  connectionDialog: { open: false },
  openConnectionDialog: (editing, preset) =>
    set({ connectionDialog: { open: true, editing, preset } }),
  closeConnectionDialog: () =>
    set((state) => ({ connectionDialog: { ...state.connectionDialog, open: false } })),
}));
