"""Build testcases-annotated.json from testcases.json + expected_kind mappings."""
import json, pathlib

EXPECTED_KIND = {
    "sample-01": "direct_prompt",
    "sample-02": "direct_prompt",
    "sample-03": "direct_prompt",
    "sample-04": "ambiguous",
    "sample-05": "direct_prompt",
    "sample-06": "backlog_action",
    "sample-07": "direct_prompt",
    "sample-08": "direct_prompt",
    "sample-09": "query",
    "sample-10": "direct_prompt",
    "sample-11": "direct_prompt",
    "sample-12": "direct_prompt",
    "sample-13": "query",
    "sample-14": "query",
    "sample-15": "direct_prompt",
    "sample-16": "backlog_action",
    "sample-17": "direct_prompt",
    "sample-18": "direct_prompt",
    "sample-19": "query",
    "sample-20": "direct_prompt",
    "sample-21": "backlog_action",
    "sample-22": "direct_prompt",
    "sample-23": "query",
    "sample-24": "query",
    "sample-25": "direct_prompt",
    "sample-26": "direct_prompt",
    "sample-27": "query",
    "sample-28": "backlog_action",
    "sample-29": "direct_prompt",
    "sample-30": "direct_prompt",
    "sample-31": "direct_prompt",
    "sample-32": "query",
    "sample-33": "query",
    "sample-34": "direct_prompt",
    "sample-35": "direct_prompt",
}

# Expected rewrite mappings (from tts_input / expected_hinted)
EXPECTED_REWRITE = {
    "sample-01": "fix the TASK-1 login bug in the voci project",
    "sample-02": "add logging to the voci CLI command handler",
    "sample-03": "update the context builder in internal/context/builder.go",
    "sample-04": "make it faster somehow",
    "sample-05": "rewrite the TASK-3 handler in the voci package to use channels",
    "sample-06": "TASK-5 and TASK-8 need to be merged before release",
    "sample-07": "run the voci command with --file option",
    "sample-08": "pass the --iterate flag to the voci binary",
    "sample-09": "look at the internal/pipeline package for the chat function",
    "sample-10": "fix the internal/asr module transcription timeout",
    "sample-11": "the RunHinted function needs a new system prompt",
    "sample-12": "call BuildContext before the pipeline stage",
    "sample-13": "TASK-6 is blocked by TASK-2 and TASK-8",
    "sample-14": "TASK-8 has the prompt changes for RunHinted",
    "sample-15": "update RunHinted in internal/pipeline to handle TASK-4 edge case",
    "sample-16": "把任务二十九推到 ready 状态",
    "sample-17": "运行所有测试，看看有没有失败",
    "sample-18": "提交代码，写提交信息",
    "sample-19": "检查最近的改动有没有问题",
    "sample-20": "新建一个分支来做这个功能",
    "sample-21": "把这个 bug 加到 backlog 里",
    "sample-22": "修复 BuildContextWithSource 里的 bug",
    "sample-23": "检查 builder.go 的逻辑",
    "sample-24": "DynamicEntitiesSource 的测试挂了，看看怎么回事",
    "sample-25": "给 RunHinted 加一个单元测试",
    "sample-26": "pipeline.go 里的 Rewrite 函数需要优化",
    "sample-27": "检查 SILICONFLOW_API_KEY 有没有配置",
    "sample-28": "Push task 29 to ready",
    "sample-29": "Run the tests and check the output",
    "sample-30": "把 --iterate flag 加到命令里",
    "sample-31": "用 go test --run 跑一下这个测试",
    "sample-32": "Check the internal/asr module for timeout issues",
    "sample-33": "把 TASK-32 的 known entities 逻辑 review 一下",
    "sample-34": "Fix the DynamicEntitiesSource benchmark",
    "sample-35": "给 gemma4 的 adapter 加 supports_hints 字段",
}

root = pathlib.Path(__file__).resolve().parent.parent.parent.parent
testcases_path = root / "testdata" / "testcases.json"
cases = json.loads(testcases_path.read_text())

annotated = []
for c in cases:
    sid = c["id"]
    # expected_rewrite: use expected_hinted if non-empty, else use the mapping
    if c.get("expected_hinted"):
        expected_rewrite = c["expected_hinted"]
    else:
        expected_rewrite = EXPECTED_REWRITE.get(sid, c.get("tts_input", ""))
    c["expected_rewrite"] = expected_rewrite
    c["expected_kind"] = EXPECTED_KIND.get(sid, "ambiguous")
    annotated.append(c)

out_path = pathlib.Path(__file__).resolve().parent / "testcases-annotated.json"
out_path.write_text(json.dumps(annotated, ensure_ascii=False, indent=2))
print(f"Wrote {len(annotated)} cases to {out_path}")
