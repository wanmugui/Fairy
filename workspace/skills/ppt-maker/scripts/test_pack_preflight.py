"""pack_preflight.py 回归测试 —— 重点覆盖模板模式参数校验（曾误用 ppt_mode=="html" 导致 template 缺参被放行）。

运行： uv run pytest skills/ppt-maker/scripts/test_pack_preflight.py
"""
import json
import subprocess
import sys
from pathlib import Path

try:
    import load_env
    load_env.load()
except (ImportError, AttributeError):
    pass

SCRIPT = Path(__file__).with_name("pack_preflight.py")

_REQUIRED = [
    "schema_version", "deck_dir", "user_query", "topic", "language", "language_decision",
    "ppt_mode", "mode", "page_count_desc", "speaker_identity", "audience_identity", "use_case",
    "template_params", "content_gap_assessment", "supplemental_info_tasks", "visual_strategy",
]


def _write_task_pack(deck: Path, **overrides) -> None:
    data = {k: "x" for k in _REQUIRED}
    data.update({
        "deck_dir": str(deck),
        "language": "zh",
        "language_decision": {
            "source": "query_fallback", "matched_signal": "",
            "chosen_language": "zh", "note": "x",
        },
        "ppt_mode": "no-template",
        "mode": "ppt-no-template-mode",
        "visual_strategy": "mixed",
        "template_params": {},
        "content_gap_assessment": {"has_gap": False},
        "supplemental_info_tasks": [],
    })
    data.update(overrides)
    (deck / "task_pack.json").write_text(json.dumps(data, ensure_ascii=False), encoding="utf-8")


def _check(deck: Path) -> int:
    return subprocess.run(
        [sys.executable, str(SCRIPT), "--check", "task_pack", "--deck", str(deck)],
        capture_output=True, text=True,
    ).returncode


def test_no_template_passes(tmp_path):
    _write_task_pack(tmp_path, ppt_mode="no-template")
    assert _check(tmp_path) == 0


def test_missing_language_fails(tmp_path):
    """回归：language 必填——缺失时下游会静默回落成 zh，必须在校验门拦下。"""
    _write_task_pack(tmp_path, ppt_mode="no-template")
    path = tmp_path / "task_pack.json"
    data = json.loads(path.read_text(encoding="utf-8"))
    data.pop("language")
    path.write_text(json.dumps(data, ensure_ascii=False), encoding="utf-8")
    assert _check(tmp_path) == 2


def test_missing_language_decision_fails(tmp_path):
    """回归：language_decision 必填——这是 language 判定的留痕，缺失=模型跳过了判定。"""
    _write_task_pack(tmp_path)
    path = tmp_path / "task_pack.json"
    data = json.loads(path.read_text(encoding="utf-8"))
    data.pop("language_decision")
    path.write_text(json.dumps(data, ensure_ascii=False), encoding="utf-8")
    assert _check(tmp_path) == 2


def test_language_decision_mismatch_fails(tmp_path):
    """回归：language_decision.chosen_language 必须等于顶层 language。"""
    _write_task_pack(tmp_path, language="en")  # language_decision.chosen_language 仍是 zh
    assert _check(tmp_path) == 2


def test_language_decision_explicit_requires_signal(tmp_path):
    """回归：source=explicit_requirement 却拿不出 matched_signal 必须被拦下。"""
    _write_task_pack(tmp_path, language_decision={
        "source": "explicit_requirement", "matched_signal": "",
        "chosen_language": "zh", "note": "x",
    })
    assert _check(tmp_path) == 2


def test_language_decision_fallback_forbids_signal(tmp_path):
    """回归：source=query_fallback 却填了 matched_signal（自相矛盾）必须被拦下。"""
    _write_task_pack(tmp_path, language_decision={
        "source": "query_fallback", "matched_signal": "role says English",
        "chosen_language": "zh", "note": "x",
    })
    assert _check(tmp_path) == 2


def test_language_decision_bad_source_fails(tmp_path):
    """回归：source 非法枚举值必须被拦下。"""
    _write_task_pack(tmp_path, language_decision={
        "source": "guess", "matched_signal": "",
        "chosen_language": "zh", "note": "x",
    })
    assert _check(tmp_path) == 2


def test_language_decision_explicit_valid_passes(tmp_path):
    """正例：命中显式英文要求、留痕自洽 → 放行。"""
    _write_task_pack(tmp_path, language="en", language_decision={
        "source": "explicit_requirement",
        "matched_signal": "角色设定：『无论用户输入什么语言，整个过程你都用英文输出』",
        "chosen_language": "en", "note": "人设指定全程英文",
    })
    assert _check(tmp_path) == 0


def test_template_missing_params_fails(tmp_path):
    """回归：ppt_mode=template + 空 template_params 必须被拦下（曾因比对 'html' 而放行）。"""
    _write_task_pack(tmp_path, ppt_mode="template", mode="ppt-template-mode", template_params={})
    assert _check(tmp_path) == 2


def test_template_bad_paths_fail(tmp_path):
    _write_task_pack(
        tmp_path, ppt_mode="template", mode="ppt-template-mode",
        template_params={
            "template_name": "t",
            "template_tags_path": "/no/such/tags.json",
            "template_html_dir": "/no/such/dir",
        },
    )
    assert _check(tmp_path) == 2


def test_illegal_ppt_mode_fails(tmp_path):
    """回归：非法 ppt_mode（如 'unknown'）必须被拦下，不能 ok 放行。"""
    _write_task_pack(tmp_path, ppt_mode="unknown", mode="ppt-template-mode")
    assert _check(tmp_path) == 2


def test_illegal_mode_fails(tmp_path):
    """回归：mode 不在合法 Skill 名集合内必须被拦下。"""
    _write_task_pack(tmp_path, ppt_mode="no-template", mode="some-bogus-skill")
    assert _check(tmp_path) == 2


def test_mode_mapping_mismatch_fails(tmp_path):
    """回归：ppt_mode 与 mode 映射不一致（no-template ↛ ppt-template-mode）必须被拦下。"""
    _write_task_pack(tmp_path, ppt_mode="no-template", mode="ppt-template-mode")
    assert _check(tmp_path) == 2


def test_template_valid_passes(tmp_path):
    tags = tmp_path / "tags.json"
    tags.write_text("{}", encoding="utf-8")
    html_dir = tmp_path / "htmls"
    html_dir.mkdir()
    _write_task_pack(
        tmp_path, ppt_mode="template", mode="ppt-template-mode",
        template_params={
            "template_name": "t",
            "template_tags_path": str(tags),
            "template_html_dir": str(html_dir),
        },
    )
    assert _check(tmp_path) == 0
