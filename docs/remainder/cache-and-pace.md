# Cache and pace

Freshness, cache policy, and pace. Moved out of the README.

## Pace

Pace is a per-window status calculated from the same observation used by compact, JSON, and scalar output.
For a known percentage allowance with a duration and future reset, Remainder calculates `reserve percentage points = percentage remaining - cycle time remaining percentage` under an explicitly uniform budget assumption.
A reserve below `-1` is `ahead`, meaning spending is faster than the uniform reserve; a reserve above `1` is `behind`, meaning spending is slower; the inclusive range from `-1` through `1` is `on_pace`.
These names match the pinned [quota-axi pace calculation](https://github.com/kunchenguid/quota-axi/blob/d3190237588cdf51046b27a346ff2e834855bf37/src/pace.ts#L38-L69) and [threshold classifier](https://github.com/kunchenguid/quota-axi/blob/d3190237588cdf51046b27a346ff2e834855bf37/src/pace.ts#L414-L420).
JSON, compact, and TOON output preserve the calculation identity, calculation time, original observation time, remaining value, reset, duration, time remaining percentage, and reserve.
The pace describes the original observation and is recomputed for each output after freshness classification; stale evidence, a reset that has passed by evaluation time, a future observation or implied cycle, or missing usage, duration, or reset produces `unknown` with a reason.
Unlimited, unknown, and zero remaining remain distinct, and non-percentage windows have no pace calculation.
Remainder does not combine account and model windows, choose an aggregate minimum across unlike pools, or fold paid credits into included allowance.

## Freshness

Use `--freshness any` to allow stale evidence or `--freshness fresh` to reject stale and unknown freshness.

## Cache

The default `--cache auto --max-age 5s` policy reuses an eligible complete provider observation before selecting a report field or window.
Use `--cache off` for a bounded live read, `--cache only` to refuse a miss without credential parsing or network access, `--refresh` to require an observation newer than the request's starting generation, and `--stale-on-error` to allow an expired observation only after a transient refresh failure.
Forced requests that overlap can share the same newer observation; a forced request that starts after that observation was recorded requires another refresh.
Transient failures use a one-second local retry delay when the provider supplies no valid deadline, and provider retry deadlines are capped at one minute so a cached failure cannot create a permanent local lockout.
Cached observations retain their original `observed_at`; previously verified account identity becomes historical, while unknown identity remains unknown.
Cache records live under the operating system user cache directory at `remainder/v1/<binding-hash>/`, use restrictive permissions and atomic replacement, and contain no credential or raw auth path.
