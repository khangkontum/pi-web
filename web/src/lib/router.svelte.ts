// Hash routing: sessions are linkable (#/session/<id>) and back/forward work.
// The route is the single source of truth for which session is open; App
// reacts to it.

export type Route = { kind: "home" } | { kind: "session"; id: string } | { kind: "new" };

function parse(hash: string): Route {
  const m = hash.match(/^#\/session\/(.+)$/);
  if (m) return { kind: "session", id: decodeURIComponent(m[1]) };
  if (hash === "#/new") return { kind: "new" };
  return { kind: "home" };
}

class Router {
  route = $state<Route>(parse(location.hash));

  constructor() {
    window.addEventListener("hashchange", () => {
      this.route = parse(location.hash);
    });
  }

  openSession(id: string): void {
    location.hash = `#/session/${encodeURIComponent(id)}`;
  }

  openNew(): void {
    location.hash = "#/new";
  }

  home(): void {
    // strip the hash without adding a history entry for ""
    if (location.hash) location.hash = "#/";
    this.route = { kind: "home" };
  }
}

export const router = new Router();
