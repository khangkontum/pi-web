// Transient notifications: extension `notify` requests and app-level errors
// (failed sends, stream drops). Auto-dismiss; errors linger longer.

export type ToastLevel = "info" | "warning" | "error";

export interface Toast {
  id: number;
  level: ToastLevel;
  message: string;
}

let nextId = 1;

class Toasts {
  items = $state<Toast[]>([]);

  show(message: string, level: ToastLevel = "info"): void {
    const id = nextId++;
    this.items.push({ id, level, message });
    const ttl = level === "error" ? 10000 : 5000;
    setTimeout(() => this.dismiss(id), ttl);
  }

  error(message: string): void {
    this.show(message, "error");
  }

  dismiss(id: number): void {
    this.items = this.items.filter((t) => t.id !== id);
  }
}

export const toasts = new Toasts();
