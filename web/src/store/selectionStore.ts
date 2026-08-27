import { create } from "zustand";

// 批量操作的选中集。独立成一个 store 而不是放在页面 state 里，
// 是因为顶部的批量操作菜单、列表行的复选框、左栏切换分组三处都要读写它。
interface SelectionState {
  selected: Set<string>;
  toggle: (id: string) => void;
  // selectPage 只对当前页可见的 id 生效——「全选」不能悄悄把没加载出来的行也算上，
  // 否则用户以为删了 50 条，实际删了 5000 条。
  selectPage: (ids: string[]) => void;
  clearPage: (ids: string[]) => void;
  clear: () => void;
  isSelected: (id: string) => boolean;
  count: () => number;
  ids: () => string[];
}

export const useSelectionStore = create<SelectionState>((set, get) => ({
  selected: new Set<string>(),

  toggle: (id) =>
    set((state) => {
      const next = new Set(state.selected);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return { selected: next };
    }),

  selectPage: (ids) =>
    set((state) => {
      const next = new Set(state.selected);
      ids.forEach((id) => next.add(id));
      return { selected: next };
    }),

  clearPage: (ids) =>
    set((state) => {
      const next = new Set(state.selected);
      ids.forEach((id) => next.delete(id));
      return { selected: next };
    }),

  clear: () => set({ selected: new Set<string>() }),

  isSelected: (id) => get().selected.has(id),
  count: () => get().selected.size,
  ids: () => Array.from(get().selected),
}));
