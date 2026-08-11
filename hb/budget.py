"""Hard budget guards for execute: preflight + mid-run kill / soft warn."""

from __future__ import annotations

from dataclasses import dataclass, field
from enum import Enum

from hb.models import Budget, Telemetry


# Soft warn when usage crosses this fraction of any limit.
WARN_FRACTION = 0.80


class BudgetLevel(str, Enum):
    ok = "ok"
    warn = "warn"
    exceeded = "exceeded"


@dataclass
class BudgetPlan:
    """Resolved limits for one execute call."""

    max_wall_s: float | None
    max_usd: float | None
    max_turns: int | None
    max_tokens: int | None
    effective_timeout_s: int
    warnings: list[str] = field(default_factory=list)

    def summary_lines(self) -> list[str]:
        lines: list[str] = []
        if self.max_wall_s is not None:
            lines.append(f"max_minutes={self.max_wall_s / 60:.1f} ({int(self.max_wall_s)}s)")
        if self.max_usd is not None:
            lines.append(f"max_usd=${self.max_usd:.4f}")
        if self.max_turns is not None:
            lines.append(f"max_turns={self.max_turns}")
        if self.max_tokens is not None:
            lines.append(f"max_tokens={self.max_tokens:,}")
        lines.append(f"effective_timeout={self.effective_timeout_s}s")
        return lines


@dataclass
class BudgetCheck:
    level: BudgetLevel
    reasons: list[str] = field(default_factory=list)
    warn_reasons: list[str] = field(default_factory=list)

    @property
    def exceeded(self) -> bool:
        return self.level == BudgetLevel.exceeded

    @property
    def should_warn(self) -> bool:
        return self.level == BudgetLevel.warn or bool(self.warn_reasons)


def plan_budget(budget: Budget | None, cli_timeout_s: int) -> BudgetPlan:
    """
    Resolve config budget + CLI timeout into enforceable limits.

    - max_minutes caps wall clock and (with CLI) effective per-round timeout
    - null limits are not enforced
    - effective_timeout is min(cli_timeout, max_minutes) when max_minutes set
    """
    budget = budget or Budget()
    warnings: list[str] = []

    max_wall_s: float | None = None
    if budget.max_minutes is not None and budget.max_minutes > 0:
        max_wall_s = float(budget.max_minutes) * 60.0

    max_usd = budget.max_usd if budget.max_usd is not None and budget.max_usd > 0 else None
    max_turns = budget.max_turns if budget.max_turns is not None and budget.max_turns > 0 else None
    max_tokens = (
        budget.max_tokens if budget.max_tokens is not None and budget.max_tokens > 0 else None
    )

    if cli_timeout_s <= 0:
        cli_timeout_s = 1800
        warnings.append("cli timeout <= 0; using 1800s")

    if max_wall_s is not None:
        effective = min(cli_timeout_s, int(max_wall_s))
        if cli_timeout_s > max_wall_s:
            warnings.append(
                f"CLI --timeout {cli_timeout_s}s capped to budget.max_minutes "
                f"({int(max_wall_s)}s)"
            )
    else:
        effective = cli_timeout_s

    if max_usd is None and max_turns is None and max_tokens is None and max_wall_s is None:
        warnings.append("no budget limits set on config (only CLI timeout applies)")

    return BudgetPlan(
        max_wall_s=max_wall_s,
        max_usd=max_usd,
        max_turns=max_turns,
        max_tokens=max_tokens,
        effective_timeout_s=max(1, effective),
        warnings=warnings,
    )


def _token_total(tel: Telemetry | None) -> int | None:
    if tel is None:
        return None
    tin = tel.tokens_in
    tout = tel.tokens_out
    if tin is None and tout is None:
        return None
    return (tin or 0) + (tout or 0)


def check_budget(
    plan: BudgetPlan,
    *,
    wall_s: float,
    telemetry: Telemetry | None = None,
    extra_usd: float = 0.0,
) -> BudgetCheck:
    """
    Evaluate current usage against plan.

    hard exceed → level=exceeded
    soft warn (≥80% of any limit, none exceeded) → level=warn
    else ok
    """
    tel = telemetry or Telemetry()
    usd = (tel.estimated_usd or 0.0) + (tel.proxy_estimated_usd or 0.0) + extra_usd
    turns = tel.turns
    tokens = _token_total(tel)

    hard: list[str] = []
    soft: list[str] = []

    def consider(
        name: str,
        used: float | None,
        limit: float | None,
        *,
        fmt: str,
    ) -> None:
        if used is None or limit is None or limit <= 0:
            return
        if used >= limit:
            hard.append(f"{name} {fmt.format(used=used)} ≥ limit {fmt.format(used=limit)}")
        elif used >= WARN_FRACTION * limit:
            soft.append(
                f"{name} {fmt.format(used=used)} ≥ {int(WARN_FRACTION * 100)}% of "
                f"{fmt.format(used=limit)}"
            )

    consider("wall", wall_s, plan.max_wall_s, fmt="{used:.1f}s")
    # Only enforce $ when we have a SUT/proxy cost signal (or forced extra_usd)
    usd_signal = tel.estimated_usd is not None or tel.proxy_estimated_usd is not None or extra_usd > 0
    if usd_signal:
        consider("usd", usd, plan.max_usd, fmt="${used:.4f}")
    consider(
        "turns",
        float(turns) if turns is not None else None,
        float(plan.max_turns) if plan.max_turns is not None else None,
        fmt="{used:.0f}",
    )
    consider(
        "tokens",
        float(tokens) if tokens is not None else None,
        float(plan.max_tokens) if plan.max_tokens is not None else None,
        fmt="{used:.0f}",
    )

    if hard:
        return BudgetCheck(level=BudgetLevel.exceeded, reasons=hard, warn_reasons=soft)
    if soft:
        return BudgetCheck(level=BudgetLevel.warn, reasons=[], warn_reasons=soft)
    return BudgetCheck(level=BudgetLevel.ok)


def remaining_timeout_s(plan: BudgetPlan, wall_s_so_far: float) -> int:
    """Seconds left for the next subprocess given wall budget + plan timeout."""
    cap = plan.effective_timeout_s
    if plan.max_wall_s is not None:
        left = plan.max_wall_s - wall_s_so_far
        if left <= 0:
            return 0
        cap = min(cap, int(left) if left == int(left) else int(left) + 1)
    return max(0, cap)
