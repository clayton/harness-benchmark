from hb.dialogue import extract_question, build_sut_user_message, DialogueLog


def test_extract_question_plain():
    text = """
Looking at the code...

STAKEHOLDER_QUESTION:
Should OpenAPI include $ref for JSONL items when using include_router?
STAKEHOLDER_END

I'll wait.
"""
    q = extract_question(text)
    assert q is not None
    assert "OpenAPI" in q
    assert "$ref" in q


def test_extract_question_none():
    assert extract_question("Just implementing the fix now.") is None


def test_extract_ignores_protocol_docs_in_user_json():
    # Simulated pi log: user message contains protocol instructions
    log = (
        '{"type":"message_end","message":{"role":"user","content":[{"type":"text","text":'
        '"STAKEHOLDER_QUESTION:\\nwrite your actual question here\\nSTAKEHOLDER_END"}]}}\n'
        '{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":'
        '"I will implement without questions."}]}}\n'
    )
    assert extract_question(log) is None


def test_build_message_includes_answers():
    d = DialogueLog()
    d.add("sut_question", "What should itemSchema look like?")
    d.add("stakeholder", "It should $ref the Item model.")
    msg = build_sut_user_message("Fix the bug.", d, include_protocol=True)
    assert "STAKEHOLDER_ANSWER" in msg
    assert "Item model" in msg
    assert "STAKEHOLDER_QUESTION" in msg
