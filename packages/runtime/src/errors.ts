/**
 * Native WebMCP calls (registerTool/getTools/executeTool) can reject with values
 * that stringify to "{}" — DOMExceptions and Error instances have non-enumerable
 * message/name, so `JSON.stringify(err)` hides them and an unawaited rejection
 * logs a useless empty object. describeError digs out something readable.
 */
export function describeError(e: unknown): string {
  if (e instanceof Error) return `${e.name}: ${e.message}`;
  if (typeof e === "object" && e !== null) {
    const anyE = e as { name?: unknown; message?: unknown };
    if (anyE.message != null || anyE.name != null) {
      return `${String(anyE.name ?? "Error")}: ${String(anyE.message ?? "")}`.trim();
    }
    try {
      const s = JSON.stringify(e);
      if (s && s !== "{}") return s;
    } catch {
      /* circular / non-serializable */
    }
    return Object.prototype.toString.call(e);
  }
  return String(e);
}
