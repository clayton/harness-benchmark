"""Generate a plain, monospace static HTML comparison report.

Readable data table + small winner chips (best quality, cheapest, fastest, leanest).
"""

from __future__ import annotations

import html
from collections import defaultdict
from datetime import datetime, timezone
from pathlib import Path

from hb.models import Config, RunRecord, Scenario

# Micro 16px Heroicons (MIT) — outline, currentColor
_ICONS = {
    "star": (
        '<svg class="ico" viewBox="0 0 16 16" aria-hidden="true">'
        '<path fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" '
        'stroke-linejoin="round" d="M8 1.5l1.76 3.57 3.94.57-2.85 2.78.67 3.92L8 10.52l-3.52 1.82'
        '.67-3.92L2.3 5.64l3.94-.57L8 1.5z"/></svg>'
    ),
    "bolt": (
        '<svg class="ico" viewBox="0 0 16 16" aria-hidden="true">'
        '<path fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" '
        'stroke-linejoin="round" d="M9 1.5L3.5 9h4L7 14.5 12.5 7h-4L9 1.5z"/></svg>'
    ),
    "currency": (
        '<svg class="ico" viewBox="0 0 16 16" aria-hidden="true">'
        '<path fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" '
        'stroke-linejoin="round" d="M8 1.5v13M10.5 4.25c-.6-.7-1.5-1-2.5-1-1.8 0-3 1.1-3 2.5s1.2 '
        '2.5 3.2 2.5c2 0 3.3 1 3.3 2.5s-1.3 2.5-3.3 2.5c-1.1 0-2.1-.4-2.7-1.1"/></svg>'
    ),
    "bars": (
        '<svg class="ico" viewBox="0 0 16 16" aria-hidden="true">'
        '<path fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" '
        'stroke-linejoin="round" d="M2.5 13V8.5M8 13V3M13.5 13V6"/></svg>'
    ),
}


def _esc(value: object) -> str:
    return html.escape("" if value is None else str(value))


def _fmt_quality(run: RunRecord) -> str:
    q = run.primary_quality()
    if q is None:
        return "—"
    return f"{q:.2f}"


def _fmt_usd(run: RunRecord) -> str:
    v = run.telemetry.estimated_usd
    if v is None:
        return "—"
    return f"${v:.3f}"


def _fmt_int(n: int | None) -> str:
    if n is None:
        return "—"
    return f"{n:,}"


def _fmt_time(run: RunRecord) -> str:
    ms = run.telemetry.wall_ms
    if ms is None:
        return "—"
    if ms < 1000:
        return f"{ms}ms"
    sec = ms / 1000
    if sec < 60:
        return f"{sec:.1f}s"
    return f"{sec / 60:.1f}m"


def _fmt_status(run: RunRecord) -> str:
    if run.status.value == "budget_exceeded":
        bools = [j.passed for j in run.judges if j.passed is not None]
        if bools and all(bools):
            return "pass/budget"
        if bools and any(bools):
            return "partial/budget"
        return "budget"
    bools = [j.passed for j in run.judges if j.passed is not None]
    if not bools:
        base = run.status.value
    elif all(bools):
        base = "pass"
    elif any(bools):
        base = "partial"
    else:
        base = "fail"
    if run.status.value not in ("completed", "ingested") and run.status.value != base:
        return f"{base}/{run.status.value}"
    return base


def _status_class(run: RunRecord) -> str:
    s = _fmt_status(run)
    if s.startswith("pass") and "budget" not in s:
        return "ok"
    if "budget" in s or s.startswith("fail") or s.startswith("timeout"):
        return "bad"
    if s.startswith("partial"):
        return "mid"
    return ""


def _patch_cell(run: RunRecord) -> str:
    stats = run.patch_stats
    if not stats:
        return "—"
    return f"{stats.files_changed}f +{stats.insertions}/-{stats.deletions}"


def _token_total(run: RunRecord) -> int | None:
    tin = run.telemetry.tokens_in
    tout = run.telemetry.tokens_out
    if tin is None and tout is None:
        return None
    return (tin or 0) + (tout or 0)



# Artifacts commonly written under results/<run_id>/
_ARTIFACT_NAMES = (
    ("patch", "patch.diff"),
    ("log", "agent.log"),
    ("dialogue", "dialogue.json"),
    ("snapshot", "snapshot.json"),
    ("judge", "judge.json"),
    ("run", "run.json"),
)


def _artifact_links(run: RunRecord, results_href: str) -> str:
    """Relative links from the HTML report into results/<id>/…"""
    base = f"{results_href.rstrip('/')}/{run.id}"
    parts: list[str] = []
    # Prefer known paths; still link even if missing so static share stays navigable
    # when files are present on disk next to the report.
    for label, name in _ARTIFACT_NAMES:
        # Only show links that exist when we can resolve via patch_path/snapshot_path
        # or always show common set — report is static, so always emit the set and
        # let missing files 404 when opened locally. Prefer existence check if
        # patch_path is absolute-ish.
        parts.append(f'<a class="art" href="{_esc(base)}/{_esc(name)}">{_esc(label)}</a>')
    return '<span class="arts">' + " ".join(parts) + "</span>"


def _winners(runs: list[RunRecord]) -> dict[str, set[str]]:
    """
    Best-of metrics for a comparison set (typically one scenario).
    Ties: all tied run ids get the chip.
    Only awards when ≥2 runs have a comparable value.
    """
    out: dict[str, set[str]] = {
        "quality": set(),
        "cost": set(),
        "speed": set(),
        "tokens": set(),
    }
    if len(runs) < 2:
        return out

    def pick(metric: str, pairs: list[tuple[str, float]], higher_better: bool) -> None:
        if len(pairs) < 2:
            return
        best = max(v for _, v in pairs) if higher_better else min(v for _, v in pairs)
        out[metric] = {rid for rid, v in pairs if v == best}

    pick(
        "quality",
        [(r.id, q) for r in runs if (q := r.primary_quality()) is not None],
        higher_better=True,
    )
    pick(
        "cost",
        [
            (r.id, c)
            for r in runs
            if (c := r.telemetry.estimated_usd) is not None
        ],
        higher_better=False,
    )
    pick(
        "speed",
        [(r.id, ms) for r in runs if (ms := r.telemetry.wall_ms) is not None],
        higher_better=False,
    )
    pick(
        "tokens",
        [(r.id, t) for r in runs if (t := _token_total(r)) is not None],
        higher_better=False,
    )
    return out


def _chip(kind: str, label: str) -> str:
    icon = _ICONS.get(kind, "")
    return (
        f'<span class="chip chip-{_esc(kind)}" title="{_esc(label)}">'
        f"{icon}<span>{_esc(label)}</span></span>"
    )


def _chips_for(run_id: str, winners: dict[str, set[str]]) -> str:
    chips = []
    if run_id in winners.get("quality", set()):
        chips.append(_chip("star", "best"))
    if run_id in winners.get("cost", set()):
        chips.append(_chip("currency", "cheap"))
    if run_id in winners.get("speed", set()):
        chips.append(_chip("bolt", "fast"))
    if run_id in winners.get("tokens", set()):
        chips.append(_chip("bars", "lean"))
    if not chips:
        return ""
    return '<span class="chips">' + "".join(chips) + "</span>"


def _winner_summary(runs: list[RunRecord], winners: dict[str, set[str]]) -> str:
    """One-line summary: who won what."""
    by_id = {r.id: r for r in runs}

    def names(metric: str) -> str:
        ids = winners.get(metric) or set()
        if not ids:
            return "—"
        labels = []
        for rid in sorted(ids):
            r = by_id.get(rid)
            labels.append(r.config_id if r else rid[:8])
        return ", ".join(labels)

    if not any(winners.values()):
        return ""

    parts = [
        f'{_chip("star", "best")} { _esc(names("quality")) }',
        f'{_chip("currency", "cheap")} { _esc(names("cost")) }',
        f'{_chip("bolt", "fast")} { _esc(names("speed")) }',
        f'{_chip("bars", "lean")} { _esc(names("tokens")) }',
    ]
    return '<p class="winners">' + " · ".join(parts) + "</p>"


def _cell_num(text: str, chip_html: str = "") -> str:
    if chip_html:
        return f'<td class="num">{_esc(text)} {chip_html}</td>'
    return f'<td class="num">{_esc(text)}</td>'


def _row_cells(
    run: RunRecord,
    configs: dict[str, Config],
    winners: dict[str, set[str]],
    *,
    include_scenario: bool,
    results_href: str = "../results",
) -> str:
    cfg = configs.get(run.config_id)
    harness = (cfg.harness_label() if cfg else None) or run.harness or "—"
    model = (cfg.model_label() if cfg else None) or run.model or "—"
    workflow = (cfg.workflow if cfg else None) or run.metadata.get("workflow") or "—"
    st = _fmt_status(run)
    st_cls = _status_class(run)

    q_chip = _chip("star", "best") if run.id in winners.get("quality", set()) else ""
    c_chip = _chip("currency", "cheap") if run.id in winners.get("cost", set()) else ""
    s_chip = _chip("bolt", "fast") if run.id in winners.get("speed", set()) else ""
    # lean chip lives on tok_in cell (represents total tokens)
    t_chip = _chip("bars", "lean") if run.id in winners.get("tokens", set()) else ""

    cells = [
        f'<td class="id"><a class="runid" href="{_esc(results_href.rstrip('/'))}/{_esc(run.id)}/run.json">{_esc(run.id[:12])}</a></td>',
    ]
    if include_scenario:
        cells.append(f'<td class="clip">{_esc(run.scenario_id)}</td>')

    # config cell carries all chips for at-a-glance row winner identity
    row_chips = _chips_for(run.id, winners)
    config_cell = f'<td class="clip">{_esc(run.config_id)}{row_chips}</td>'

    cells.extend(
        [
            config_cell,
            f'<td>{_esc(harness)}</td>',
            f'<td>{_esc(model)}</td>',
            f'<td>{_esc(workflow)}</td>',
            _cell_num(_fmt_quality(run), q_chip),
            _cell_num(_fmt_int(run.telemetry.tokens_in), t_chip),
            _cell_num(_fmt_int(run.telemetry.tokens_out)),
            _cell_num(_fmt_usd(run), c_chip),
            _cell_num(_fmt_time(run), s_chip),
            f'<td class="num">{_esc(_patch_cell(run))}</td>',
            f'<td class="status {st_cls}">{_esc(st)}</td>',
            f'<td class="arts-cell">{_artifact_links(run, results_href)}</td>',
        ]
    )
    return "<tr>\n" + "\n".join(cells) + "\n</tr>"


def _table(
    runs: list[RunRecord],
    configs: dict[str, Config],
    *,
    include_scenario: bool,
    winners: dict[str, set[str]] | None = None,
    results_href: str = "../results",
) -> str:
    winners = winners if winners is not None else _winners(runs)
    headers = ["run"]
    if include_scenario:
        headers.append("scenario")
    headers.extend(
        [
            "config",
            "harness",
            "model",
            "workflow",
            "quality",
            "tok_in",
            "tok_out",
            "cost",
            "time",
            "patch",
            "result",
            "artifacts",
        ]
    )
    num_headers = {"quality", "tok_in", "tok_out", "cost", "time", "patch"}
    ths = []
    for h in headers:
        cls = ' class="num"' if h in num_headers else ""
        ths.append(f"<th{cls}>{_esc(h)}</th>")

    body = "\n".join(
        _row_cells(
            r, configs, winners, include_scenario=include_scenario, results_href=results_href
        )
        for r in runs
    )
    empty = f'<tr><td colspan="{len(headers)}" class="empty">no runs</td></tr>'
    summary = _winner_summary(runs, winners)
    return f"""
{summary}
<div class="scroll">
<table>
  <thead>
    <tr>
      {"".join(ths)}
    </tr>
  </thead>
  <tbody>
    {body if body else empty}
  </tbody>
</table>
</div>
"""


def render_report(
    runs: list[RunRecord],
    scenarios: dict[str, Scenario] | None = None,
    configs: dict[str, Config] | None = None,
    title: str = "Harness Benchmark Report",
    *,
    experiment_id: str | None = None,
    hypothesis: str | None = None,
    notes: str | None = None,
    results_href: str = "../results",
) -> str:
    scenarios = scenarios or {}
    configs = configs or {}
    generated = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M UTC")

    by_scenario: dict[str, list[RunRecord]] = defaultdict(list)
    for run in sorted(runs, key=lambda r: (r.scenario_id, r.config_id, r.repeat)):
        by_scenario[run.scenario_id].append(run)

    # Overview winners only make sense across same-scenario pairs;
    # for mixed overview we still compute global bests as a weak signal.
    overview = _table(
        sorted(runs, key=lambda r: r.created_at, reverse=True),
        configs,
        include_scenario=True,
        winners=_winners(runs),
        results_href=results_href,
    )

    sections: list[str] = []
    for scenario_id, scenario_runs in by_scenario.items():
        scenario = scenarios.get(scenario_id)
        stitle = scenario.title if scenario else scenario_id
        stype = scenario.type.value if scenario else "?"
        win = _winners(scenario_runs)
        sections.append(
            f"""
<section class="block">
  <h2>{_esc(stitle)}</h2>
  <p class="meta">{_esc(scenario_id)} · {_esc(stype)} · {len(scenario_runs)} run(s)</p>
  {_table(scenario_runs, configs, include_scenario=False, winners=win, results_href=results_href)}
</section>
"""
        )

    if not sections:
        sections.append(
            '<p class="empty">No runs yet. Use <code>hb execute</code> or <code>hb finish</code>.</p>'
        )

    exp_block = ""
    if experiment_id or hypothesis or notes:
        bits = []
        if experiment_id:
            bits.append(f"experiment <code>{_esc(experiment_id)}</code>")
        if hypothesis:
            bits.append(f"hypothesis: {_esc(hypothesis.strip())}")
        if notes:
            bits.append(_esc(notes.strip()[:500]))
        exp_block = '<p class="meta">' + " · ".join(bits) + "</p>"

    return f"""<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>{_esc(title)}</title>
  <style>
    :root {{
      --bg: #fafafa;
      --fg: #111;
      --muted: #666;
      --line: #ccc;
      --head: #f0f0f0;
      --ok: #0a0;
      --bad: #a00;
      --mid: #a60;
      --chip-bg: #eee;
      --chip-fg: #222;
      --chip-best: #e8f5e9;
      --chip-cheap: #e3f2fd;
      --chip-fast: #fff8e1;
      --chip-lean: #f3e5f5;
      --mono: ui-monospace, "SFMono-Regular", Menlo, Consolas, "Liberation Mono", monospace;
      --gap: 24px;
    }}
    * {{ box-sizing: border-box; }}
    html {{ font-size: 13px; }}
    body {{
      margin: 0;
      padding: var(--gap);
      font-family: var(--mono);
      font-size: 13px;
      line-height: 1.45;
      color: var(--fg);
      background: var(--bg);
    }}
    main {{ max-width: 100%; margin: 0 auto; }}
    h1 {{ margin: 0 0 4px; font-size: 15px; font-weight: 700; }}
    h2 {{ margin: 0 0 4px; font-size: 13px; font-weight: 700; }}
    .sub, .meta, .legend, .empty {{
      color: var(--muted);
      margin: 0 0 var(--gap);
    }}
    .meta {{ margin-bottom: 8px; }}
    .legend {{ margin-top: var(--gap); font-size: 12px; max-width: 72ch; }}
    .block {{
      margin-bottom: var(--gap);
      padding-top: 8px;
      border-top: 1px solid var(--line);
    }}
    .block:first-of-type {{ border-top: 0; padding-top: 0; }}

    .scroll {{
      overflow-x: auto;
      border: 1px solid var(--line);
      background: #fff;
    }}
    table {{
      width: 100%;
      border-collapse: collapse;
      font-variant-numeric: tabular-nums;
      white-space: nowrap;
    }}
    th, td {{
      padding: 6px 10px;
      border-bottom: 1px solid var(--line);
      border-right: 1px solid #eee;
      text-align: left;
      vertical-align: middle;
    }}
    th:last-child, td:last-child {{ border-right: 0; }}
    th {{
      background: var(--head);
      color: var(--muted);
      font-weight: 600;
      font-size: 11px;
      text-transform: lowercase;
      position: sticky;
      top: 0;
      z-index: 1;
    }}
    tbody tr:nth-child(even) td {{ background: #fcfcfc; }}
    tbody tr:hover td {{ background: #f5f5f5; }}
    td.num, th.num {{ text-align: right; font-variant-numeric: tabular-nums; }}
    td.id {{ color: var(--muted); }}
    td.clip {{
      max-width: 22ch;
      overflow: hidden;
      text-overflow: ellipsis;
    }}
    td.status.ok {{ color: var(--ok); font-weight: 600; }}
    td.status.bad {{ color: var(--bad); font-weight: 600; }}
    td.status.mid {{ color: var(--mid); font-weight: 600; }}

    /* Winner chips — small, plain, scannable */
    .chips {{
      display: inline-flex;
      gap: 4px;
      margin-left: 6px;
      vertical-align: middle;
    }}
    .chip {{
      display: inline-flex;
      align-items: center;
      gap: 3px;
      padding: 1px 5px 1px 3px;
      border: 1px solid var(--line);
      border-radius: 3px;
      background: var(--chip-bg);
      color: var(--chip-fg);
      font-size: 10px;
      font-weight: 600;
      line-height: 1.3;
      text-transform: lowercase;
      white-space: nowrap;
    }}
    .chip .ico {{
      width: 12px;
      height: 12px;
      flex: 0 0 auto;
    }}
    .chip-star {{ background: var(--chip-best); border-color: #bdb; }}
    .chip-currency {{ background: var(--chip-cheap); border-color: #bcd; }}
    .chip-bolt {{ background: var(--chip-fast); border-color: #dcc; }}
    .chip-bars {{ background: var(--chip-lean); border-color: #cbc; }}
    td.num .chip {{ margin-left: 6px; }}

    .winners {{
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 8px 14px;
      margin: 0 0 8px;
      color: var(--fg);
      font-size: 12px;
    }}
    .winners .chip {{ vertical-align: middle; }}

    code {{ font-family: inherit; font-size: inherit; }}
    a.runid {{ color: inherit; text-decoration: none; border-bottom: 1px dotted var(--line); }}
    a.runid:hover {{ border-bottom-color: var(--fg); }}
    .arts {{
      display: inline-flex;
      flex-wrap: wrap;
      gap: 4px 8px;
    }}
    a.art {{
      color: var(--muted);
      text-decoration: none;
      border-bottom: 1px dotted var(--line);
      font-size: 11px;
    }}
    a.art:hover {{ color: var(--fg); border-bottom-color: var(--fg); }}
    td.arts-cell {{ white-space: normal; max-width: 28ch; }}

    @media (prefers-color-scheme: dark) {{
      :root {{
        --bg: #111;
        --fg: #e8e8e8;
        --muted: #999;
        --line: #333;
        --head: #1a1a1a;
        --ok: #6c6;
        --bad: #f66;
        --mid: #fc6;
        --chip-bg: #222;
        --chip-fg: #eee;
        --chip-best: #1a2e1a;
        --chip-cheap: #1a2430;
        --chip-fast: #2a2518;
        --chip-lean: #241a28;
      }}
      .scroll {{ background: #161616; }}
      tbody tr:nth-child(even) td {{ background: #141414; }}
      tbody tr:hover td {{ background: #1c1c1c; }}
      th, td {{ border-right-color: #222; }}
      .chip-star {{ border-color: #3a5; }}
      .chip-currency {{ border-color: #47a; }}
      .chip-bolt {{ border-color: #a83; }}
      .chip-bars {{ border-color: #858; }}
    }}
  </style>
</head>
<body>
  <main>
    <h1>{_esc(title)}</h1>
    <p class="sub">{_esc(generated)} · {len(runs)} run(s)</p>
    {exp_block}

    <section class="block">
      <h2>all runs</h2>
      {overview}
    </section>

    <section class="block">
      <h2>by scenario</h2>
      {"".join(sections)}
    </section>

    <p class="legend">
      quality = mean judge score (0–1). cost from harness JSON usage when available.
      artifacts link into <code>{_esc(results_href)}/&lt;run_id&gt;/</code>
      (patch.diff, agent.log, dialogue.json, snapshot.json, judge.json, run.json).
      Open this HTML from the repo so relative paths resolve.
    </p>
  </main>
</body>
</html>
"""


def write_report(html_doc: str, out_path: Path) -> Path:
    out_path = out_path.resolve()
    out_path.parent.mkdir(parents=True, exist_ok=True)
    out_path.write_text(html_doc, encoding="utf-8")
    return out_path
