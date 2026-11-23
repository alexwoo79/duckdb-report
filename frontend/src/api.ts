import type { Backend } from "./types";

const numberFormatter = new Intl.NumberFormat("zh-CN", {
  maximumFractionDigits: 4,
});

export function backend(): Backend {
  const value = window.go?.main?.App;
  if (!value) {
    throw new Error("Wails 后端尚未就绪，请重新启动应用。");
  }
  return value;
}

export function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  if (typeof error === "string") return error;
  if (error && typeof error === "object" && "message" in error) {
    return String((error as { message: unknown }).message);
  }
  return String(error);
}

export function baseName(path: string): string {
  return path.split(/[\\/]/).pop() ?? path;
}

export function fileStem(path: string): string {
  return baseName(path).replace(/\.[^.]+$/, "");
}

export function fileKind(path: string): string {
  return (path.split(".").pop() ?? "").toLowerCase();
}

export function resolveTemplate(
  value: string,
  params: Record<string, string>,
): string {
  return value.replace(/\{\{\s*([a-zA-Z0-9_.-]+)\s*\}\}/g, (_, key: string) => {
    return params[key] ?? `{{${key}}}`;
  });
}

export function formatNumber(value: unknown): string {
  if (value === null || value === undefined) return "NULL";
  if (typeof value === "number") {
    return numberFormatter.format(value);
  }
  return String(value);
}

export function formatTime(value: number): string {
  if (!value) return "刚刚";
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(value * 1000));
}
