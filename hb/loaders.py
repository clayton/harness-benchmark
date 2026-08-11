"""Load and validate YAML scenarios and configs from disk."""

from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path

import yaml
from pydantic import ValidationError

from hb.models import Config, Scenario

ROOT = Path(__file__).resolve().parent.parent


def _read_yaml(path: Path) -> dict:
    data = yaml.safe_load(path.read_text(encoding="utf-8"))
    if data is None:
        raise ValueError(f"Empty YAML: {path}")
    if not isinstance(data, dict):
        raise ValueError(f"Expected mapping at root of {path}")
    return data


def load_scenario(path: Path) -> Scenario:
    raw = _read_yaml(path)
    raw.setdefault("id", path.stem)
    return Scenario.model_validate(raw)


def load_config(path: Path) -> Config:
    raw = _read_yaml(path)
    raw.setdefault("id", path.stem)
    return Config.model_validate(raw)


def discover_yaml(directory: Path) -> list[Path]:
    if not directory.exists():
        return []
    paths = list(directory.rglob("*.yaml")) + list(directory.rglob("*.yml"))
    return sorted({p.resolve() for p in paths})


def load_all_scenarios(root: Path | None = None) -> list[Scenario]:
    base = (root or ROOT) / "scenarios"
    return [load_scenario(path) for path in discover_yaml(base)]


def load_all_configs(root: Path | None = None) -> list[Config]:
    base = (root or ROOT) / "configs"
    return [load_config(path) for path in discover_yaml(base)]


def find_scenario(scenario_id: str, root: Path | None = None) -> Scenario:
    for s in load_all_scenarios(root):
        if s.id == scenario_id:
            return s
    path = Path(scenario_id)
    if path.exists():
        return load_scenario(path)
    raise FileNotFoundError(f"Scenario not found: {scenario_id}")


def find_config(config_id: str, root: Path | None = None) -> Config:
    for c in load_all_configs(root):
        if c.id == config_id:
            return c
    path = Path(config_id)
    if path.exists():
        return load_config(path)
    raise FileNotFoundError(f"Config not found: {config_id}")


@dataclass
class ValidationIssue:
    path: str
    message: str


@dataclass
class ValidationReport:
    scenarios_ok: int = 0
    configs_ok: int = 0
    issues: list[ValidationIssue] = field(default_factory=list)

    @property
    def ok(self) -> bool:
        return not self.issues


def validate_corpus(root: Path | None = None) -> ValidationReport:
    root = root or ROOT
    report = ValidationReport()
    seen_scenario_ids: set[str] = set()
    seen_config_ids: set[str] = set()

    for path in discover_yaml(root / "scenarios"):
        try:
            scenario = load_scenario(path)
            if scenario.id in seen_scenario_ids:
                report.issues.append(
                    ValidationIssue(str(path), f"Duplicate scenario id: {scenario.id}")
                )
            else:
                seen_scenario_ids.add(scenario.id)
                report.scenarios_ok += 1
        except (ValidationError, ValueError, yaml.YAMLError) as e:
            report.issues.append(ValidationIssue(str(path), str(e)))

    for path in discover_yaml(root / "configs"):
        try:
            config = load_config(path)
            if config.id in seen_config_ids:
                report.issues.append(
                    ValidationIssue(str(path), f"Duplicate config id: {config.id}")
                )
            else:
                seen_config_ids.add(config.id)
                report.configs_ok += 1
            # Non-manual configs should pin harness version for real experiments.
            # Manual / synthetic may omit it; warn as an issue only when harness
            # is a real product family without a version pin.
            if config.harness not in {"manual", "example", "synthetic"} and not config.harness_version:
                report.issues.append(
                    ValidationIssue(
                        str(path),
                        f"Config '{config.id}' has harness={config.harness} but no "
                        f"harness_version — pin the CLI/app version for reproducible claims",
                    )
                )
        except (ValidationError, ValueError, yaml.YAMLError) as e:
            report.issues.append(ValidationIssue(str(path), str(e)))

    return report
