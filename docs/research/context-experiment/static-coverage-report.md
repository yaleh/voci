# Static Known-Entities Coverage Report

Static canonical forms: 47

Coverage = tool_use entities covered by static list / total tool_use entities

Match rule: exact | prefix path (internal/context → .../builder.go) | function prefix


## Coverage by Entity Type and Project

| Entity Type | Project | Total | Covered | Coverage % |
|-------------|---------|-------|---------|------------|
| file_path | baime | 9174 | 0 | 0.0% |
| file_path | voci | 4482 | 428 | 9.5% |
| identifier | baime | 3961 | 0 | 0.0% |
| identifier | voci | 1911 | 51 | 2.7% |
| **all** | **all** | **19528** | **479** | **2.5%** |

## Top Uncovered Identifiers (all projects, identifier type)

| Entity | Occurrences |
|--------|-------------|
| `Bash` | 2009 |
| `---` | 298 |
| `--status` | 296 |
| `--plain` | 291 |
| `Edit` | 272 |
| `--append-notes` | 267 |
| `Read` | 254 |
| `Agent` | 223 |
| `ToolSearch` | 137 |
| `--plan` | 125 |
| `--show-toplevel` | 106 |
| `mcp__backlog__task_edit` | 102 |
| `--oneline` | 69 |
| `Write` | 60 |
| `--no-ff` | 55 |
| `--dod` | 49 |
| `--tasks-dir` | 42 |
| `--description` | 42 |
| `--force` | 40 |
| `Monitor` | 40 |
| `--interval` | 36 |
| `mcp__backlog__task_create` | 34 |
| `--help` | 31 |
| `mcp__backlog__task_archive` | 30 |
| `--serve` | 29 |
| `mcp__backlog__task_view` | 27 |
| `--manifest` | 27 |
| `Skill` | 26 |
| `--labels` | 24 |
| `--stop-file` | 24 |

## Top Uncovered File Paths (all projects)

| Path | Occurrences |
|------|-------------|
| `/dev/null` | 663 |
| `SKILL.md` | 480 |
| `/home/yale/work/baime` | 402 |
| `validate-plugin.sh` | 302 |
| `scripts/validate-plugin.sh` | 219 |
| `/home/yale/work/voci` | 118 |
| `gcl-events.jsonl` | 117 |
| `./...` | 107 |
| `main.go` | 104 |
| `plugin/skills/loop-backlog/SKILL.md` | 96 |
| `CLAUDE.md` | 91 |
| `docs/research/gcl-events.jsonl` | 87 |
| `ttb-plan.md` | 85 |
| `/home/yale/work/baime/plugin/skills/loop-backlog/SKILL.md` | 84 |
| `server.go` | 78 |
| `ftb-proposal.md` | 74 |
| `ftb-plan.md` | 71 |
| `backlog/tasks/` | 69 |
| `E/C/H` | 67 |
| `/home/yale/.claude/jobs/ec8485c1/tmp` | 62 |
| `/tmp/ttb-plan.md` | 53 |
| `./cmd/voci` | 51 |
| `settings.json` | 48 |
| `install.sh` | 44 |
| `grounding-infrastructure.md` | 44 |
| `ttb-plan-verdict.txt` | 42 |
| `gcl-synthesis.md` | 41 |
| `skill-quality-engineering.md` | 41 |
| `builder.go` | 39 |
| `/home/yale/work/voci/cmd/voci/main.go` | 39 |

## Key Finding

- Static list covers **2.5%** of all identifier+file_path tool-use entities
- Dynamic (uncovered): **97.5%** of entities have no static hint entry
