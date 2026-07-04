#!/usr/bin/env python3
import json
import math
from collections import defaultdict
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parent
ROWS = ROOT / "results.jsonl"
QUEUE_ROWS = ROOT / "queue-results.jsonl"
PRODUCTION_QUEUE_ROWS = ROOT / "production-queue-results.jsonl"
RETRY_QUEUE_SUMMARY = ROOT / "retry-queue-summary.json"
OUT = ROOT / "report.html"
META = ROOT / "run-meta.json"


def pct(values, p):
    if not values:
        return 0
    values = sorted(values)
    k = (len(values) - 1) * (p / 100)
    f = math.floor(k)
    c = math.ceil(k)
    if f == c:
        return values[int(k)]
    return values[f] * (c - k) + values[c] * (k - f)


rows = []
for line in ROWS.read_text().splitlines():
    if line.strip():
        row = json.loads(line)
        row.setdefault("phase", "capacity")
        rows.append(row)

queue_rows = []
if QUEUE_ROWS.exists():
    for line in QUEUE_ROWS.read_text().splitlines():
        if line.strip():
            row = json.loads(line)
            row.setdefault("phase", "queue")
            queue_rows.append(row)

production_queue_rows = []
if PRODUCTION_QUEUE_ROWS.exists():
    for line in PRODUCTION_QUEUE_ROWS.read_text().splitlines():
        if line.strip():
            row = json.loads(line)
            row.setdefault("phase", "production_queue")
            production_queue_rows.append(row)

by_key = defaultdict(list)
by_model = defaultdict(list)
for row in rows:
    by_key[(row["model"], row["concurrency"])].append(row)
    by_model[row["model"]].append(row)

summary = []
for (model, concurrency), items in sorted(by_key.items()):
    durations = [r["duration_ms"] for r in items]
    waits = [r["queue_wait_ms"] for r in items]
    limits = [r.get("limit", 0) for r in items if r.get("limit", 0)]
    queue_depths = [r.get("queue_depth", 0) for r in items]
    queue_maxes = [r.get("queue_max", 0) for r in items if r.get("queue_max", 0)]
    ok = sum(1 for r in items if r["ok"])
    overload = sum(1 for r in items if r["status"] == 429)
    errors = len(items) - ok - overload
    queued = sum(1 for r in items if r.get("queue_wait_ms", 0) > 0)
    summary.append({
        "model": model,
        "concurrency": concurrency,
        "requests": len(items),
        "ok": ok,
        "immediate": len(items) - queued,
        "queued": queued,
        "overload": overload,
        "errors": errors,
        "success_rate": ok / len(items) if items else 0,
        "avg_ms": sum(durations) / len(durations) if durations else 0,
        "p50_ms": pct(durations, 50),
        "p95_ms": pct(durations, 95),
        "p99_ms": pct(durations, 99),
        "max_ms": max(durations) if durations else 0,
        "avg_queue_ms": sum(waits) / len(waits) if waits else 0,
        "p50_queue_ms": pct(waits, 50),
        "p95_queue_ms": pct(waits, 95),
        "p99_queue_ms": pct(waits, 99),
        "max_queue_ms": max(waits) if waits else 0,
        "max_queue_depth": max(queue_depths) if queue_depths else 0,
        "queue_max": max(queue_maxes) if queue_maxes else 0,
        "limit_start": limits[0] if limits else 0,
        "limit_end": limits[-1] if limits else 0,
        "limit_min": min(limits) if limits else 0,
        "limit_max": max(limits) if limits else 0,
        "codes": dict(sorted({c: sum(1 for r in items if r["error_code"] == c) for c in set(r["error_code"] for r in items) if c}.items())),
    })

capacity = []
for model, items in sorted(by_model.items()):
    levels = sorted(set(r["concurrency"] for r in items))
    clean = []
    for level in levels:
        s = next(x for x in summary if x["model"] == model and x["concurrency"] == level)
        if s["success_rate"] >= 0.95 and s["overload"] == 0 and s["errors"] == 0:
            clean.append(level)
    best = max(clean) if clean else 0
    highest = max(levels) if levels else 0
    capacity.append({
        "model": model,
        "observed_capacity": best,
        "label": f">= {best}" if best == highest and best else str(best),
        "note": "No ceiling reached in this run." if best == highest and best else "Errors or overloads appeared above this level.",
    })

queue_summary = []
queue_by_model = defaultdict(list)
queue_source = production_queue_rows if production_queue_rows else (queue_rows if queue_rows else rows)
for row in queue_source:
    queue_by_model[row["model"]].append(row)
for model, items in sorted(queue_by_model.items()):
    waits = [r["queue_wait_ms"] for r in items]
    durations = [r["duration_ms"] for r in items]
    queue_depths = [r.get("queue_depth", 0) for r in items]
    queue_maxes = [r.get("queue_max", 0) for r in items if r.get("queue_max", 0)]
    limits = [r.get("limit", 0) for r in items if r.get("limit", 0)]
    ok = sum(1 for r in items if r["ok"])
    queued = sum(1 for r in items if r.get("queue_wait_ms", 0) > 0)
    queue_summary.append({
        "model": model,
        "requests": len(items),
        "ok": ok,
        "immediate": len(items) - queued,
        "queued": queued,
        "success_rate": ok / len(items) if items else 0,
        "avg_queue_ms": sum(waits) / len(waits) if waits else 0,
        "p50_queue_ms": pct(waits, 50),
        "p95_queue_ms": pct(waits, 95),
        "p99_queue_ms": pct(waits, 99),
        "max_queue_ms": max(waits) if waits else 0,
        "max_queue_depth": max(queue_depths) if queue_depths else 0,
        "queue_max": max(queue_maxes) if queue_maxes else 0,
        "limit_min": min(limits) if limits else 0,
        "limit_max": max(limits) if limits else 0,
        "avg_latency_ms": sum(durations) / len(durations) if durations else 0,
        "p50_latency_ms": pct(durations, 50),
        "p95_latency_ms": pct(durations, 95),
        "p99_latency_ms": pct(durations, 99),
        "overload": sum(1 for r in items if r["status"] == 429),
    })

production_queue_summary = []
prod_by_model = defaultdict(list)
for row in production_queue_rows:
    prod_by_model[row["model"]].append(row)
for model, items in sorted(prod_by_model.items()):
    waits = [r["queue_wait_ms"] for r in items]
    durations = [r["duration_ms"] for r in items]
    limits = [r.get("limit", 0) for r in items if r.get("limit", 0)]
    queue_depths = [r.get("queue_depth", 0) for r in items]
    status_counts = {str(status): sum(1 for r in items if r.get("status") == status) for status in sorted({r.get("status") for r in items})}
    ok = sum(1 for r in items if r["ok"])
    overload = sum(1 for r in items if r["status"] == 429)
    client_timeout = sum(1 for r in items if r["status"] == 0)
    queued = sum(1 for r in items if r.get("queue_wait_ms", 0) > 0)
    admitted = sum(1 for r in items if r.get("limit", 0) > 0)
    production_queue_summary.append({
        "model": model,
        "requests": len(items),
        "ok": ok,
        "overload": overload,
        "client_timeout": client_timeout,
        "status_counts": status_counts,
        "success_rate": ok / len(items) if items else 0,
        "immediate": len(items) - queued,
        "queued": queued,
        "admitted": admitted,
        "missing_adaptive_headers": len(items) - admitted,
        "avg_queue_ms": sum(waits) / len(waits) if waits else 0,
        "p50_queue_ms": pct(waits, 50),
        "p75_queue_ms": pct(waits, 75),
        "p90_queue_ms": pct(waits, 90),
        "p95_queue_ms": pct(waits, 95),
        "p99_queue_ms": pct(waits, 99),
        "max_queue_ms": max(waits) if waits else 0,
        "p50_latency_ms": pct(durations, 50),
        "p95_latency_ms": pct(durations, 95),
        "p99_latency_ms": pct(durations, 99),
        "max_latency_ms": max(durations) if durations else 0,
        "limit_min": min(limits) if limits else 0,
        "limit_max": max(limits) if limits else 0,
        "max_queue_depth": max(queue_depths) if queue_depths else 0,
        "queue_max": max([r.get("queue_max", 0) for r in items]) if items else 0,
    })

production_conclusions = []
if production_queue_summary:
    best = max(production_queue_summary, key=lambda d: (d["ok"], -d["client_timeout"], -d["p95_queue_ms"]))
    worst_timeout = max(production_queue_summary, key=lambda d: d["client_timeout"])
    deepest = max(production_queue_summary, key=lambda d: d["max_queue_depth"])
    highest_wait = max(production_queue_summary, key=lambda d: d["p95_queue_ms"])
    saturated = sum(1 for d in production_queue_summary if d["max_queue_depth"] >= 900)
    production_conclusions = [
        {
            "label": "BEST 1000-BURST RESULT",
            "value": best["model"],
            "detail": f"{best['ok']}/{best['requests']} completed with p95 queue {best['p95_queue_ms'] / 1000:.1f}s.",
        },
        {
            "label": "QUEUE SATURATION",
            "value": f"{saturated}/{len(production_queue_summary)} MODELS",
            "detail": f"Highest observed depth was {deepest['max_queue_depth']}/{deepest['queue_max'] or 2048}; the queue absorbed bursts, but wait time grew sharply.",
        },
        {
            "label": "SYNC HTTP RISK",
            "value": worst_timeout["model"],
            "detail": f"{worst_timeout['client_timeout']} k6 client timeouts; use async jobs for large bursts on this model.",
        },
        {
            "label": "LONGEST P95 QUEUE",
            "value": highest_wait["model"],
            "detail": f"p95 queue wait reached {highest_wait['p95_queue_ms'] / 1000:.1f}s under 1000 VUs.",
        },
    ]

retry_queue_summary = []
retry_queue_conclusions = []
if RETRY_QUEUE_SUMMARY.exists():
    retry_rows = json.loads(RETRY_QUEUE_SUMMARY.read_text())
    retry_by_model = defaultdict(list)
    for row in retry_rows:
        retry_by_model[row["model"]].append(row)
    for model, items in sorted(retry_by_model.items()):
        items = sorted(items, key=lambda r: r["level"])
        queue_safe = [r for r in items if r.get("max_queue_wait_ms", 0) <= 8000 and int(r.get("statuses", {}).get("0", 0)) == 0]
        clean_safe = [r for r in queue_safe if r.get("ok") == r.get("rows")]
        crossing = next((r for r in items if r.get("max_queue_wait_ms", 0) > 8000 or int(r.get("statuses", {}).get("0", 0)) > 0), None)
        best = queue_safe[-1] if queue_safe else None
        clean = clean_safe[-1] if clean_safe else None
        retry_queue_summary.append({
            "model": model,
            "queue_wait_safe_level": best["level"] if best else 0,
            "clean_success_safe_level": clean["level"] if clean else 0,
            "crossing_level": crossing["level"] if crossing else 0,
            "max_queue_wait_ms": best.get("max_queue_wait_ms", 0) if best else 0,
            "p95_queue_wait_ms": best.get("p95_queue_wait_ms", 0) if best else 0,
            "p95_latency_ms": best.get("p95_latency_ms", 0) if best else 0,
            "ok": best.get("ok", 0) if best else 0,
            "rows": best.get("rows", 0) if best else 0,
            "statuses": best.get("statuses", {}) if best else {},
            "crossing_max_queue_wait_ms": crossing.get("max_queue_wait_ms", 0) if crossing else 0,
            "crossing_statuses": crossing.get("statuses", {}) if crossing else {},
        })
    if retry_queue_summary:
        best_queue = max(retry_queue_summary, key=lambda d: d["queue_wait_safe_level"])
        best_clean = max(retry_queue_summary, key=lambda d: d["clean_success_safe_level"])
        retry_queue_conclusions = [
            {
                "label": "BEST QUEUE-WAIT SLA",
                "value": best_queue["model"],
                "detail": f"Stayed under 8s queue wait through {best_queue['queue_wait_safe_level']} concurrent burst requests.",
            },
            {
                "label": "BEST CLEAN BURST",
                "value": best_clean["model"],
                "detail": f"Stayed under 8s queue wait with all responses HTTP 200 through {best_clean['clean_success_safe_level']} requests.",
            },
            {
                "label": "RETRY MODE",
                "value": "3 ATTEMPTS",
                "detail": "Real exponential backoff enabled: 250ms initial, 2000ms max, Retry-After honored by gateway.",
            },
            {
                "label": "PRODUCTION READ",
                "value": "LOWER CAPS",
                "detail": "Backoff holds permits longer, so safe sync queue caps are far lower than no-retry stress numbers.",
            },
        ]

data = {
    "generated_at": datetime.now(timezone.utc).isoformat(),
    "meta": json.loads(META.read_text()) if META.exists() else {},
    "requests": len(rows),
    "summary": summary,
    "capacity": capacity,
    "queue_summary": queue_summary,
    "production_queue_summary": production_queue_summary,
    "production_conclusions": production_conclusions,
    "retry_queue_summary": retry_queue_summary,
    "retry_queue_conclusions": retry_queue_conclusions,
    "raw": rows,
    "queue_raw": queue_rows,
    "production_queue_raw": production_queue_rows,
}

payload = json.dumps(data)
html_template = """<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>3.11 LABS // GATEWAY PERFORMANCE BENCHMARK</title>
  <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
  <style>
    :root {
      --bg-main: #000000;
      --bg-card: #000000;
      --border-color: #d4d3cb;
      --border-muted: #2e2d2a;
      --text-primary: #d4d3cb;
      --text-secondary: #a3a29b;
      --text-muted: #5a5954;
      --card-shadow: none;
      --grid-line: #1c1c1a;
      
      /* V1 Monochrome defaults */
      --success: #d4d3cb;
      --warning: #d4d3cb;
      --danger: #d4d3cb;
      --info: #d4d3cb;
      
      --accent-glow: transparent;
    }

    /* V2 Color accents overrides for Dark theme */
    :root.v2 {
      --primary: #00f3ff;
      --primary-light: #3be6ff;
      --success: #39ff14; /* phosphor green */
      --warning: #ffb000; /* amber */
      --danger: #ff3333;  /* red */
      --info: #00f3ff;    /* cyan */
      
      --accent-glow: rgba(0, 243, 255, 0.15);
    }

    :root.light {
      --bg-main: #dfded8;
      --bg-card: #dfded8;
      --border-color: #000000;
      --border-muted: #9c9b95;
      --text-primary: #000000;
      --text-secondary: #222220;
      --text-muted: #6b6a65;
      --grid-line: #cbcabf;
      
      /* V1 Light Monochrome defaults */
      --success: #000000;
      --warning: #000000;
      --danger: #000000;
      --info: #000000;
      
      --accent-glow: transparent;
    }

    /* V2 Color accents overrides for Light theme */
    :root.light.v2 {
      --primary: #0066aa;
      --primary-light: #0088cc;
      --success: #008800; /* deep green */
      --warning: #bb6600; /* dark amber */
      --danger: #cc0000;  /* deep red */
      --info: #0066aa;    /* dark cyan */
      
      --accent-glow: rgba(0, 102, 170, 0.1);
    }

    * {
      box-sizing: border-box;
      margin: 0;
      padding: 0;
    }

    body {
      background-color: var(--bg-main);
      color: var(--text-primary);
      font-family: 'SF Mono', Monaco, Consolas, 'Fira Code', 'Courier New', monospace;
      line-height: 1.4;
      font-size: 11px;
      text-transform: uppercase;
      letter-spacing: 0.02em;
      transition: background-color 0.15s ease, color 0.15s ease;
      min-height: 100vh;
      padding-bottom: 2rem;
    }

    header {
      border-bottom: 1px solid var(--border-color);
      padding: 1.5rem 2rem;
      display: grid;
      grid-template-columns: 1fr 1fr 1.3fr;
      gap: 1.5rem;
      background: var(--bg-card);
    }

    @media (max-width: 1024px) {
      header {
        grid-template-columns: 1fr;
        gap: 1.25rem;
        padding: 1rem;
      }
    }

    .header-section {
      display: flex;
      flex-direction: column;
      justify-content: space-between;
    }

    .header-section .title-row {
      display: flex;
      gap: 0.75rem;
      align-items: flex-start;
    }

    .header-section svg {
      color: var(--text-primary);
      flex-shrink: 0;
    }

    /* Cyan glow on wireframes in V2 mode */
    :root.v2 .header-section svg {
      color: var(--info);
      filter: drop-shadow(0 0 4px var(--accent-glow));
    }

    .header-section .label {
      font-weight: 700;
      font-size: 12px;
      transition: color 0.15s ease;
    }

    :root.v2 .header-section .label {
      color: var(--info);
    }

    .header-section .sublabel {
      font-size: 10px;
      color: var(--text-secondary);
      margin-top: 2px;
    }

    .header-section .note {
      font-size: 9px;
      color: var(--text-muted);
      margin-top: 4px;
    }

    .ruler-container {
      display: flex;
      flex-direction: column;
      align-items: flex-start;
      margin-top: 4px;
    }

    .ruler {
      font-size: 8px;
      letter-spacing: 1px;
      color: var(--text-secondary);
      line-height: 1;
    }

    .ruler-arrow {
      font-size: 7px;
      line-height: 1;
      margin-top: -2px;
    }

    .btn-text {
      background: none;
      border: 1px solid var(--border-color);
      color: var(--text-primary);
      font-family: inherit;
      font-size: 10px;
      padding: 3px 8px;
      cursor: pointer;
      font-weight: bold;
      text-transform: uppercase;
      outline: none;
      transition: background-color 0.1s ease, color 0.1s ease, border-color 0.15s ease;
    }

    .btn-text:hover {
      background-color: var(--border-color);
      color: var(--bg-main);
    }

    :root.v2 .btn-text {
      border-color: var(--info);
      color: var(--info);
    }

    :root.v2 .btn-text:hover {
      background-color: var(--info);
      color: var(--bg-main);
    }

    main {
      max-width: 1440px;
      margin: 0 auto;
      padding: 0 2rem;
    }

    @media (max-width: 900px) {
      main {
        padding: 0 1rem;
      }
    }

    /* KPI Metrics Panel */
    .metrics-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
      gap: 1rem;
      margin: 1.5rem 0;
    }

    .kpi-card {
      border: 1px solid var(--border-color);
      position: relative;
      padding: 1.25rem 1rem 0.75rem;
      background: var(--bg-card);
      transition: border-color 0.15s ease;
    }

    :root.v2 .kpi-card {
      border-color: var(--info);
    }

    .kpi-card .kpi-label {
      position: absolute;
      top: -7px;
      left: 10px;
      background: var(--bg-main);
      padding: 0 6px;
      font-size: 9px;
      color: var(--text-primary);
      font-weight: 700;
      transition: color 0.15s ease;
    }

    :root.v2 .kpi-card .kpi-label {
      color: var(--info);
    }

    .kpi-card .kpi-value {
      font-size: 22px;
      font-weight: 700;
      color: var(--text-primary);
      line-height: 1.1;
    }

    .kpi-card .kpi-sublabel {
      font-size: 9px;
      color: var(--text-secondary);
      margin-top: 4px;
    }

    /* Checklist panel */
    .checklist-grid {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 4px 12px;
      font-size: 9px;
      color: var(--text-secondary);
    }

    /* Navigation Menu */
    .tabs-container {
      border-bottom: 1px solid var(--border-color);
      display: flex;
      gap: 0.5rem;
      margin-bottom: 1.5rem;
      overflow-x: auto;
      transition: border-color 0.15s ease;
    }

    :root.v2 .tabs-container {
      border-color: var(--info);
    }

    .tab-btn {
      background: none;
      border: 1px solid transparent;
      border-bottom: none;
      color: var(--text-secondary);
      font-family: inherit;
      font-size: 11px;
      font-weight: 700;
      padding: 8px 14px;
      cursor: pointer;
      transition: all 0.1s ease;
      white-space: nowrap;
    }

    .tab-btn:hover {
      color: var(--text-primary);
      background-color: rgba(255, 255, 255, 0.03);
    }

    :root.light .tab-btn:hover {
      background-color: rgba(0, 0, 0, 0.03);
    }

    .tab-btn.active {
      background-color: var(--border-color);
      color: var(--bg-main);
      border: 1px solid var(--border-color);
      border-bottom: none;
    }

    :root.v2 .tab-btn.active {
      background-color: var(--info);
      color: var(--bg-main);
      border-color: var(--info);
    }

    /* Tab Panels */
    .tab-panel {
      display: none;
      animation: terminalInit 0.15s ease-out;
    }

    .tab-panel.active {
      display: block;
    }

    @keyframes terminalInit {
      from { opacity: 0; }
      to { opacity: 1; }
    }

    /* Layout Grids */
    .charts-grid {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 1.5rem;
      margin-bottom: 1.5rem;
    }

    @media (max-width: 1024px) {
      .charts-grid {
        grid-template-columns: 1fr;
      }
    }

    .chart-card {
      border: 1px solid var(--border-color);
      border-radius: 0;
      padding: 1.25rem;
      background: var(--bg-card);
      display: flex;
      flex-direction: column;
      transition: border-color 0.15s ease;
    }

    :root.v2 .chart-card {
      border-color: var(--info);
    }

    .chart-card h2 {
      font-size: 11px;
      font-weight: 700;
      margin-bottom: 4px;
      color: var(--text-primary);
      transition: color 0.15s ease;
    }

    :root.v2 .chart-card h2 {
      color: var(--info);
    }

    .chart-card p.subtitle {
      font-size: 9px;
      color: var(--text-muted);
      margin-bottom: 1rem;
    }

    .chart-container {
      position: relative;
      height: 280px;
      width: 100%;
    }

    /* Tables */
    .table-card {
      border: 1px solid var(--border-color);
      padding: 1.25rem;
      background: var(--bg-card);
      margin-top: 1.5rem;
      transition: border-color 0.15s ease;
    }

    :root.v2 .table-card {
      border-color: var(--info);
    }

    .table-header-row {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-bottom: 1rem;
      flex-wrap: wrap;
      gap: 1rem;
    }

    .table-header-row h2 {
      font-size: 11px;
      font-weight: 700;
      transition: color 0.15s ease;
    }

    :root.v2 .table-header-row h2 {
      color: var(--info);
    }

    .table-actions {
      display: flex;
      gap: 0.5rem;
      align-items: center;
      flex-wrap: wrap;
    }

    .search-input {
      background: var(--bg-main);
      border: 1px solid var(--border-color);
      border-radius: 0;
      padding: 4px 8px;
      color: var(--text-primary);
      font-family: inherit;
      font-size: 10px;
      outline: none;
      transition: background-color 0.1s ease, border-color 0.15s ease;
      min-width: 180px;
    }

    :root.v2 .search-input {
      border-color: var(--info);
    }

    .search-input:focus {
      background-color: rgba(255, 255, 255, 0.05);
    }

    :root.light .search-input:focus {
      background-color: rgba(0, 0, 0, 0.02);
    }

    .table-wrapper {
      overflow-x: auto;
      border: 1px solid var(--border-color);
      transition: border-color 0.15s ease;
    }

    :root.v2 .table-wrapper {
      border-color: var(--info);
    }

    table {
      width: 100%;
      border-collapse: collapse;
      text-align: left;
      font-size: 10px;
    }

    th {
      background-color: var(--border-color);
      color: var(--bg-main);
      font-weight: 700;
      font-size: 10px;
      padding: 6px 10px;
      border: 1px solid var(--border-color);
      cursor: pointer;
      user-select: none;
      position: relative;
      transition: background-color 0.15s ease, color 0.15s ease, border-color 0.15s ease;
    }

    :root.v2 th {
      background-color: var(--info);
      color: var(--bg-main);
      border-color: var(--info);
    }

    th:hover {
      opacity: 0.9;
    }

    th.sort-asc::after {
      content: ' ^';
      position: absolute;
      font-weight: 700;
    }

    th.sort-desc::after {
      content: ' v';
      position: absolute;
      font-weight: 700;
    }

    td {
      padding: 6px 10px;
      border: 1px solid var(--border-color);
      color: var(--text-secondary);
      vertical-align: middle;
      transition: border-color 0.15s ease, background-color 0.05s ease;
    }

    :root.v2 td {
      border-color: var(--border-muted);
    }

    tr {
      transition: background-color 0.05s ease, color 0.05s ease;
    }

    tr.expandable-row {
      cursor: pointer;
    }

    tr:hover td {
      background-color: var(--border-color);
      color: var(--bg-main);
    }

    :root.v2 tr:hover td {
      background-color: var(--info);
      color: var(--bg-main);
    }

    tr:hover td span.model-badge {
      background-color: var(--bg-main);
      color: var(--text-primary);
      border-color: var(--border-color);
    }

    :root.v2 tr:hover td span.model-badge {
      border-color: var(--bg-main);
    }

    tr:hover td span.badge {
      background-color: var(--bg-main);
      color: var(--text-primary);
      border-color: var(--border-color);
    }

    .detail-row td {
      padding: 0;
    }

    tr:hover.detail-row td {
      background-color: transparent;
    }

    .detail-content {
      max-height: 0;
      overflow: hidden;
      transition: max-height 0.25s ease-out;
      background: rgba(255, 255, 255, 0.02);
    }

    :root.light .detail-content {
      background: rgba(0, 0, 0, 0.02);
    }

    .detail-content-inner {
      padding: 1rem;
      font-family: inherit;
      font-size: 10px;
      color: var(--text-secondary);
      border-top: 1px solid var(--border-color);
      transition: border-color 0.15s ease;
    }

    :root.v2 .detail-content-inner {
      border-top-color: var(--info);
    }

    .detail-content pre {
      white-space: pre-wrap;
      word-break: break-all;
      margin: 0;
    }

    .text-right {
      text-align: right;
    }

    .font-mono {
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    }

    /* Brackets & Badges */
    .model-badge {
      font-family: inherit;
      font-size: 10px;
      font-weight: 700;
      color: var(--text-primary);
      background: transparent;
      padding: 1px 4px;
      border: 1px dashed var(--border-color);
      transition: border-color 0.15s ease;
    }

    :root.v2 .model-badge {
      border-color: var(--info);
    }

    .badge {
      display: inline-flex;
      align-items: center;
      font-size: 9px;
      font-weight: 700;
      padding: 1px 5px;
      border: 1px solid var(--border-color);
      color: var(--text-primary);
      transition: border-color 0.15s ease, color 0.15s ease;
    }

    .badge-info {
      border-style: solid;
    }

    :root.v2 .badge-info {
      border-color: var(--info);
      color: var(--info);
    }

    .badge-success {
      border-style: solid;
      border-color: var(--success);
      color: var(--success);
    }

    .badge-warning {
      border-style: dashed;
      border-color: var(--warning);
      color: var(--warning);
    }

    .badge-danger {
      border-style: dotted;
      border-color: var(--danger);
      color: var(--danger);
      font-weight: 900;
    }

    /* Pagination */
    .pagination-container {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-top: 1.25rem;
      padding-top: 1rem;
      border-top: 1px solid var(--border-color);
      font-size: 10px;
      color: var(--text-secondary);
      transition: border-color 0.15s ease;
    }

    :root.v2 .pagination-container {
      border-color: var(--info);
    }

    .pagination-buttons {
      display: flex;
      gap: 0.5rem;
    }

    .btn-icon {
      background: none;
      border: 1px solid var(--border-color);
      padding: 3px 8px;
      color: var(--text-primary);
      cursor: pointer;
      font-family: inherit;
      font-size: 10px;
      transition: all 0.1s ease, border-color 0.15s ease;
      display: flex;
      align-items: center;
      justify-content: center;
      gap: 0.25rem;
      outline: none;
    }

    :root.v2 .btn-icon {
      border-color: var(--info);
      color: var(--info);
    }

    .btn-icon:hover:not(:disabled) {
      background-color: var(--border-color);
      color: var(--bg-main);
    }

    :root.v2 .btn-icon:hover:not(:disabled) {
      background-color: var(--info);
      color: var(--bg-main);
    }

    .btn-icon:disabled {
      opacity: 0.3;
      cursor: not-allowed;
    }

    /* Footers */
    footer {
      max-width: 1440px;
      margin: 2rem auto 0;
      padding: 0 2rem;
      border-top: 1px solid var(--border-color);
      padding-top: 1.5rem;
      transition: border-color 0.15s ease;
    }

    :root.v2 footer {
      border-color: var(--info);
    }

    .footer-row {
      display: flex;
      justify-content: space-between;
      align-items: center;
      flex-wrap: wrap;
      gap: 1.5rem;
    }

    .striped-box {
      background: repeating-linear-gradient(
        -45deg,
        transparent,
        transparent 5px,
        var(--border-color) 5px,
        var(--border-color) 6px
      );
      height: 40px;
      width: 100px;
      border: 1px solid var(--border-color);
      transition: border-color 0.15s ease;
    }

    :root.v2 .striped-box {
      border-color: var(--info);
      background: repeating-linear-gradient(
        -45deg,
        transparent,
        transparent 5px,
        var(--info) 5px,
        var(--info) 6px
      );
    }

    .pagination-dots {
      display: flex;
      gap: 6px;
      font-size: 12px;
      font-weight: bold;
    }

    .pagination-dots span {
      opacity: 0.3;
    }

    .pagination-dots span.active {
      opacity: 1;
      transition: color 0.15s ease;
    }

    :root.v2 .pagination-dots span.active {
      color: var(--info);
    }

    .logo-grid {
      opacity: 0.7;
      transition: color 0.15s ease;
    }

    :root.v2 .logo-grid {
      color: var(--info);
    }

    /* Details styling */
    .methodology-card {
      border: 1px solid var(--border-color);
      padding: 1rem;
      margin: 1.5rem 0;
      background: var(--bg-card);
      transition: border-color 0.15s ease;
    }

    :root.v2 .methodology-card {
      border-color: var(--info);
    }

    .methodology-card summary {
      font-weight: 700;
      cursor: pointer;
      outline: none;
      user-select: none;
      transition: color 0.15s ease;
    }

    :root.v2 .methodology-card summary {
      color: var(--info);
    }

    .methodology-content {
      margin-top: 0.75rem;
      font-size: 10px;
      color: var(--text-secondary);
      line-height: 1.5;
      border-top: 1px dashed var(--border-color);
      padding-top: 0.75rem;
      transition: border-color 0.15s ease;
    }

    :root.v2 .methodology-content {
      border-top-color: var(--info);
    }

    .methodology-content p {
      margin-bottom: 0.5rem;
    }

    .text-success {
      color: var(--success) !important;
      transition: color 0.15s ease;
    }

    .text-warning {
      color: var(--warning) !important;
      transition: color 0.15s ease;
    }

    .text-danger {
      color: var(--danger) !important;
      transition: color 0.15s ease;
    }

    .note {
      font-size: 9px;
      color: var(--text-muted);
      margin-top: 0.5rem;
    }
  </style>
</head>
<body>
  <script>
    (function() {
      const savedTheme = localStorage.getItem('theme');
      if (savedTheme === 'light' || (!savedTheme && window.matchMedia('(prefers-color-scheme: light)').matches)) {
        document.documentElement.classList.add('light');
      }
      const savedVersion = localStorage.getItem('version');
      if (savedVersion === 'v2') {
        document.documentElement.classList.add('v2');
      }
    })();
  </script>

  <header>
    <!-- Header Left (Parachute Wireframe & Lab Name) -->
    <div class="header-section">
      <div class="title-row">
        <svg viewBox="0 0 100 100" width="48" height="48" stroke="currentColor" stroke-width="1.5" fill="none">
          <rect x="15" y="15" width="70" height="35" />
          <path d="M 50 50 L 15 85 L 85 85 Z" />
          <circle cx="50" cy="50" r="10" />
          <path d="M 50 15 L 50 40" />
          <text x="20" y="80" fill="currentColor" font-size="8" stroke="none" font-family="monospace">SD</text>
          <text x="65" y="80" fill="currentColor" font-size="8" stroke="none" font-family="monospace">8.07</text>
        </svg>
        <div>
          <div class="label">3.11 LABS SP</div>
          <div class="sublabel">[SYNC ASSETS]</div>
          <div class="note">VERSION CONTROL REFS LOADED</div>
        </div>
      </div>
    </div>

    <!-- Header Center (Star Wireframe & Tool Title) -->
    <div class="header-section" style="align-items: center;">
      <div class="title-row">
        <svg viewBox="0 0 100 100" width="48" height="48" stroke="currentColor" stroke-width="1.2" fill="none">
          <path d="M 50 10 L 50 90 M 10 50 L 90 50 M 22 22 L 78 78 M 78 22 L 22 78 M 35 15 L 65 85 M 65 15 L 35 85" />
        </svg>
        <div>
          <div class="label">BYTO GATEWAY</div>
          <div class="sublabel">[PERFORMANCE BENCHMARK]</div>
          <div class="note">CORE LOADED STATUS: ESTÁVEL</div>
        </div>
      </div>
    </div>

    <!-- Header Right (ruler slider and buttons) -->
    <div class="header-section" style="align-items: flex-end;">
      <div>
        <span>FRAME RATE 24 FPS</span>
        <button id="versionToggle" class="btn-text" style="margin-left: 10px;">[ THEME: V1 (MONO) ]</button>
        <button id="themeToggle" class="btn-text" style="margin-left: 6px;">[ INVERT COLOR ]</button>
      </div>
      
      <div style="border: 1px solid var(--border-color); padding: 2px 8px; font-weight: bold; margin: 4px 0 2px 0;">
        FINAL REPORT
      </div>

      <div class="ruler-container" style="align-items: flex-end;">
        <div class="ruler">|...|...|...|...|...|...|</div>
        <div class="ruler-arrow" style="margin-right: 28px;">▼</div>
      </div>
      
      <div class="note">SYNC APPROVED DIRECTOR SIGN_OFF</div>
    </div>
  </header>

  <main>
    <!-- KPI metrics grid -->
    <div class="metrics-grid">
      <div class="kpi-card">
        <span class="kpi-label">TOTAL MEASURED RUNS</span>
        <div class="kpi-value" id="totalRequests">--</div>
        <div class="kpi-sublabel">REQS LOADED THROUGH GATEWAY</div>
      </div>
      <div class="kpi-card">
        <span class="kpi-label">MODELS TESTED</span>
        <div class="kpi-value" id="modelCount">--</div>
        <div class="kpi-sublabel">PROVIDERS ENROLLED</div>
      </div>
      <div class="kpi-card">
        <span class="kpi-label">GLOBAL SUCCESS RATE</span>
        <div class="kpi-value" id="overallSuccess">--</div>
        <div class="kpi-sublabel">STABILITY COEFFICIENT</div>
      </div>
      <!-- Diagnostic Checklist (Matches checkbox look in screenshot) -->
      <div class="kpi-card" style="grid-column: span 2;">
        <span class="kpi-label">DIAGNOSTIC STATUS CHECK</span>
        <div class="checklist-grid">
          <div>[X] SYNC: ESTÁVEL</div>
          <div>[X] ADMISSION LIMITS loaded</div>
          <div>[X] k6 SIMULATOR STABLE</div>
          <div>[X] HEADERS IDENTIFIED</div>
          <div class="text-danger">[ ] FAILURES OCCURRED</div>
          <div class="text-warning">[ ] OVERLOAD SLOTS FLOODED</div>
        </div>
      </div>
    </div>

    <!-- Navigation Tabs -->
    <div class="tabs-container">
      <button class="tab-btn active" data-tab="capacityTab">[ 01 // CAPACITY & CONCURRENCY ]</button>
      <button class="tab-btn" data-tab="queueTab">[ 02 // QUEUE SWEEP ]</button>
      <button class="tab-btn" data-tab="productionQueueTab">[ 03 // PRODUCTION QUEUE BURST ]</button>
      <button class="tab-btn" data-tab="retryQueueTab">[ 04 // REAL RETRY QUEUE SLA ]</button>
      <button class="tab-btn" data-tab="logsTab">[ 05 // LOGS EXPLORER ]</button>
    </div>

    <!-- Tab 1: Capacity Sweep -->
    <div id="capacityTab" class="tab-panel active">
      <div class="charts-grid">
        <div class="chart-card">
          <h2>OBSERVED SAFE CONCURRENCY</h2>
          <p class="subtitle">MAX TESTED LOAD PER MODEL WITH &ge;95% OK RATE AND ZERO OVERLOADS. ">= 64" MEANS NO CEILING WAS REACHED.</p>
          <div class="chart-container">
            <canvas id="capacityChart"></canvas>
          </div>
        </div>
        <div class="chart-card">
          <h2>P95 RESPONSE LATENCY TRENDS</h2>
          <p class="subtitle">P95 RESPONSE TIME BY TARGET K6 CONCURRENCY LEVEL.</p>
          <div class="chart-container">
            <canvas id="latencyChart"></canvas>
          </div>
        </div>
      </div>

      <div class="chart-card" style="margin-bottom: 1.5rem;">
        <h2>AIMD LIMIT GROWTH OVER CONCURRENCY</h2>
        <p class="subtitle">MAX GATEWAY PER-MODEL CONCURRENCY LIMIT OBSERVED IN EACH STEP.</p>
        <div class="chart-container" style="height: 250px;">
          <canvas id="successChart"></canvas>
        </div>
      </div>
      
      <div class="table-card">
        <div class="table-header-row">
          <h2>CAPACITY DIAGNOSTIC MATRIX</h2>
          <div class="table-actions">
            <input type="text" id="capacitySearch" class="search-input" placeholder="filter by model...">
          </div>
        </div>
        <div class="table-wrapper">
          <table id="capacityTable">
            <thead>
              <tr>
                <th onclick="sortTable('capacityTable', 0)">MODEL</th>
                <th onclick="sortTable('capacityTable', 1)" class="text-right">CONC</th>
                <th onclick="sortTable('capacityTable', 2)" class="text-right">REQS</th>
                <th onclick="sortTable('capacityTable', 3)" class="text-right">OK</th>
                <th onclick="sortTable('capacityTable', 4)" class="text-right">IMMEDIATE</th>
                <th onclick="sortTable('capacityTable', 5)" class="text-right">QUEUED</th>
                <th onclick="sortTable('capacityTable', 6)" class="text-right">429</th>
                <th onclick="sortTable('capacityTable', 7)">SUCCESS RATE</th>
                <th onclick="sortTable('capacityTable', 8)" class="text-right">P50 LAT</th>
                <th onclick="sortTable('capacityTable', 9)" class="text-right">P95 LAT</th>
                <th onclick="sortTable('capacityTable', 10)" class="text-right">P99 LAT</th>
                <th onclick="sortTable('capacityTable', 11)" class="text-right">P95 QUEUE</th>
                <th onclick="sortTable('capacityTable', 12)" class="text-right">LIMIT START→END</th>
                <th onclick="sortTable('capacityTable', 13)" class="text-right">LIMIT MAX</th>
                <th onclick="sortTable('capacityTable', 14)" class="text-right">Q DEPTH MAX</th>
                <th onclick="sortTable('capacityTable', 15)">CODES</th>
              </tr>
            </thead>
            <tbody id="capacityTableBody">
              <!-- Dynamically populated -->
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <!-- Tab 2: Queue Sweep -->
    <div id="queueTab" class="tab-panel">
      <div class="charts-grid">
        <div class="chart-card">
          <h2>QUEUE WAIT TIMES (AVG VS P95)</h2>
          <p class="subtitle">TIME HELD BEFORE ADMISSION. QUEUED=HEADER WAIT &gt; 0; IMMEDIATE=NO WAIT.</p>
          <div class="chart-container">
            <canvas id="queueChart"></canvas>
          </div>
        </div>
        <div class="chart-card">
          <h2>QUEUE DEPTH OBSERVED VS MAX</h2>
          <p class="subtitle">MAX REMAINING QUEUE DEPTH SEEN AT ADMISSION AGAINST CONFIGURED BOUND.</p>
          <div class="chart-container">
            <canvas id="queueSuccessChart"></canvas>
          </div>
        </div>
      </div>
      
      <div class="table-card">
        <div class="table-header-row">
          <h2>QUEUE & AIMD SUMMARY</h2>
          <div class="table-actions">
            <input type="text" id="queueSearch" class="search-input" placeholder="filter by model...">
          </div>
        </div>
        <div class="table-wrapper">
          <table id="queueTable">
            <thead>
              <tr>
                <th onclick="sortTable('queueTable', 0)">MODEL</th>
                <th onclick="sortTable('queueTable', 1)" class="text-right">REQS</th>
                <th onclick="sortTable('queueTable', 2)" class="text-right">OK</th>
                <th onclick="sortTable('queueTable', 3)" class="text-right">IMMEDIATE</th>
                <th onclick="sortTable('queueTable', 4)" class="text-right">QUEUED</th>
                <th onclick="sortTable('queueTable', 5)">SUCCESS RATE</th>
                <th onclick="sortTable('queueTable', 6)" class="text-right">P50 QUEUE</th>
                <th onclick="sortTable('queueTable', 7)" class="text-right">P95 QUEUE</th>
                <th onclick="sortTable('queueTable', 8)" class="text-right">P99 QUEUE</th>
                <th onclick="sortTable('queueTable', 9)" class="text-right">MAX QUEUE</th>
                <th onclick="sortTable('queueTable', 10)" class="text-right">Q DEPTH MAX/MAX</th>
                <th onclick="sortTable('queueTable', 11)" class="text-right">LIMIT MIN→MAX</th>
                <th onclick="sortTable('queueTable', 12)" class="text-right">P95 LATENCY</th>
                <th onclick="sortTable('queueTable', 13)" class="text-right">429</th>
              </tr>
            </thead>
            <tbody id="queueTableBody">
              <!-- Dynamically populated -->
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <!-- Tab 3: Production Queue Burst -->
    <div id="productionQueueTab" class="tab-panel">
      <div class="metrics-grid" id="productionConclusions">
        <!-- Dynamically populated -->
      </div>

      <div class="charts-grid">
        <div class="chart-card">
          <h2>1000 REQUEST BURST QUEUE CURVE</h2>
          <p class="subtitle">P50/P75/P90/P95/P99/MAX QUEUE WAIT UNDER A LARGE BACKLOG.</p>
          <div class="chart-container">
            <canvas id="productionQueueChart"></canvas>
          </div>
        </div>
        <div class="chart-card">
          <h2>ADAPTIVE LIMIT VS QUEUE DEPTH</h2>
          <p class="subtitle">MAX AIMD LIMIT OBSERVED COMPARED WITH MAX REMAINING QUEUE DEPTH AT ADMISSION.</p>
          <div class="chart-container">
            <canvas id="productionDepthChart"></canvas>
          </div>
        </div>
      </div>

      <div class="table-card">
        <div class="table-header-row">
          <h2>PRODUCTION QUEUE BURST SUMMARY</h2>
        </div>
        <div class="table-wrapper">
          <table id="productionQueueTable">
            <thead>
              <tr>
                <th>MODEL</th>
                <th class="text-right">REQS</th>
                <th class="text-right">OK</th>
                <th class="text-right">429</th>
                <th class="text-right">CLIENT TIMEOUT</th>
                <th class="text-right">ADMITTED</th>
                <th class="text-right">IMMEDIATE</th>
                <th class="text-right">QUEUED</th>
                <th class="text-right">P50 Q</th>
                <th class="text-right">P90 Q</th>
                <th class="text-right">P95 Q</th>
                <th class="text-right">P99 Q</th>
                <th class="text-right">MAX Q</th>
                <th class="text-right">P95 LAT</th>
                <th class="text-right">LIMIT MIN→MAX</th>
                <th class="text-right">MAX Q DEPTH</th>
                <th>STATUS COUNTS</th>
              </tr>
            </thead>
            <tbody id="productionQueueTableBody"></tbody>
          </table>
        </div>
        <div class="note">THIS BURST IS DESIGNED TO SHOW QUEUE WAIT GROWTH. ADMITTED MEANS THE REQUEST RECEIVED A GATEWAY PERMIT; QUEUED MEANS X-BYTO-QUEUE-WAIT-MS &gt; 0. 429 MEANS VERTEX STILL RETURNED RESOURCE EXHAUSTION OR QUOTA/RATE LIMIT AFTER ADMISSION. STATUS 0 MEANS K6 TIMED OUT BEFORE RECEIVING AN HTTP RESPONSE.</div>
      </div>
    </div>

    <!-- Tab 4: Real Retry Queue SLA -->
    <div id="retryQueueTab" class="tab-panel">
      <div class="metrics-grid" id="retryQueueConclusions">
        <!-- Dynamically populated -->
      </div>

      <div class="table-card">
        <div class="table-header-row">
          <h2>RETRY-ENABLED 8S QUEUE-WAIT THRESHOLDS</h2>
        </div>
        <div class="table-wrapper">
          <table id="retryQueueTable">
            <thead>
              <tr>
                <th>MODEL</th>
                <th class="text-right">QUEUE ≤8S LEVEL</th>
                <th class="text-right">CLEAN 200 LEVEL</th>
                <th class="text-right">CROSSING LEVEL</th>
                <th class="text-right">MAX Q WAIT @ SAFE</th>
                <th class="text-right">P95 Q WAIT @ SAFE</th>
                <th class="text-right">P95 LAT @ SAFE</th>
                <th class="text-right">OK @ SAFE</th>
                <th>STATUS @ SAFE</th>
                <th>STATUS @ CROSSING</th>
              </tr>
            </thead>
            <tbody id="retryQueueTableBody"></tbody>
          </table>
        </div>
        <div class="note">THIS IS THE PRODUCTION-REALISTIC RUN: VERTEX RETRIES ENABLED WITH EXPONENTIAL BACKOFF. QUEUE ≤8S LEVEL ALLOWS RETRIED 429S IF THEY OCCURRED; CLEAN 200 LEVEL REQUIRES EVERY REQUEST AT THAT LEVEL TO COMPLETE SUCCESSFULLY.</div>
      </div>
    </div>

    <!-- Tab 4: Raw Logs Explorer -->
    <div id="logsTab" class="tab-panel">
      <div class="table-card">
        <div class="table-header-row">
          <h2>CONSOLE RAW RUN LOGGER</h2>
          <div class="table-actions">
            <input type="text" id="rawSearch" class="search-input" placeholder="search model or status...">
            <select id="rawPhaseFilter" class="search-input" style="min-width: 100px;">
              <option value="all">ALL PHASES</option>
              <option value="capacity">CAPACITY</option>
              <option value="queue">QUEUE</option>
              <option value="production_queue">PRODUCTION QUEUE</option>
            </select>
            <select id="rawStatusFilter" class="search-input" style="min-width: 100px;">
              <option value="all">ALL STATUSES</option>
              <option value="200">200 OK</option>
              <option value="429">429 OVL</option>
              <option value="errors">ERRORS</option>
            </select>
          </div>
        </div>
        <div class="table-wrapper">
          <table id="rawLogsTable">
            <thead>
              <tr>
                <th>TIMESTAMP</th>
                <th>PHASE</th>
                <th>MODEL</th>
                <th class="text-right">CONC</th>
                <th class="text-right">VU</th>
                <th class="text-right">ITER</th>
                <th class="text-right">STATUS</th>
                <th class="text-right">LATENCY</th>
                <th class="text-right">Q_WAIT</th>
                <th class="text-right">IN_FLIGHT</th>
                <th class="text-right">LIMIT</th>
                <th class="text-right">Q_DEPTH</th>
                <th>RESULT</th>
              </tr>
            </thead>
            <tbody id="rawLogsTableBody">
              <!-- Dynamically populated -->
            </tbody>
          </table>
        </div>
        
        <div class="pagination-container">
          <div id="paginationInfo">SHOWING 0-0 OF 0 RESULTS</div>
          <div class="pagination-buttons">
            <button id="prevPage" class="btn-icon">PREV</button>
            <button id="nextPage" class="btn-icon">NEXT</button>
          </div>
        </div>
      </div>
    </div>

    <!-- Collapsible methodology -->
    <details class="methodology-card">
      <summary>[+] METHODOLOGY & BENCHMARK SPECS</summary>
      <div class="methodology-content">
        <p>CAPACITY SWEEP: EACH ENABLED+AVAILABLE GEMINI MODEL WAS RUN THROUGH STEPPED K6 CONCURRENCY LEVELS. EACH LEVEL USED MULTIPLE REQUEST WAVES, NOT A SINGLE SHOT, SO THE ADAPTIVE LIMITER COULD MOVE.</p>
        <p>AIMD: THE GATEWAY INCREASES PER-MODEL CONCURRENCY AFTER CLEAN COMPLETIONS AND SHRINKS IT WHEN VERTEX RETURNS RESOURCE EXHAUSTION. THE QUEUE MAX DEPTH DOES NOT ADAPT; IT IS A FIXED CONFIGURED BOUND. THIS REPORT SHOWS LIMIT START/END/MIN/MAX SEPARATELY FROM QUEUE DEPTH.</p>
        <p>QUEUED REQUEST: ANY REQUEST WITH X-BYTO-QUEUE-WAIT-MS &gt; 0. IMMEDIATE REQUEST: ADMITTED WITHOUT WAIT. P50/P95/P99 ARE PERCENTILES; P50 IS THE MEDIAN.</p>
        <p id="runMetaLine">RUN CONFIGURATION LOADING...</p>
      </div>
    </details>
  </main>

  <footer>
    <div class="footer-row">
      <!-- Decorative Stripe (matches screenshot left stripe) -->
      <div class="striped-box"></div>
      
      <!-- Pagination Dot Indicator (matches bottom dots in screenshot) -->
      <div class="pagination-dots">
        <span>o</span><span>o</span><span>o</span><span>o</span><span class="active">o</span><span>o</span><span>o</span><span>o</span><span>o</span><span>o</span><span>o</span><span>o</span><span>o</span><span>o</span><span>o</span><span>o</span><span>o</span><span>o</span><span>o</span><span>o</span><span>o</span>
      </div>

      <!-- Lab Logo (matches bottom right screenshot logo) -->
      <div class="logo-grid">
        <svg width="24" height="24" viewBox="0 0 24 24" fill="currentColor">
          <rect x="2" y="2" width="4" height="4" />
          <rect x="10" y="2" width="4" height="4" />
          <rect x="18" y="2" width="4" height="4" />
          <rect x="2" y="10" width="4" height="4" />
          <rect x="10" y="10" width="4" height="4" />
          <rect x="18" y="10" width="4" height="4" />
          <rect x="2" y="18" width="4" height="4" />
          <rect x="10" y="18" width="4" height="4" />
          <rect x="18" y="18" width="4" height="4" />
        </svg>
      </div>
    </div>
  </footer>

  <script>
    const bench = __BENCH_DATA__;
    const fmt = new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 });
    const pct = (v) => {
      if (v === undefined || isNaN(v)) return '0.0%';
      return `${(v * 100).toFixed(1)}%`;
    };
    
    const models = [...new Set(bench.summary.map((d) => d.model))];
    const levels = [...new Set(bench.summary.map((d) => d.concurrency))].sort((a, b) => a - b);
    
    // Core KPIs
    document.getElementById("totalRequests").textContent = (bench.raw ? bench.raw.length : 0) + (bench.queue_raw ? bench.queue_raw.length : 0) + (bench.production_queue_raw ? bench.production_queue_raw.length : 0);
    document.getElementById("modelCount").textContent = models.length;
    
    const totalRaw = (bench.raw ? bench.raw.length : 0) + (bench.queue_raw ? bench.queue_raw.length : 0) + (bench.production_queue_raw ? bench.production_queue_raw.length : 0);
    const okRaw = (bench.raw ? bench.raw.filter(d => d.ok).length : 0) + (bench.queue_raw ? bench.queue_raw.filter(d => d.ok).length : 0) + (bench.production_queue_raw ? bench.production_queue_raw.filter(d => d.ok).length : 0);
    document.getElementById("overallSuccess").textContent = totalRaw ? pct(okRaw / totalRaw) : '0.0%';
    const meta = bench.meta || {};
    const metaLine = document.getElementById("runMetaLine");
    if (metaLine) {
      const prod = meta.production_queue || {};
      const prodModelText = prod.models ? `${prod.models.length} MODELS` : (prod.model || 'N/A');
      const prodRequestText = prod.total_requests || prod.requests || prod.requests_per_model || '';
      const prodText = prodRequestText ? ` // PRODUCTION_BURST=${prodModelText} REQUESTS=${prodRequestText} VUS=${prod.concurrency} AIMD=${prod.adaptive_initial || '?'}→${prod.adaptive_max || '?'} QUEUE_MAX=${prod.queue_max || '?'} MAX_WAIT_MS=${prod.queue_max_wait_ms || '?'} K6_HTTP_TIMEOUT=${prod.k6_http_timeout || 'n/a'}` : '';
      metaLine.textContent = `RUN CONFIG: LEVELS=${(meta.levels || []).join(',')} ITERATIONS=${meta.iterations || 'n/a'} RETRIES=${meta.vertex_retry_max_attempts || 'n/a'} AIMD=${meta.adaptive_initial || '?'}→${meta.adaptive_max || '?'} QUEUE_MAX=${meta.queue_max || '?'} STOP_ON_FAILURE=${meta.stop_on_failure ? 'YES' : 'NO'}${prodText}`;
    }

    // Expandable detail row handler
    window.toggleRowDetail = function(id) {
      const row = document.getElementById(id);
      if (!row) return;
      const content = row.querySelector('.detail-content');
      if (row.style.display === 'none') {
        row.style.display = 'table-row';
        setTimeout(() => {
          content.style.maxHeight = '300px';
        }, 10);
      } else {
        content.style.maxHeight = '0';
        setTimeout(() => {
          row.style.display = 'none';
        }, 250);
      }
    };

    // Table character progress bar
    function renderProgressBar(successRate) {
      const filled = Math.round(successRate * 10);
      const empty = 10 - filled;
      const progress = '█'.repeat(filled) + '░'.repeat(empty);
      
      let cls = 'text-success';
      if (successRate < 0.95 && successRate >= 0.75) cls = 'text-warning';
      else if (successRate < 0.75) cls = 'text-danger';
      
      return `<span class="${cls}">[${progress}] ${pct(successRate)}</span>`;
    }

    // Format error codes
    function formatErrorCodes(codesObj) {
      if (!codesObj || Object.keys(codesObj).length === 0) return `[ NONE ]`;
      return Object.entries(codesObj).map(([code, count]) => {
        let cls = 'badge-danger';
        if (code === '200') cls = 'badge-success';
        else if (code === '429') cls = 'badge-warning';
        return `<span class="badge ${cls}">[ ${code} : ${count} ]</span>`;
      }).join(' ');
    }

    // Populate tables
    function populateCapacityTable() {
      const query = document.getElementById('capacitySearch').value.toLowerCase();
      const filtered = bench.summary.filter(d => d.model.toLowerCase().includes(query));
      
      const tbody = document.getElementById('capacityTableBody');
      tbody.innerHTML = filtered.map(d => `
        <tr>
          <td><span class="model-badge">${d.model}</span></td>
          <td class="text-right font-mono">${d.concurrency}</td>
          <td class="text-right font-mono">${d.requests}</td>
          <td class="text-right font-mono text-success">[ ${d.ok} ]</td>
          <td class="text-right font-mono">[ ${d.immediate} ]</td>
          <td class="text-right font-mono ${d.queued ? 'text-warning' : ''}">[ ${d.queued} ]</td>
          <td class="text-right font-mono ${d.overload ? 'text-warning' : ''}">[ ${d.overload} ]</td>
          <td class="font-mono">${renderProgressBar(d.success_rate)}</td>
          <td class="text-right font-mono">${fmt.format(d.p50_ms)} MS</td>
          <td class="text-right font-mono">${fmt.format(d.p95_ms)} MS</td>
          <td class="text-right font-mono">${fmt.format(d.p99_ms)} MS</td>
          <td class="text-right font-mono">${fmt.format(d.p95_queue_ms)} MS</td>
          <td class="text-right font-mono">${d.limit_start}→${d.limit_end}</td>
          <td class="text-right font-mono">${d.limit_max}</td>
          <td class="text-right font-mono">${d.max_queue_depth}/${d.queue_max || '-'}</td>
          <td>${formatErrorCodes(d.codes)}</td>
        </tr>
      `).join('');
    }

    // Sort Tables logic (In-memory array sorting)
    let capacitySort = { col: null, asc: true };
    let queueSort = { col: null, asc: true };

    window.sortTable = function(tableId, colIndex) {
      const isCapacity = tableId === 'capacityTable';
      const sortState = isCapacity ? capacitySort : queueSort;
      
      if (sortState.col === colIndex) {
        sortState.asc = !sortState.asc;
      } else {
        sortState.col = colIndex;
        sortState.asc = true;
      }
      
      const table = document.getElementById(tableId);
      table.querySelectorAll('th').forEach(th => th.classList.remove('sort-asc', 'sort-desc'));
      const activeHeader = table.querySelectorAll('th')[colIndex];
      activeHeader.classList.add(sortState.asc ? 'sort-asc' : 'sort-desc');
      
      const dataList = isCapacity ? bench.summary : bench.queue_summary;
      dataList.sort((a, b) => {
        let valA, valB;
        
        if (isCapacity) {
          const fields = [
            'model', 'concurrency', 'requests', 'ok', 'immediate', 'queued',
            'overload', 'success_rate', 'p50_ms', 'p95_ms', 'p99_ms',
            'p95_queue_ms', 'limit_end', 'limit_max', 'max_queue_depth', 'codes'
          ];
          const field = fields[colIndex];
          if (field === 'codes') {
            valA = Object.keys(a.codes).length;
            valB = Object.keys(b.codes).length;
          } else {
            valA = a[field];
            valB = b[field];
          }
        } else {
          const fields = [
            'model', 'requests', 'ok', 'immediate', 'queued', 'success_rate',
            'p50_queue_ms', 'p95_queue_ms', 'p99_queue_ms', 'max_queue_ms',
            'max_queue_depth', 'limit_max', 'p95_latency_ms', 'overload'
          ];
          valA = a[fields[colIndex]];
          valB = b[fields[colIndex]];
        }
        
        if (typeof valA === 'string') {
          return sortState.asc ? valA.localeCompare(valB) : valB.localeCompare(valA);
        } else {
          return sortState.asc ? valA - valB : valB - valA;
        }
      });
      
      if (isCapacity) populateCapacityTable();
      else populateQueueTable();
    };

    function populateQueueTable() {
      const query = document.getElementById('queueSearch').value.toLowerCase();
      const filtered = bench.queue_summary.filter(d => d.model.toLowerCase().includes(query));
      
      const tbody = document.getElementById('queueTableBody');
      tbody.innerHTML = filtered.map(d => `
        <tr>
          <td><span class="model-badge">${d.model}</span></td>
          <td class="text-right font-mono">${d.requests}</td>
          <td class="text-right font-mono text-success">[ ${d.ok} ]</td>
          <td class="text-right font-mono">[ ${d.immediate} ]</td>
          <td class="text-right font-mono ${d.queued ? 'text-warning' : ''}">[ ${d.queued} ]</td>
          <td class="font-mono">${renderProgressBar(d.success_rate)}</td>
          <td class="text-right font-mono">${fmt.format(d.p50_queue_ms)} MS</td>
          <td class="text-right font-mono">${fmt.format(d.p95_queue_ms)} MS</td>
          <td class="text-right font-mono">${fmt.format(d.p99_queue_ms)} MS</td>
          <td class="text-right font-mono">${fmt.format(d.max_queue_ms)} MS</td>
          <td class="text-right font-mono">${d.max_queue_depth}/${d.queue_max || '-'}</td>
          <td class="text-right font-mono">${d.limit_min}→${d.limit_max}</td>
          <td class="text-right font-mono">${fmt.format(d.p95_latency_ms)} MS</td>
          <td class="text-right font-mono ${d.overload ? 'text-warning' : ''}">[ ${d.overload} ]</td>
        </tr>
      `).join('');
    }

    // Raw Logger
    let logState = {
      page: 1,
      pageSize: 25,
      query: '',
      phase: 'all',
      status: 'all'
    };

    const rawLogs = [
      ...(bench.raw || []).map(r => ({ ...r, phase: 'capacity' })),
      ...(bench.queue_raw || []).map(r => ({ ...r, phase: 'queue' })),
      ...(bench.production_queue_raw || []).map(r => ({ ...r, phase: 'production_queue' }))
    ].sort((a, b) => new Date(b.ts) - new Date(a.ts));

    function populateRawLogsTable() {
      const tbody = document.getElementById('rawLogsTableBody');
      if (!tbody) return;

      const filtered = rawLogs.filter(log => {
        const matchesSearch = log.model.toLowerCase().includes(logState.query) || 
                              (log.error_code && log.error_code.toLowerCase().includes(logState.query)) ||
                              String(log.status).includes(logState.query);
        const matchesPhase = logState.phase === 'all' || log.phase === logState.phase;
        
        let matchesStatus = true;
        if (logState.status === '200') matchesStatus = log.status === 200;
        else if (logState.status === '429') matchesStatus = log.status === 429;
        else if (logState.status === 'errors') matchesStatus = log.status !== 200 && log.status !== 429;
        
        return matchesSearch && matchesPhase && matchesStatus;
      });

      const total = filtered.length;
      const start = (logState.page - 1) * logState.pageSize;
      const end = Math.min(start + logState.pageSize, total);
      const paginated = filtered.slice(start, end);

      const infoEl = document.getElementById('paginationInfo');
      if (total === 0) {
        infoEl.textContent = 'SHOWING 0 OF 0 RESULTS';
        document.getElementById('prevPage').disabled = true;
        document.getElementById('nextPage').disabled = true;
        tbody.innerHTML = '<tr><td colspan="13" style="text-align: center; padding: 2rem;">NO RECORD LOGS LOADED</td></tr>';
        return;
      }

      infoEl.textContent = `SHOWING ${start + 1}-${end} OF ${total} LOGS`;
      document.getElementById('prevPage').disabled = logState.page === 1;
      document.getElementById('nextPage').disabled = end >= total;

      tbody.innerHTML = paginated.map((log, index) => {
        const badgeClass = log.ok ? 'badge-success' : (log.status === 429 ? 'badge-warning' : 'badge-danger');
        const resultText = log.ok ? 'SUCCESS' : (log.status === 429 ? 'OVERLOAD' : 'FAILURE');
        const formattedTs = new Date(log.ts).toLocaleTimeString();
        const detailId = `detail-${start + index}`;

        return `
          <tr class="expandable-row" onclick="toggleRowDetail('${detailId}')">
            <td class="font-mono">${formattedTs}</td>
            <td><span class="badge ${log.phase === 'capacity' ? 'badge-info' : 'badge-warning'}">[ ${log.phase} ]</span></td>
            <td><span class="model-badge">${log.model}</span></td>
            <td class="text-right font-mono">${log.concurrency}</td>
            <td class="text-right font-mono">${log.vu}</td>
            <td class="text-right font-mono">${log.iter}</td>
            <td class="text-right font-mono">${log.status}</td>
            <td class="text-right font-mono">${log.duration_ms} MS</td>
            <td class="text-right font-mono">${log.queue_wait_ms} MS</td>
            <td class="text-right font-mono">${log.in_flight}</td>
            <td class="text-right font-mono">${log.limit || 0}</td>
            <td class="text-right font-mono">${log.queue_depth || 0}/${log.queue_max || '-'}</td>
            <td><span class="badge ${badgeClass}">${resultText}</span></td>
          </tr>
          <tr class="detail-row" id="${detailId}" style="display: none;">
            <td colspan="13">
              <div class="detail-content">
                <div class="detail-content-inner">
                  <pre>${JSON.stringify(log, null, 2)}</pre>
                </div>
              </div>
            </td>
          </tr>
        `;
      }).join('');
    }

    // Chart Oscilloscope Styling
    let capacityChart, successChart, latencyChart, queueChart, queueSuccessChart, productionQueueChart, productionDepthChart;

    const monoColors = [
      "#ffffff", // High Contrast White
      "#d4d3cb", // Warm Cream
      "#a3a29b", // Mid Warm Gray
      "#73726c", // Darker Gray
      "#ffffff", // Loop back
      "#d4d3cb",
      "#a3a29b",
      "#73726c"
    ];

    const v2Colors = [
      "#39ff14", // Phosphor Green
      "#00f3ff", // Cyan
      "#ffb000", // Amber
      "#ff55ff", // Magenta
      "#ff5555", // Red
      "#ffff55", // Yellow
      "#00ffaa", // Teal
      "#aa55ff"  // Violet
    ];

    const pointStyles = ['circle', 'rect', 'triangle', 'rectRot', 'star', 'cross', 'circle', 'rect'];
    const borderDashes = [
      [],
      [4, 4],
      [2, 2],
      [8, 4],
      [6, 2, 2, 2],
      [12, 4],
      [1, 5],
      [3, 3, 9, 3]
    ];

    function initCharts() {
      if (!bench.summary || bench.summary.length === 0) return;

      const isLight = document.documentElement.classList.contains('light');
      const isV2 = document.documentElement.classList.contains('v2');
      const textColor = isLight ? '#000000' : '#d4d3cb';
      const gridColor = isLight ? '#cbcabf' : '#1c1c1a';
      
      const activePalette = isV2 ? v2Colors : monoColors;
      
      // 1. Observed Capacity Horizontal Bar Chart
      const capCtx = document.getElementById('capacityChart').getContext('2d');
      capacityChart = new Chart(capCtx, {
        type: 'bar',
        data: {
          labels: bench.capacity.map(d => d.model),
          datasets: [{
            label: 'Safe Concurrency Limit',
            data: bench.capacity.map(d => d.observed_capacity),
            backgroundColor: isV2 
              ? (isLight ? '#0066aa' : '#00f3ff')
              : (isLight ? '#000000' : '#d4d3cb'),
            borderColor: isV2
              ? (isLight ? '#0066aa' : '#00f3ff')
              : (isLight ? '#000000' : '#ffffff'),
            borderWidth: 1,
            borderRadius: 0,
            barThickness: 10
          }]
        },
        options: {
          indexAxis: 'y',
          responsive: true,
          maintainAspectRatio: false,
          plugins: {
            legend: { display: false },
            tooltip: {
              backgroundColor: isLight ? '#dfded8' : '#000000',
              titleColor: textColor,
              bodyColor: textColor,
              borderColor: textColor,
              borderWidth: 1,
              cornerRadius: 0
            }
          },
          scales: {
            x: {
              beginAtZero: true,
              title: { display: true, text: 'CONCURRENT REQS (VUs)', color: textColor, font: { family: 'monospace', size: 10 } },
              ticks: { color: textColor, font: { family: 'monospace', size: 8 } },
              grid: { color: gridColor }
            },
            y: {
              ticks: { color: textColor, font: { family: 'monospace', size: 8 } },
              grid: { display: false }
            }
          }
        }
      });

      // 2. AIMD limit growth grouped bar chart
      const successCtx = document.getElementById('successChart').getContext('2d');
      successChart = new Chart(successCtx, {
        type: 'bar',
        data: {
          labels: models,
          datasets: levels.map((level, i) => ({
            label: `CONC = ${level}`,
            data: models.map(model => {
              const row = bench.summary.find(d => d.model === model && d.concurrency === level);
              return row ? row.limit_max : 0;
            }),
            backgroundColor: activePalette[i % activePalette.length],
            borderColor: textColor,
            borderWidth: 1,
            borderRadius: 0,
            barThickness: 6
          }))
        },
        options: {
          responsive: true,
          maintainAspectRatio: false,
          plugins: {
            legend: { position: 'bottom', labels: { color: textColor, font: { family: 'monospace', size: 8 }, boxWidth: 10 } }
          },
          scales: {
            x: { ticks: { color: textColor, font: { family: 'monospace', size: 8 } }, grid: { display: false } },
            y: {
              beginAtZero: true,
              title: { display: true, text: 'AIMD LIMIT', color: textColor, font: { family: 'monospace', size: 10 } },
              ticks: { color: textColor, font: { family: 'monospace', size: 8 } },
              grid: { color: gridColor }
            }
          }
        }
      });

      // 3. P95 Latency Progression Line Chart
      const latCtx = document.getElementById('latencyChart').getContext('2d');
      latencyChart = new Chart(latCtx, {
        type: 'line',
        data: {
          labels: levels.map(l => `C=${l}`),
          datasets: models.map((model, i) => ({
            label: model,
            data: levels.map(level => {
              const row = bench.summary.find(d => d.model === model && d.concurrency === level);
              return row ? row.p95_ms : null;
            }),
            borderColor: activePalette[i % activePalette.length],
            backgroundColor: 'transparent',
            borderWidth: 1.5,
            borderDash: borderDashes[i % borderDashes.length],
            pointStyle: pointStyles[i % pointStyles.length],
            pointRadius: 4,
            pointHoverRadius: 6,
            tension: 0,
            spanGaps: true
          }))
        },
        options: {
          responsive: true,
          maintainAspectRatio: false,
          plugins: {
            legend: { position: 'bottom', labels: { color: textColor, font: { family: 'monospace', size: 8 }, boxWidth: 15 } }
          },
          scales: {
            x: { ticks: { color: textColor, font: { family: 'monospace', size: 8 } }, grid: { display: false } },
            y: {
              beginAtZero: true,
              title: { display: true, text: 'LATENCY (MS)', color: textColor, font: { family: 'monospace', size: 10 } },
              ticks: { color: textColor, font: { family: 'monospace', size: 8 } },
              grid: { color: gridColor }
            }
          }
        }
      });

      // 4. Queue Wait times bar chart (Avg & P95)
      const qCtx = document.getElementById('queueChart').getContext('2d');
      const queueModels = bench.queue_summary.map(d => d.model);
      queueChart = new Chart(qCtx, {
        type: 'bar',
        data: {
          labels: queueModels,
          datasets: [
            {
              label: 'P95 Queue Wait',
              data: bench.queue_summary.map(d => d.p95_queue_ms),
              backgroundColor: isV2 
                ? '#ffb000' // Amber
                : (isLight ? '#000000' : '#ffffff'),
              borderColor: textColor,
              borderWidth: 1,
              borderRadius: 0,
              barThickness: 12
            },
            {
              label: 'Average Queue Wait',
              data: bench.queue_summary.map(d => d.avg_queue_ms),
              backgroundColor: isV2
                ? (isLight ? '#0066aa' : '#00f3ff') // Cyan
                : (isLight ? '#cbcabf' : '#73726c'),
              borderColor: textColor,
              borderWidth: 1,
              borderRadius: 0,
              barThickness: 12
            }
          ]
        },
        options: {
          responsive: true,
          maintainAspectRatio: false,
          plugins: {
            legend: { position: 'bottom', labels: { color: textColor, font: { family: 'monospace', size: 8 }, boxWidth: 10 } }
          },
          scales: {
            x: { ticks: { color: textColor, font: { family: 'monospace', size: 8 } }, grid: { display: false } },
            y: {
              beginAtZero: true,
              title: { display: true, text: 'QUEUE WAIT (MS)', color: textColor, font: { family: 'monospace', size: 10 } },
              ticks: { color: textColor, font: { family: 'monospace', size: 8 } },
              grid: { color: gridColor }
            }
          }
        }
      });

      // 5. Queue depth observed vs configured max
      const qSuccessCtx = document.getElementById('queueSuccessChart').getContext('2d');
      queueSuccessChart = new Chart(qSuccessCtx, {
        type: 'bar',
        data: {
          labels: queueModels,
          datasets: [
            {
              label: 'Max Observed Queue Depth',
              data: bench.queue_summary.map(d => d.max_queue_depth),
              backgroundColor: isV2
                ? (isLight ? '#008800' : '#39ff14')
                : (isLight ? '#000000' : '#d4d3cb'),
              borderColor: textColor,
              borderWidth: 1,
              borderRadius: 0,
              barThickness: 12
            },
            {
              label: 'Configured Queue Max',
              data: bench.queue_summary.map(d => d.queue_max),
              backgroundColor: isV2
                ? (isLight ? '#bb6600' : '#ffb000')
                : (isLight ? '#cbcabf' : '#73726c'),
              borderColor: textColor,
              borderWidth: 1,
              borderRadius: 0,
              barThickness: 12
            }
          ]
        },
        options: {
          responsive: true,
          maintainAspectRatio: false,
          plugins: {
            legend: { position: 'bottom', labels: { color: textColor, font: { family: 'monospace', size: 8 }, boxWidth: 10 } }
          },
          scales: {
            x: { ticks: { color: textColor, font: { family: 'monospace', size: 8 } }, grid: { display: false } },
            y: {
              beginAtZero: true,
              title: { display: true, text: 'QUEUE DEPTH', color: textColor, font: { family: 'monospace', size: 10 } },
              ticks: { color: textColor, font: { family: 'monospace', size: 8 } },
              grid: { color: gridColor }
            }
          }
        }
      });

      // 6. Production queue burst percentiles
      if (bench.production_queue_summary && bench.production_queue_summary.length > 0) {
        const prodModels = bench.production_queue_summary.map(d => d.model);
        const prodCtx = document.getElementById('productionQueueChart').getContext('2d');
        productionQueueChart = new Chart(prodCtx, {
          type: 'bar',
          data: {
            labels: ['P50', 'P75', 'P90', 'P95', 'P99', 'MAX'],
            datasets: bench.production_queue_summary.map((d, i) => ({
              label: d.model,
              data: [d.p50_queue_ms, d.p75_queue_ms, d.p90_queue_ms, d.p95_queue_ms, d.p99_queue_ms, d.max_queue_ms],
              backgroundColor: activePalette[i % activePalette.length],
              borderColor: textColor,
              borderWidth: 1,
              borderRadius: 0
            }))
          },
          options: {
            responsive: true,
            maintainAspectRatio: false,
            plugins: { legend: { position: 'bottom', labels: { color: textColor, font: { family: 'monospace', size: 8 }, boxWidth: 10 } } },
            scales: {
              x: { ticks: { color: textColor, font: { family: 'monospace', size: 8 } }, grid: { display: false } },
              y: {
                beginAtZero: true,
                title: { display: true, text: 'QUEUE WAIT (MS)', color: textColor, font: { family: 'monospace', size: 10 } },
                ticks: { color: textColor, font: { family: 'monospace', size: 8 } },
                grid: { color: gridColor }
              }
            }
          }
        });

        const prodDepthCtx = document.getElementById('productionDepthChart').getContext('2d');
        productionDepthChart = new Chart(prodDepthCtx, {
          type: 'bar',
          data: {
            labels: prodModels,
            datasets: [
              {
                label: 'Max AIMD Limit',
                data: bench.production_queue_summary.map(d => d.limit_max),
                backgroundColor: isV2 ? (isLight ? '#0066aa' : '#00f3ff') : (isLight ? '#000000' : '#d4d3cb'),
                borderColor: textColor,
                borderWidth: 1,
                borderRadius: 0,
                barThickness: 14
              },
              {
                label: 'Max Queue Depth',
                data: bench.production_queue_summary.map(d => d.max_queue_depth),
                backgroundColor: isV2 ? '#ffb000' : (isLight ? '#cbcabf' : '#73726c'),
                borderColor: textColor,
                borderWidth: 1,
                borderRadius: 0,
                barThickness: 14
              }
            ]
          },
          options: {
            responsive: true,
            maintainAspectRatio: false,
            plugins: { legend: { position: 'bottom', labels: { color: textColor, font: { family: 'monospace', size: 8 }, boxWidth: 10 } } },
            scales: {
              x: { ticks: { color: textColor, font: { family: 'monospace', size: 8 } }, grid: { display: false } },
              y: {
                beginAtZero: true,
                title: { display: true, text: 'COUNT', color: textColor, font: { family: 'monospace', size: 10 } },
                ticks: { color: textColor, font: { family: 'monospace', size: 8 } },
                grid: { color: gridColor }
              }
            }
          }
        });
      }
    }

    function updateChartsTheme() {
      const isLight = document.documentElement.classList.contains('light');
      const isV2 = document.documentElement.classList.contains('v2');
      const textColor = isLight ? '#000000' : '#d4d3cb';
      const gridColor = isLight ? '#cbcabf' : '#1c1c1a';
      
      const activePalette = isV2 ? v2Colors : monoColors;
      
      const allCharts = [capacityChart, successChart, latencyChart, queueChart, queueSuccessChart, productionQueueChart, productionDepthChart];
      allCharts.forEach(chart => {
        if (!chart) return;
        chart.options.scales.x.ticks.color = textColor;
        chart.options.scales.y.ticks.color = textColor;
        if (chart.options.scales.x.title) chart.options.scales.x.title.color = textColor;
        if (chart.options.scales.y.title) chart.options.scales.y.title.color = textColor;
        chart.options.scales.x.grid.color = gridColor;
        chart.options.scales.y.grid.color = gridColor;
        
        if (chart === capacityChart) {
          chart.data.datasets[0].backgroundColor = isV2 
            ? (isLight ? '#0066aa' : '#00f3ff') 
            : (isLight ? '#000000' : '#d4d3cb');
          chart.data.datasets[0].borderColor = isV2 
            ? (isLight ? '#0066aa' : '#00f3ff') 
            : (isLight ? '#000000' : '#ffffff');
        } else if (chart === queueChart) {
          chart.data.datasets[0].backgroundColor = isV2 
            ? '#ffb000' 
            : (isLight ? '#000000' : '#ffffff');
          chart.data.datasets[1].backgroundColor = isV2 
            ? (isLight ? '#0066aa' : '#00f3ff') 
            : (isLight ? '#cbcabf' : '#73726c');
          chart.data.datasets[0].borderColor = textColor;
          chart.data.datasets[1].borderColor = textColor;
        } else if (chart === queueSuccessChart) {
          chart.data.datasets[0].backgroundColor = isV2 
            ? (isLight ? '#008800' : '#39ff14') 
            : (isLight ? '#000000' : '#d4d3cb');
          chart.data.datasets[0].borderColor = textColor;
          chart.data.datasets[1].backgroundColor = isV2
            ? (isLight ? '#bb6600' : '#ffb000')
            : (isLight ? '#cbcabf' : '#73726c');
          chart.data.datasets[1].borderColor = textColor;
        } else if (chart === successChart) {
          chart.data.datasets.forEach((ds, i) => {
            ds.backgroundColor = activePalette[i % activePalette.length];
            ds.borderColor = textColor;
          });
        } else if (chart === latencyChart) {
          chart.data.datasets.forEach((ds, i) => {
            ds.borderColor = activePalette[i % activePalette.length];
          });
        }
        
        if (chart.options.plugins.legend) {
          chart.options.plugins.legend.labels.color = textColor;
        }
        chart.update();
      });
    }

    function populateProductionQueueTable() {
      const tbody = document.getElementById('productionQueueTableBody');
      if (!tbody) return;
      const rows = bench.production_queue_summary || [];
      if (rows.length === 0) {
        tbody.innerHTML = '<tr><td colspan="17" style="text-align: center; padding: 2rem;">NO PRODUCTION BURST DATA LOADED</td></tr>';
        return;
      }
      tbody.innerHTML = rows.map(d => `
        <tr>
          <td><span class="model-badge">${d.model}</span></td>
          <td class="text-right font-mono">${d.requests}</td>
          <td class="text-right font-mono text-success">[ ${d.ok} ]</td>
          <td class="text-right font-mono ${d.overload ? 'text-warning' : ''}">[ ${d.overload} ]</td>
          <td class="text-right font-mono ${d.client_timeout ? 'text-danger' : ''}">[ ${d.client_timeout || 0} ]</td>
          <td class="text-right font-mono">[ ${d.admitted} ]</td>
          <td class="text-right font-mono">[ ${d.immediate} ]</td>
          <td class="text-right font-mono ${d.queued ? 'text-warning' : ''}">[ ${d.queued} ]</td>
          <td class="text-right font-mono">${fmt.format(d.p50_queue_ms)} MS</td>
          <td class="text-right font-mono">${fmt.format(d.p90_queue_ms)} MS</td>
          <td class="text-right font-mono">${fmt.format(d.p95_queue_ms)} MS</td>
          <td class="text-right font-mono">${fmt.format(d.p99_queue_ms)} MS</td>
          <td class="text-right font-mono">${fmt.format(d.max_queue_ms)} MS</td>
          <td class="text-right font-mono">${fmt.format(d.p95_latency_ms)} MS</td>
          <td class="text-right font-mono">${d.limit_min}→${d.limit_max}</td>
          <td class="text-right font-mono">${d.max_queue_depth}</td>
          <td>${formatErrorCodes(d.status_counts)}</td>
        </tr>
      `).join('');
    }

    function populateProductionConclusions() {
      const container = document.getElementById('productionConclusions');
      if (!container) return;
      const rows = bench.production_conclusions || [];
      if (rows.length === 0) {
        container.innerHTML = '';
        return;
      }
      container.innerHTML = rows.map(d => `
        <div class="kpi-card">
          <span class="kpi-label">${d.label}</span>
          <div class="kpi-value" style="font-size: 15px;">${d.value}</div>
          <div class="kpi-sublabel">${d.detail}</div>
        </div>
      `).join('');
    }

    function populateRetryQueueConclusions() {
      const container = document.getElementById('retryQueueConclusions');
      if (!container) return;
      const rows = bench.retry_queue_conclusions || [];
      container.innerHTML = rows.map(d => `
        <div class="kpi-card">
          <span class="kpi-label">${d.label}</span>
          <div class="kpi-value" style="font-size: 15px;">${d.value}</div>
          <div class="kpi-sublabel">${d.detail}</div>
        </div>
      `).join('');
    }

    function populateRetryQueueTable() {
      const tbody = document.getElementById('retryQueueTableBody');
      if (!tbody) return;
      const rows = bench.retry_queue_summary || [];
      if (rows.length === 0) {
        tbody.innerHTML = '<tr><td colspan="10" style="text-align: center; padding: 2rem;">NO RETRY-ENABLED QUEUE SLA DATA LOADED</td></tr>';
        return;
      }
      tbody.innerHTML = rows.map(d => `
        <tr>
          <td><span class="model-badge">${d.model}</span></td>
          <td class="text-right font-mono text-success">[ ${d.queue_wait_safe_level} ]</td>
          <td class="text-right font-mono ${d.clean_success_safe_level < d.queue_wait_safe_level ? 'text-warning' : 'text-success'}">[ ${d.clean_success_safe_level} ]</td>
          <td class="text-right font-mono text-danger">[ ${d.crossing_level || '-'} ]</td>
          <td class="text-right font-mono">${fmt.format(d.max_queue_wait_ms)} MS</td>
          <td class="text-right font-mono">${fmt.format(d.p95_queue_wait_ms)} MS</td>
          <td class="text-right font-mono">${fmt.format(d.p95_latency_ms)} MS</td>
          <td class="text-right font-mono">[ ${d.ok}/${d.rows} ]</td>
          <td>${formatErrorCodes(d.statuses)}</td>
          <td>${formatErrorCodes(d.crossing_statuses)}</td>
        </tr>
      `).join('');
    }

    // App Initialization
    document.addEventListener('DOMContentLoaded', () => {
      // Setup tabs
      document.querySelectorAll('.tab-btn').forEach(btn => {
        btn.addEventListener('click', () => {
          document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
          document.querySelectorAll('.tab-panel').forEach(p => p.classList.remove('active'));
          
          btn.classList.add('active');
          const panelId = btn.getAttribute('data-tab');
          document.getElementById(panelId).classList.add('active');
        });
      });

      // Setup theme toggle
      document.getElementById('themeToggle').addEventListener('click', () => {
        document.documentElement.classList.toggle('light');
        const isLight = document.documentElement.classList.contains('light');
        localStorage.setItem('theme', isLight ? 'light' : 'dark');
        updateChartsTheme();
      });

      // Setup version toggle
      const versionBtn = document.getElementById('versionToggle');
      versionBtn.addEventListener('click', () => {
        document.documentElement.classList.toggle('v2');
        const isV2 = document.documentElement.classList.contains('v2');
        localStorage.setItem('version', isV2 ? 'v2' : 'v1');
        versionBtn.textContent = isV2 ? '[ THEME: V2 (COLOR) ]' : '[ THEME: V1 (MONO) ]';
        updateChartsTheme();
      });

      // Initialize version button text on load
      const isV2 = document.documentElement.classList.contains('v2');
      versionBtn.textContent = isV2 ? '[ THEME: V2 (COLOR) ]' : '[ THEME: V1 (MONO) ]';

      // Setup searches & filters
      document.getElementById('capacitySearch').addEventListener('input', populateCapacityTable);
      document.getElementById('queueSearch').addEventListener('input', populateQueueTable);
      
      document.getElementById('rawSearch').addEventListener('input', (e) => {
        logState.query = e.target.value.toLowerCase();
        logState.page = 1;
        populateRawLogsTable();
      });

      document.getElementById('rawPhaseFilter').addEventListener('change', (e) => {
        logState.phase = e.target.value;
        logState.page = 1;
        populateRawLogsTable();
      });

      document.getElementById('rawStatusFilter').addEventListener('change', (e) => {
        logState.status = e.target.value;
        logState.page = 1;
        populateRawLogsTable();
      });

      document.getElementById('prevPage').addEventListener('click', () => {
        if (logState.page > 1) {
          logState.page--;
          populateRawLogsTable();
        }
      });

      document.getElementById('nextPage').addEventListener('click', () => {
        logState.page++;
        populateRawLogsTable();
      });

      // Initial populate
      populateCapacityTable();
      populateQueueTable();
      populateProductionConclusions();
      populateProductionQueueTable();
      populateRetryQueueConclusions();
      populateRetryQueueTable();
      populateRawLogsTable();
      
      // Init Charts
      initCharts();
    });
  </script>
</body>
</html>
"""
html = html_template.replace("__BENCH_DATA__", payload)
OUT.write_text(html)
print(OUT)
