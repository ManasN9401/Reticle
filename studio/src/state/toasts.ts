import { create } from 'zustand'

export type ToastTone = 'success' | 'failure'

export interface ToastAction {
  label: string
  run(): void
}

export interface Toast {
  id: string
  tone: ToastTone
  title: string
  detail?: string
  actions: ToastAction[]
}

const MAX_TOASTS = 4

interface ToastStore {
  toasts: Toast[]
  /** Pushing an id that is already showing replaces it in place. */
  push(toast: Toast): void
  dismiss(id: string): void
}

export const useToasts = create<ToastStore>((set) => ({
  toasts: [],
  push(toast) {
    set((state) => {
      const index = state.toasts.findIndex((existing) => existing.id === toast.id)
      if (index >= 0) {
        const next = state.toasts.slice()
        next[index] = toast
        return { toasts: next }
      }
      return { toasts: [...state.toasts, toast].slice(-MAX_TOASTS) }
    })
  },
  dismiss(id) {
    set((state) => ({ toasts: state.toasts.filter((toast) => toast.id !== id) }))
  },
}))
