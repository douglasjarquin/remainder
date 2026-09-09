const fixedNow = "2026-03-08T07:30:00.000Z";
const NativeDate = Date;

class FixedDate extends NativeDate {
  constructor(value) {
    super(value === undefined ? fixedNow : value);
  }

  static now() {
    return new NativeDate(fixedNow).getTime();
  }
}

globalThis.Date = FixedDate;
globalThis.fetch = async (input, init = {}) => {
  const url = typeof input === "string" ? input : input.url;
  const headers = new Headers(init.headers);
  const allowedURLs = new Set([
    "https://chatgpt.com/backend-api/wham/usage",
    "https://chatgpt.com/backend-api/codex/usage",
  ]);
  if (!allowedURLs.has(url) ||
      headers.get("authorization") !== "Bearer synthetic-token" ||
      headers.get("chatgpt-account-id") !== "acct-1") {
    throw new Error("unexpected quota-axi fixture request");
  }
  return new Response(JSON.stringify({
    plan_type: "synthetic",
    account_id: "acct-1",
    rate_limit: {
      primary_window: {
        used_percent: 58,
        limit_window_seconds: 18000,
        reset_at: "2026-03-08T12:30:00Z",
      },
      secondary_window: {
        used_percent: 58,
        limit_window_seconds: 604800,
        reset_at: "2026-03-15T07:30:00Z",
      },
    },
  }), {
    status: 200,
    headers: { "content-type": "application/json" },
  });
};
