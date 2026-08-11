"""Stakeholder Q&A: parse SUT questions, answer via proxy or human, log dialogue."""

from __future__ import annotations

import json
import re
import subprocess
import time
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path

from hb.models import Scenario, StakeholderProxy, Telemetry
from hb.telemetry import merge_telemetry, parse_agent_log, parse_pi_jsonl

# SUT must emit this block when it needs stakeholder input (see protocol prompt).
# Require END marker so we never greedily swallow the rest of a huge log.
QUESTION_RE = re.compile(
    r"STAKEHOLDER_QUESTION:\s*\n?(.*?)\nSTAKEHOLDER_END\b",
    re.DOTALL | re.IGNORECASE,
)

PROTOCOL_APPENDIX = """
---
INTERACTION PROTOCOL (mandatory when you need information):
If you need clarification from a stakeholder (product owner / maintainer), do NOT
guess product intent. Output a real question using this exact wire format
(replace the body; do not copy this instruction block):

STAKEHOLDER_QUESTION:
write your actual question here as one or more sentences
STAKEHOLDER_END

You will then receive a line starting with STAKEHOLDER_ANSWER and may continue.
When you have enough information, implement the fix and tests without further questions.
Do not invent stakeholder answers yourself.
Only emit STAKEHOLDER_QUESTION when you truly need product/behavior clarification.
""".strip()


def extract_question(text: str) -> str | None:
    """Return the question body if the *assistant* asked via the protocol."""
    # Only assistant-visible text — never match the user prompt's protocol docs
    plain = (
        _pi_json_to_plain(text, roles=("assistant",))
        if '"type":' in text or '"role"' in text
        else text
    )
    # Prefer the last well-formed question in assistant output
    matches = list(QUESTION_RE.finditer(plain))
    if not matches:
        return None
    q = matches[-1].group(1).strip()
    # Filter placeholders / instruction echoes
    if not q or len(q) > 4000:
        return None
    low = q.lower()
    if "write your actual question" in low or "your question" == low.strip("<> "):
        return None
    if "do not invent" in low or "wire format" in low:
        return None
    return q


def _pi_json_to_plain(text: str, roles: tuple[str, ...] = ("assistant",)) -> str:
    chunks: list[str] = []
    for line in text.splitlines():
        line = line.strip()
        if not line.startswith("{"):
            # Non-JSON lines only kept for non-JSON logs
            if '"type":' not in text:
                chunks.append(line)
            continue
        try:
            obj = json.loads(line)
        except json.JSONDecodeError:
            continue
        if not isinstance(obj, dict):
            continue
        msg = obj.get("message") if isinstance(obj.get("message"), dict) else None
        if not msg or msg.get("role") not in roles:
            continue
        content = msg.get("content")
        if isinstance(content, list):
            for part in content:
                if isinstance(part, dict) and part.get("type") == "text":
                    chunks.append(str(part.get("text") or ""))
        elif isinstance(content, str):
            chunks.append(content)
    return "\n".join(chunks)


@dataclass
class DialogueTurn:
    role: str  # sut | stakeholder | system
    content: str
    at: str = field(default_factory=lambda: datetime.now(timezone.utc).isoformat())
    tokens_in: int | None = None
    tokens_out: int | None = None
    estimated_usd: float | None = None


@dataclass
class DialogueLog:
    turns: list[DialogueTurn] = field(default_factory=list)

    def add(self, role: str, content: str, tel: Telemetry | None = None) -> None:
        self.turns.append(
            DialogueTurn(
                role=role,
                content=content,
                tokens_in=tel.tokens_in if tel else None,
                tokens_out=tel.tokens_out if tel else None,
                estimated_usd=tel.estimated_usd if tel else None,
            )
        )

    def to_json(self) -> str:
        return json.dumps(
            {
                "turns": [
                    {
                        "role": t.role,
                        "content": t.content,
                        "at": t.at,
                        "tokens_in": t.tokens_in,
                        "tokens_out": t.tokens_out,
                        "estimated_usd": t.estimated_usd,
                    }
                    for t in self.turns
                ]
            },
            indent=2,
        ) + "\n"

    def proxy_telemetry(self) -> Telemetry:
        tin = tout = 0
        cost = 0.0
        n = 0
        for t in self.turns:
            if t.role != "stakeholder":
                continue
            n += 1
            tin += t.tokens_in or 0
            tout += t.tokens_out or 0
            cost += t.estimated_usd or 0.0
        if n == 0:
            return Telemetry(qa_rounds=0)
        return Telemetry(
            proxy_tokens_in=tin or None,
            proxy_tokens_out=tout or None,
            proxy_estimated_usd=cost or None,
            qa_rounds=n,
        )


def build_stakeholder_system(scenario: Scenario) -> str:
    brief = (scenario.stakeholder_brief or "").strip()
    acceptance = scenario.acceptance
    tests = "\n".join(f"- {c}" for c in acceptance.test_commands) or "- (none listed)"
    ftp = "\n".join(f"- {c}" for c in acceptance.fail_to_pass) or "- (none listed)"
    return f"""You are a project stakeholder / maintainer answering clarifying questions
from a coding agent fixing a bug. You know product intent and acceptance criteria.
You do NOT know (or must not reveal) the gold-standard source code patch.

Rules:
- Answer helpfully about expected behavior, edge cases, OpenAPI/API contracts, tests.
- Prefer concrete examples (inputs/outputs, schema snippets) over vague advice.
- NEVER provide the implementation patch, exact code diffs, or "change this line to…".
- NEVER invent requirements that contradict the brief.
- If you do not know, say so and restate the acceptance criteria.
- Keep answers concise (under ~200 words unless examples need more).

## Task title
{scenario.title}

## Stakeholder brief (privileged)
{brief or "(No extra brief — use the public description and acceptance only.)"}

## Public description
{scenario.description}

## Acceptance tests that must pass after the fix
{tests}

## FAIL_TO_PASS test ids (behavior to restore)
{ftp}

## Source
{scenario.source or "n/a"}
"""


def answer_as_proxy(
    question: str,
    scenario: Scenario,
    proxy: StakeholderProxy,
    *,
    timeout: int = 120,
    work_dir: Path | None = None,
) -> tuple[str, Telemetry]:
    """Call a cheap headless pi session with no tools to answer as stakeholder."""
    system = build_stakeholder_system(scenario)
    user = f"STAKEHOLDER_QUESTION from the coding agent:\n\n{question}\n\nReply with the answer only."
    combined = f"{system}\n\n---\n\n{user}"
    # Write to file to avoid ARG_MAX
    work_dir = work_dir or Path("/tmp")
    work_dir.mkdir(parents=True, exist_ok=True)
    prompt_file = work_dir / "proxy_prompt.txt"
    prompt_file.write_text(combined, encoding="utf-8")
    cmd = (
        f'pi -p --mode json --no-session --no-tools --no-extensions --no-skills '
        f'--provider {proxy.provider} --model {proxy.model} "$(cat {prompt_file})"'
    )
    # Still can hit ARG_MAX if file is huge — keep question capped already
    if len(combined) > 100_000:
        raise ValueError(f"proxy prompt too large ({len(combined)} chars)")
    proc = subprocess.run(
        cmd,
        shell=True,
        capture_output=True,
        text=True,
        timeout=timeout,
        check=False,
        cwd=str(work_dir),
    )
    raw = (proc.stdout or "") + "\n" + (proc.stderr or "")
    tel = parse_pi_jsonl(raw)
    answer = _pi_json_to_plain(raw, roles=("assistant",)).strip()
    if not answer:
        answer = raw.strip() or "(proxy produced empty answer)"
    answer = re.sub(
        r"STAKEHOLDER_QUESTION:.*", "", answer, flags=re.DOTALL | re.IGNORECASE
    ).strip()
    return answer, tel


def answer_as_human(
    question: str,
    run_dir: Path,
    *,
    wait_seconds: int = 3600,
) -> str:
    """
    Write question to disk and wait for human answer file or stdin.

    Files:
      run_dir/stakeholder_question.txt  — current question
      run_dir/stakeholder_answer.txt    — human writes answer, then saves
    """
    q_path = run_dir / "stakeholder_question.txt"
    a_path = run_dir / "stakeholder_answer.txt"
    if a_path.exists():
        a_path.unlink()
    q_path.write_text(question.strip() + "\n", encoding="utf-8")
    print("\n" + "=" * 60)
    print("STAKEHOLDER_QUESTION (human-in-the-loop)")
    print("=" * 60)
    print(question.strip())
    print("=" * 60)
    print(f"Write your answer to:\n  {a_path}")
    print("Or type the answer below and end with a single line: END")
    print("=" * 60 + "\n")

    # Prefer file if created; also accept stdin multi-line until END
    # Non-blocking poll file first for automation scripts that write answers
    deadline = time.time() + wait_seconds
    # Try short stdin only if TTY — otherwise poll file
    import sys

    if sys.stdin.isatty():
        lines: list[str] = []
        print("(stdin) answer, then END:")
        while time.time() < deadline:
            try:
                line = input()
            except EOFError:
                break
            if line.strip() == "END":
                break
            lines.append(line)
        if lines:
            ans = "\n".join(lines).strip()
            a_path.write_text(ans + "\n", encoding="utf-8")
            return ans

    while time.time() < deadline:
        if a_path.exists() and a_path.stat().st_size > 0:
            return a_path.read_text(encoding="utf-8").strip()
        time.sleep(1.0)
    raise TimeoutError(f"No human answer within {wait_seconds}s (expected {a_path})")


def build_sut_user_message(
    base_prompt: str,
    dialogue: DialogueLog,
    *,
    include_protocol: bool,
) -> str:
    parts = [base_prompt.strip()]
    if include_protocol:
        parts.append(PROTOCOL_APPENDIX)
    # Append prior Q&A
    qa = [t for t in dialogue.turns if t.role in ("sut_question", "stakeholder")]
    if qa:
        parts.append("--- Prior stakeholder dialogue ---")
        for t in qa:
            if t.role == "sut_question":
                parts.append(f"STAKEHOLDER_QUESTION:\n{t.content}\nSTAKEHOLDER_END")
            else:
                parts.append(f"STAKEHOLDER_ANSWER:\n{t.content}")
        parts.append(
            "Continue from here. If you still need info, ask another STAKEHOLDER_QUESTION. "
            "Otherwise implement and verify."
        )
    return "\n\n".join(parts)
