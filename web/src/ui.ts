import { create } from "zustand";

interface UiState {
  connectionDialog: { open: boolean; editing?: string };
  openConnectionDialog: (editing?: string) => void;
  closeConnectionDialog: () => void;
}

export const useUi = create<UiState>()((set) => ({
  connectionDialog: { open: false },
  openConnectionDialog: (editing) => set({ connectionDialog: { open: true, editing } }),
  closeConnectionDialog: () =>
    set((state) => ({ connectionDialog: { ...state.connectionDialog, open: false } })),
}));
