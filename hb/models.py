"""Pydantic schemas for scenarios, configs, runs, and results."""

from __future__ import annotations

from datetime import datetime, timezone
from enum import Enum
from typing import Any

from pydantic import BaseModel, Field, HttpUrl


def utcnow() -> datetime:
    return datetime.now(timezone.utc)


class ScenarioType(str, Enum):
    bugfix = "bugfix"
    feature = "feature"
    refactor = "refactor"


class ContaminationRisk(str, Enum):
    low = "low"
    medium = "medium"
    high = "high"


class ComparisonMode(str, Enum):
    controlled = "controlled"
    ecological = "ecological"


class InteractionMode(str, Enum):
    """How the system under test gets answers to clarifying questions."""

    unattended = "unattended"  # no Q&A; implement from prompt alone
    human = "human"  # pause for a human (stdin or answer file)
    proxy = "proxy"  # stakeholder proxy agent answers from stakeholder_brief


class Budget(BaseModel):
    max_minutes: float | None = 30
    max_tokens: int | None = None
    max_turns: int | None = None
    max_usd: float | None = None


class StakeholderProxy(BaseModel):
    """Config for the stakeholder proxy (outside the system under test)."""

    model: str = Field(
        default="anthropic/claude-haiku-4.5",
        description=(
            "OpenRouter (or provider) model id for the proxy. Prefer a cheap model — "
            "proxy only answers behavior questions, not coding."
        ),
    )
    provider: str = Field(default="openrouter")
    harness: str = Field(default="pi", description="Runner used to call the proxy (pi only for now)")
    max_qa_rounds: int = 8
    # Keep proxy cheap/fast when possible
    temperature: float | None = 0.2


class ScenarioRepo(BaseModel):
    """Where to get the code under test."""

    url: str = Field(description="Git clone URL")
    base_ref: str = Field(description="Commit/tag/branch the agent starts from")
    gold_ref: str | None = Field(
        default=None,
        description="Optional reference implementation (never shown to agent)",
    )


class ScenarioAcceptance(BaseModel):
    """How we know a run succeeded (oracle signals)."""

    setup_commands: list[str] = Field(
        default_factory=list,
        description="One-time / per-checkout setup before tests (install deps, etc.)",
    )
    test_commands: list[str] = Field(
        default_factory=list,
        description="Shell commands run after applying the patch; all must exit 0",
    )
    build_commands: list[str] = Field(default_factory=list)
    must_pass_existing_tests: bool = True
    # Many real PRs add the regression test with the fix. base_ref alone may
    # pass existing tests. Prefer fail_to_pass paths from gold when judging.
    fail_to_pass: list[str] = Field(
        default_factory=list,
        description="Test node ids / patterns expected to fail at base and pass after a correct fix",
    )
    notes: str | None = None


class Scenario(BaseModel):
    """A frozen, reproducible task."""

    id: str = Field(description="Stable slug, e.g. ruby-json-issue-123")
    type: ScenarioType
    title: str
    description: str = Field(
        description="Human-readable summary of what the agent should achieve"
    )
    prompt: str = Field(
        description="Exact text given to the agent (SUT). Intent only — no gold solution."
    )
    stakeholder_brief: str | None = Field(
        default=None,
        description=(
            "Privileged brief for human/proxy stakeholders only — richer intent, "
            "acceptance examples, non-goals. Must NOT include the gold patch or "
            "step-by-step implementation. Never shown to the SUT."
        ),
    )
    repo: ScenarioRepo
    acceptance: ScenarioAcceptance = Field(default_factory=ScenarioAcceptance)
    language: str | None = None
    tags: list[str] = Field(default_factory=list)
    contamination_risk: ContaminationRisk = ContaminationRisk.medium
    source: str | None = Field(
        default=None,
        description="Issue URL, changelog section, PR, etc.",
    )
    difficulty: str | None = Field(default=None, description="easy|medium|hard or free text")
    allowed_paths: list[str] = Field(
        default_factory=list,
        description="Optional path allowlist hints for judges (not hard sandbox yet)",
    )
    metadata: dict[str, Any] = Field(default_factory=dict)


class Config(BaseModel):
    """A treatment: what system is under test.

    Harness and model versions are first-class. Silent CLI/product updates can
    swing results as much as model swaps — pin both when making claims.
    Skill versions are deferred (name-only for now).
    """

    id: str
    name: str
    harness: str = Field(description="Family name: grok, claude_code, codex, cursor, manual")
    harness_version: str | None = Field(
        default=None,
        description=(
            "Exact harness/CLI/app version under test (e.g. output of `claude --version`, "
            "Codex release tag, Grok Build build). Required for non-manual real experiments."
        ),
    )
    model: str | None = Field(
        default=None,
        description="Model id as precisely as the provider allows (include dated/build suffix when available)",
    )
    model_version: str | None = Field(
        default=None,
        description=(
            "Optional extra pin when `model` is a family alias "
            "(e.g. model=claude-opus-4, model_version=20250514). Prefer baking version into model when possible."
        ),
    )
    workflow: str = Field(
        default="baseline",
        description="e.g. baseline, plan_then_implement, grill_me, orchestrator_subagents",
    )
    interaction: InteractionMode = Field(
        default=InteractionMode.unattended,
        description="unattended | human | proxy — how clarifying questions are answered",
    )
    stakeholder: StakeholderProxy | None = Field(
        default=None,
        description="Required when interaction=proxy",
    )
    tools: list[str] = Field(default_factory=list)
    skills: list[str] = Field(
        default_factory=list,
        description="Skill names only for now; pin skill versions later if needed",
    )
    budget: Budget = Field(default_factory=Budget)
    comparison_mode: ComparisonMode = ComparisonMode.controlled
    # Sampling controls when the harness/provider supports them
    seed: int | None = Field(
        default=None,
        description=(
            "Requested sampling seed for re-runs. Best-effort only — many agent "
            "harnesses do not expose seed; still recorded for experiment provenance."
        ),
    )
    temperature: float | None = Field(
        default=None,
        description="Requested temperature when the harness/provider supports it",
    )
    notes: str | None = None
    # Free-form knobs for adapter-specific settings
    harness_options: dict[str, Any] = Field(default_factory=dict)
    metadata: dict[str, Any] = Field(default_factory=dict)

    def harness_label(self) -> str:
        if self.harness_version:
            return f"{self.harness}@{self.harness_version}"
        return self.harness

    def model_label(self) -> str:
        if not self.model:
            return "—"
        if self.model_version:
            return f"{self.model}@{self.model_version}"
        return self.model


class Telemetry(BaseModel):
    wall_ms: int | None = None
    tokens_in: int | None = None
    tokens_out: int | None = None
    tokens_cache: int | None = None
    estimated_usd: float | None = None
    turns: int | None = None
    tool_calls: int | None = None
    human_interventions: int = 0
    # Stakeholder proxy cost (separate from SUT)
    proxy_tokens_in: int | None = None
    proxy_tokens_out: int | None = None
    proxy_estimated_usd: float | None = None
    qa_rounds: int | None = None


class PatchStats(BaseModel):
    files_changed: int = 0
    insertions: int = 0
    deletions: int = 0
    paths: list[str] = Field(default_factory=list)


class JudgeScore(BaseModel):
    name: str
    score: float | None = Field(default=None, description="Normalized 0–1 when possible")
    raw: dict[str, Any] = Field(default_factory=dict)
    passed: bool | None = None
    notes: str | None = None


class RunStatus(str, Enum):
    pending = "pending"
    running = "running"
    completed = "completed"
    failed = "failed"
    timeout = "timeout"
    budget_exceeded = "budget_exceeded"
    ingested = "ingested"


class RunRecord(BaseModel):
    """Immutable record of one scenario × config attempt."""

    id: str
    scenario_id: str
    config_id: str
    repeat: int = 0
    status: RunStatus = RunStatus.ingested
    created_at: datetime = Field(default_factory=utcnow)
    finished_at: datetime | None = None
    worktree: str | None = None
    patch_path: str | None = None
    patch_stats: PatchStats | None = None
    telemetry: Telemetry = Field(default_factory=Telemetry)
    judges: list[JudgeScore] = Field(default_factory=list)
    error: str | None = None
    notes: str | None = None
    # Version pins as declared on the config (copied at ingest for immutability)
    harness: str | None = None
    harness_version: str | None = None
    model: str | None = None
    model_version: str | None = None
    # What was actually detected on the machine at run time (may differ from config)
    resolved_harness_version: str | None = Field(
        default=None,
        description="Actual harness version string captured during the run, if known",
    )
    resolved_model: str | None = Field(
        default=None,
        description="Actual model id the harness reported using, if known",
    )
    # Reproducibility
    seed: int | None = Field(
        default=None,
        description="Requested seed (copied from config or set for this run)",
    )
    temperature: float | None = None
    parent_run_id: str | None = Field(
        default=None,
        description="If this run was created via `hb rerun`, the source run id",
    )
    experiment_id: str | None = Field(
        default=None,
        description="Optional experiment matrix id this run belongs to",
    )
    snapshot_path: str | None = Field(
        default=None,
        description="Path to frozen scenario+config snapshot JSON for this run",
    )
    # Snapshots of scenario/config ids only; full YAML lives in corpus
    metadata: dict[str, Any] = Field(default_factory=dict)

    def primary_quality(self) -> float | None:
        """Best-effort single quality number for sorting."""
        if not self.judges:
            return None
        scored = [j.score for j in self.judges if j.score is not None]
        if not scored:
            # Fall back to pass rate of boolean judges
            bools = [j.passed for j in self.judges if j.passed is not None]
            if not bools:
                return None
            return sum(1 for b in bools if b) / len(bools)
        return sum(scored) / len(scored)


class ExperimentManifest(BaseModel):
    """Optional grouping: which scenarios × configs to run together."""

    id: str
    name: str
    scenario_ids: list[str]
    config_ids: list[str]
    repeats: int = 1
    hypothesis: str | None = None
    notes: str | None = None
