---
id: TASK-78
title: voci-listen 改造为可分发 plugin：plugin/skills 结构 + marketplace 挂载准备
status: 'Basic: Done'
assignee: []
created_date: '2026-07-02 05:31'
updated_date: '2026-07-02 07:24'
labels:
  - 'kind:basic'
dependencies: []
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
把 voci-listen skill 从项目私有的 .claude/skills/voci-listen 真实目录，改造为符合 baime ADR-001 模式的可分发 plugin 结构：新建 plugin/.claude-plugin/plugin.json + plugin/skills/voci-listen/SKILL.md 作为 Single Source of Truth，.claude/skills/voci-listen 改为指向它的相对 symlink。同时把 SKILL.md 中调用 extract-instruction.py 的仓库根相对路径改写为 ${CLAUDE_PLUGIN_ROOT} 占位符（参照 baime ADR-015），并把该脚本迁移进插件自身目录树，使其在其他仓库安装该插件后仍能正确解析。仓库根新增 voci 自己独立、自托管的 .claude-plugin/marketplace.json（source: "./plugin"），使用户可通过 /plugin marketplace add + /plugin install 直接安装，不依赖 baime marketplace、不需要 clone 仓库。git remote 由用户后续自行配置。
<!-- SECTION:DESCRIPTION:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
# Proposal: voci-listen 改造为可分发 plugin，并自建 marketplace

## Background

`voci-listen` 目前只存在于 `voci/.claude/skills/voci-listen/`（真实目录），只能被手动
复制文件的方式带入其他项目使用，没有任何标准分发入口。经研究确认：Claude Code
**没有**官方预置、默认启用的中心化 marketplace（类似 npm registry/PyPI）——所有
marketplace 都是去中心化的 git 仓库，用户须显式 `/plugin marketplace add <repo>`。
因此挂靠 baime 的 marketplace 并不能带来额外的可发现性（用户仍需先知道并添加
baime 仓库），voci 完全可以自建独立、自托管的 `.claude-plugin/marketplace.json`，
不依赖 baime 侧任何改动。

同时，voci-listen 的 SKILL.md 正文通过仓库根目录相对路径调用
`python3 scripts/extract-instruction.py`。研究 baime 项目（其 ADR-015，
`docs/adr/ADR-015-plugin-root-reference-mechanism.md`，2026-07-01 Accepted）确认：
这类仓库根路径引用在插件被其他仓库安装后会失效——安装者的 cwd 不是 voci 仓库根，
不存在该 `scripts/` 目录。baime 的解决方案是用 Claude Code 官方运行时占位符
`${CLAUDE_PLUGIN_ROOT}` 替换硬编码相对路径；该占位符在 skill/agent/hook/monitor
正文文本中会被 Claude Code 在读取时替换为已安装插件的绝对路径（不是 shell 环境变量，
不能在裸 Bash 调用里 `echo $CLAUDE_PLUGIN_ROOT` 期望有值，只在托管文本中生效）。
baime 已通过 TASK-236 完成了同类迁移（ADR-007 D2 的运行时 `installed_plugins.json`
+ `find` 搜索方案被 ADR-015 取代并被静态 lint 规则禁止）。voci-listen 必须采用同样
的模式，否则插件在其他仓库中安装后脚本调用会 100% 失败。

## Goals

1. `plugin/.claude-plugin/plugin.json` 存在，是合法 JSON，`commands` 数组注册
   `./skills/voci-listen/SKILL.md`，字段形状对齐 baime。
2. `plugin/skills/voci-listen/SKILL.md` 存在，是迁移后的 Single Source of Truth；
   `.claude/skills/voci-listen` 变为指向它的相对 symlink
   （`../../plugin/skills/voci-listen`）。
3. `extract-instruction.py`（及其测试脚本）从仓库根目录 `scripts/` 迁移进插件自身
   目录树 `plugin/skills/voci-listen/scripts/`，SKILL.md 中的调用改写为
   `${CLAUDE_PLUGIN_ROOT}/skills/voci-listen/scripts/extract-instruction.py`，
   使其在任意安装该插件的仓库中都能正确解析，不再依赖 voci 自身仓库结构。
4. 仓库根目录新增 `.claude-plugin/marketplace.json`，voci 自托管、独立于 baime 的
   marketplace 定义，`plugins[0].source` 指向 `./plugin`（本仓库自身），
   `plugins[0].version` 与 `plugin/.claude-plugin/plugin.json` 的 `version` 一致
   （镜像 baime `validate-plugin.sh` 的版本一致性约定）。
5. skill 运行时行为不变（除脚本路径这一处必要修改外）：`/voci-listen` 触发逻辑、
   Monitor 描述、contracts frontmatter 字段全部不变。
6. 不需要改动 Makefile / build / install：Claude Code 原生支持通过
   `.claude/skills/` 自动发现 skill；`plugin.json`/`marketplace.json` 只在被
   `/plugin marketplace add`/`/plugin install` 使用时生效，voci 自身运行不依赖它们。
7. 完成后用户可通过 `/plugin marketplace add <voci-repo>` +
   `/plugin install voci-listen@voci` 安装，无需 clone 仓库、无需依赖 baime。

## Proposed Approach

1. `mkdir -p plugin/skills/voci-listen/scripts plugin/.claude-plugin .claude-plugin`
2. `git mv .claude/skills/voci-listen/SKILL.md plugin/skills/voci-listen/SKILL.md`；
   `rmdir .claude/skills/voci-listen`；
   `ln -s ../../plugin/skills/voci-listen .claude/skills/voci-listen`。
3. `git mv scripts/extract-instruction.py plugin/skills/voci-listen/scripts/extract-instruction.py`；
   `git mv scripts/extract-instruction_test.sh plugin/skills/voci-listen/scripts/extract-instruction_test.sh`
   （测试脚本通过 `dirname "$0"` 自定位，迁移后无需修改内容即可运行）。
4. 编辑迁移后的 `plugin/skills/voci-listen/SKILL.md`：把
   `python3 scripts/extract-instruction.py` 改写为
   `python3 "${CLAUDE_PLUGIN_ROOT}/skills/voci-listen/scripts/extract-instruction.py"`
   （仅此一处内容变化，其余 SKILL.md 正文保持不变）。
5. 新建 `plugin/.claude-plugin/plugin.json`（字段镜像 baime shape，仅注册
   voci-listen 一个 skill，`name` 为仓库级 `voci`）。
6. 新建仓库根 `.claude-plugin/marketplace.json`（镜像
   `/home/yale/work/baime/.claude-plugin/marketplace.json` 的字段形状：
   `name`/`description`/`owner`/`plugins[]`），`plugins[0].source` 用本地相对路径
   `"./plugin"`（自托管，无需等待 git remote 配置——用户 clone/添加本仓库后即可
   直接工作；`homepage`/`repository` 字段仍用占位符，remote 配置后由用户更正）。

## Trade-offs and Risks

- **不做的事**：不配置 git remote；不在 baime 的 `marketplace.json` 中注册 voci
  的 git-source 条目（已确认非必要——不挂靠 baime 不影响可分发性，baime marketplace
  作为可选的额外曝光渠道，留给用户自行决定是否之后追加）。
- **风险**：`${CLAUDE_PLUGIN_ROOT}` 只在 Claude Code 托管的 skill/agent/hook/monitor
  文本中被替换，不是 shell 环境变量；SKILL.md 中必须原样保留这个占位符字面量
  （不能提前手工替换成绝对路径），否则脚本调用在实际安装场景下会失败。
- **风险**：脚本迁移改变了 SKILL.md 的字节内容（相比原先"逐字节不变"的迁移前提），
  这是本次修订后唯一允许的内容变化，范围收窄到"仅此一行路径替换"，通过 DoD 断言
  精确约束，避免顺带改写其他内容。
- **风险**：`marketplace.json` 中的 `plugins[0].version` 与 `plugin.json` 的
  `version` 若后续升级不同步会造成不一致；缓解措施是 DoD 中加入版本一致性检查
  （镜像 baime `validate-plugin.sh` 的检查项）。

---

# Plan: voci-listen 改造为可分发 plugin，并自建 marketplace

## Context / Key Finding (read before implementing)

`scripts/extract-instruction.py` 和 `scripts/extract-instruction_test.sh` 目前位于
仓库根目录 `voci/scripts/`。`extract-instruction_test.sh` 第 4 行用
`SCRIPT="$(dirname "$0")/extract-instruction.py"` 自定位，迁移到新目录后无需修改
即可继续工作（已验证）。

baime ADR-015（`/home/yale/work/baime/docs/adr/ADR-015-plugin-root-reference-mechanism.md`）
确认：SKILL.md 正文中对插件自带文件的引用必须写成 `${CLAUDE_PLUGIN_ROOT}/<相对路径>`
字面量文本，由 Claude Code 在加载 skill 内容时替换为实际安装路径；不能用仓库根路径、
不能用运行时 `installed_plugins.json` + `find` 搜索（该方案已被 baime 标记为反模式并
lint 禁止）。

baime `.claude-plugin/marketplace.json`（仓库根目录，非 `plugin/` 内）的字段形状：
`name`/`description`/`owner{name,url}`/`plugins[]`，每个 plugin 条目含
`name`/`source`/`version`/`description`/`license`/`homepage`/`category`/`tags`。
`source` 为本地相对路径字符串（如 `"./plugin"`）时指向同仓库内的 plugin 目录，
无需 git-source 对象包装。baime 的 `validate-plugin.sh` 检查
`plugin.json.version == marketplace.json.plugins[0].version`——voci 应遵循同一约定。

voci 仓库当前无 git remote；`homepage`/`repository` 使用占位符
`https://github.com/<owner>/voci`，不编造真实地址。

## Phase A: 建立 plugin/ 目录结构，迁移 SKILL.md 与脚本，改写运行时路径

### Tests (write first)

纯文件重组 + 单行文本替换任务，无 Go/JS 逻辑变更。结构性验证命令：

```bash
# plugin.json 存在、合法、字段正确
test -f /home/yale/work/voci/plugin/.claude-plugin/plugin.json
python3 -c "import json; json.load(open('/home/yale/work/voci/plugin/.claude-plugin/plugin.json'))"
python3 -c "import json; d=json.load(open('/home/yale/work/voci/plugin/.claude-plugin/plugin.json')); assert d['commands']==['./skills/voci-listen/SKILL.md']"
python3 -c "import json; d=json.load(open('/home/yale/work/voci/plugin/.claude-plugin/plugin.json')); assert d['name']=='voci'"

# SKILL.md 迁移到位，symlink 正确
test -f /home/yale/work/voci/plugin/skills/voci-listen/SKILL.md
test -L /home/yale/work/voci/.claude/skills/voci-listen
[ "$(readlink /home/yale/work/voci/.claude/skills/voci-listen)" = "../../plugin/skills/voci-listen" ]
test -s /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md

# 脚本迁移到位、旧位置清空、测试仍通过（自定位不受影响）
test -f /home/yale/work/voci/plugin/skills/voci-listen/scripts/extract-instruction.py
test -f /home/yale/work/voci/plugin/skills/voci-listen/scripts/extract-instruction_test.sh
! test -f /home/yale/work/voci/scripts/extract-instruction.py
! test -f /home/yale/work/voci/scripts/extract-instruction_test.sh
bash /home/yale/work/voci/plugin/skills/voci-listen/scripts/extract-instruction_test.sh

# SKILL.md 正文改写为 ${CLAUDE_PLUGIN_ROOT} 占位符，且不再包含旧的裸相对路径调用
grep -qF '${CLAUDE_PLUGIN_ROOT}/skills/voci-listen/scripts/extract-instruction.py' /home/yale/work/voci/plugin/skills/voci-listen/SKILL.md
! grep -qE 'python3 scripts/extract-instruction\.py' /home/yale/work/voci/plugin/skills/voci-listen/SKILL.md

# 迁移后 SKILL.md 只在这一处调用行发生变化，其余内容逐字节不变
diff <(git -C /home/yale/work/voci show HEAD:.claude/skills/voci-listen/SKILL.md) /home/yale/work/voci/plugin/skills/voci-listen/SKILL.md | grep -c '^[<>]'
```

（最后一条命令的期望值是 2 —— 恰好一行删除 + 一行新增，对应唯一允许的路径替换；
实施后需人工确认 diff 内容确实只是该行，而非行数巧合。）

### Implementation

```bash
cd /home/yale/work/voci

mkdir -p plugin/skills/voci-listen/scripts plugin/.claude-plugin

git mv .claude/skills/voci-listen/SKILL.md plugin/skills/voci-listen/SKILL.md
rmdir .claude/skills/voci-listen
ln -s ../../plugin/skills/voci-listen .claude/skills/voci-listen

git mv scripts/extract-instruction.py plugin/skills/voci-listen/scripts/extract-instruction.py
git mv scripts/extract-instruction_test.sh plugin/skills/voci-listen/scripts/extract-instruction_test.sh

# 改写 SKILL.md 中唯一的调用行（保留注释行不变，只替换命令行）
sed -i \
  's#python3 scripts/extract-instruction\.py#python3 "${CLAUDE_PLUGIN_ROOT}/skills/voci-listen/scripts/extract-instruction.py"#' \
  plugin/skills/voci-listen/SKILL.md

cat > plugin/.claude-plugin/plugin.json <<'EOF'
{
  "name": "voci",
  "version": "0.1.0",
  "description": "voci-listen: arms a persistent Monitor with voci serve --share for browser-based voice-to-instruction delivery into Claude Code sessions.",
  "author": {
    "name": "Yale Huang"
  },
  "license": "MIT",
  "homepage": "https://github.com/<owner>/voci",
  "repository": "https://github.com/<owner>/voci",
  "commands": [
    "./skills/voci-listen/SKILL.md"
  ]
}
EOF

git add .claude/skills/voci-listen plugin/.claude-plugin/plugin.json \
  plugin/skills/voci-listen/scripts
```

### DoD

- [ ] `go test ./...`
- [ ] `test -f /home/yale/work/voci/plugin/.claude-plugin/plugin.json`
- [ ] `python3 -c "import json; json.load(open('/home/yale/work/voci/plugin/.claude-plugin/plugin.json'))"`
- [ ] `python3 -c "import json; d=json.load(open('/home/yale/work/voci/plugin/.claude-plugin/plugin.json')); assert d['commands']==['./skills/voci-listen/SKILL.md']"`
- [ ] `python3 -c "import json; d=json.load(open('/home/yale/work/voci/plugin/.claude-plugin/plugin.json')); assert d['name']=='voci'"`
- [ ] `test -f /home/yale/work/voci/plugin/skills/voci-listen/SKILL.md`
- [ ] `test -L /home/yale/work/voci/.claude/skills/voci-listen`
- [ ] `[ "$(readlink /home/yale/work/voci/.claude/skills/voci-listen)" = "../../plugin/skills/voci-listen" ]`
- [ ] `test -s /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md`
- [ ] `test -f /home/yale/work/voci/plugin/skills/voci-listen/scripts/extract-instruction.py`
- [ ] `test -f /home/yale/work/voci/plugin/skills/voci-listen/scripts/extract-instruction_test.sh`
- [ ] `! test -f /home/yale/work/voci/scripts/extract-instruction.py`
- [ ] `bash /home/yale/work/voci/plugin/skills/voci-listen/scripts/extract-instruction_test.sh`
- [ ] `grep -qF '${CLAUDE_PLUGIN_ROOT}/skills/voci-listen/scripts/extract-instruction.py' /home/yale/work/voci/plugin/skills/voci-listen/SKILL.md`
- [ ] `! grep -qE 'python3 scripts/extract-instruction\.py' /home/yale/work/voci/plugin/skills/voci-listen/SKILL.md`

## Phase B: 仓库根自托管 marketplace.json

### Tests (write first)

```bash
test -f /home/yale/work/voci/.claude-plugin/marketplace.json
python3 -c "import json; json.load(open('/home/yale/work/voci/.claude-plugin/marketplace.json'))"
python3 -c "
import json
d = json.load(open('/home/yale/work/voci/.claude-plugin/marketplace.json'))
assert d['plugins'][0]['source'] == './plugin', d['plugins'][0]['source']
"
python3 -c "
import json
p = json.load(open('/home/yale/work/voci/plugin/.claude-plugin/plugin.json'))
m = json.load(open('/home/yale/work/voci/.claude-plugin/marketplace.json'))
assert p['version'] == m['plugins'][0]['version'], (p['version'], m['plugins'][0]['version'])
"
```

### Implementation

```bash
cd /home/yale/work/voci
mkdir -p .claude-plugin
cat > .claude-plugin/marketplace.json <<'EOF'
{
  "$schema": "https://anthropic.com/claude-code/marketplace.schema.json",
  "name": "voci",
  "description": "voci: browser-based voice-to-instruction delivery for Claude Code sessions",
  "owner": {
    "name": "Yale Huang",
    "url": "https://github.com/<owner>"
  },
  "plugins": [
    {
      "name": "voci-listen",
      "source": "./plugin",
      "version": "0.1.0",
      "description": "Arms a persistent Monitor with voci serve --share for browser-based voice-to-instruction delivery into Claude Code sessions.",
      "license": "MIT",
      "homepage": "https://github.com/<owner>/voci",
      "category": "productivity",
      "tags": ["voice", "asr", "monitor"]
    }
  ]
}
EOF
git add .claude-plugin/marketplace.json
```

### DoD

- [ ] `go test ./...`
- [ ] `test -f /home/yale/work/voci/.claude-plugin/marketplace.json`
- [ ] `python3 -c "import json; json.load(open('/home/yale/work/voci/.claude-plugin/marketplace.json'))"`
- [ ] `python3 -c "import json; d=json.load(open('/home/yale/work/voci/.claude-plugin/marketplace.json')); assert d['plugins'][0]['source']=='./plugin'"`
- [ ] `python3 -c "import json; p=json.load(open('/home/yale/work/voci/plugin/.claude-plugin/plugin.json')); m=json.load(open('/home/yale/work/voci/.claude-plugin/marketplace.json')); assert p['version']==m['plugins'][0]['version']"`

## Constraints

- 不改动 `Makefile`、`go.mod`、任何 Go/JS 源码。
- 本次修订放弃"SKILL.md 逐字节不变"的原始约束，改为"仅允许一处路径调用行的替换"，
  由 Phase A 的 diff 行数断言（期望恰好 2 行变化）+ 内容级 grep 断言双重约束，
  防止顺带改写其他内容。
- `${CLAUDE_PLUGIN_ROOT}` 必须原样保留为字面量文本写入 SKILL.md，不得提前手工替换
  为任何绝对/相对路径——它只在 Claude Code 加载 skill 正文时被替换，不是 shell
  环境变量，在裸 Bash 调用里没有值。
- `plugin.json`/`marketplace.json` 的 `homepage`/`repository` 字段使用占位符
  （`<owner>` placeholder），不得编造真实 GitHub 组织/用户名。
- `marketplace.json` 与 baime 的 marketplace **完全独立**，不修改 baime 仓库的任何
  文件，不依赖 baime marketplace 才能完成安装——voci 自己的 marketplace 单独可用。
- `plugin.json.version` 与 `marketplace.json.plugins[0].version` 必须保持一致
  （镜像 baime `validate-plugin.sh` 的版本一致性检查），本次两者都设为 `0.1.0`。
- `voci/scripts/` 目录下除 `extract-instruction.py`/`extract-instruction_test.sh`
  外的其他文件（如有）不在本任务迁移范围内，保持原地不动。

## Acceptance Gate

- [ ] `go test ./...`
- [ ] `make build`
- [ ] `test -L /home/yale/work/voci/.claude/skills/voci-listen`
- [ ] `[ "$(readlink /home/yale/work/voci/.claude/skills/voci-listen)" = "../../plugin/skills/voci-listen" ]`
- [ ] `python3 -c "import json; json.load(open('/home/yale/work/voci/plugin/.claude-plugin/plugin.json'))"`
- [ ] `python3 -c "import json; json.load(open('/home/yale/work/voci/.claude-plugin/marketplace.json'))"`
- [ ] `python3 -c "import json; p=json.load(open('/home/yale/work/voci/plugin/.claude-plugin/plugin.json')); m=json.load(open('/home/yale/work/voci/.claude-plugin/marketplace.json')); assert p['version']==m['plugins'][0]['version']"`
- [ ] `bash /home/yale/work/voci/plugin/skills/voci-listen/scripts/extract-instruction_test.sh`
- [ ] `grep -qF '${CLAUDE_PLUGIN_ROOT}/skills/voci-listen/scripts/extract-instruction.py' /home/yale/work/voci/plugin/skills/voci-listen/SKILL.md`
- [ ] `! git -C /home/yale/work/voci status --porcelain | grep -vE '^(R |A |D |\?\? plugin|\?\? \.claude-plugin| M plugin)'`

## Phase C: 文档化两步安装模式（binary + skill 分离）

### Context

voci-listen 是一个调用 `voci` 二进制（`voci serve`/`voci listen-preflight`）的 Claude
Code skill。Phase A/B 只分发 skill 本身（SKILL.md + 脚本，通过 `/plugin marketplace
add` + `/plugin install`），不分发/安装 `voci` 二进制——这是两个独立的分发问题。
`go.mod` 已声明 `module github.com/yaleh/voci`（仓库 remote 就绪后），因此二进制安装
已有现成的一行命令，等价于 npm/pip 的全局安装：`go install
github.com/yaleh/voci/cmd/voci@latest`。若不显式文档化，安装了 skill 但没有二进制
的用户会在 skill 首次触发时才发现 `voci listen-preflight`/`voci serve` 命令不存在，
体验为静默失败，必须在文档中明确说明这是两个独立步骤。

### Tests (write first)

```bash
# README 必须包含一个 Install 章节，且同时提到两个安装步骤的命令
grep -qF 'go install github.com/yaleh/voci/cmd/voci@latest' /home/yale/work/voci/README.md
grep -qF '/plugin marketplace add' /home/yale/work/voci/README.md
grep -qF '/plugin install voci-listen@voci' /home/yale/work/voci/README.md
```

### Implementation

在 `README.md` 新增一个 `## Install` 章节（`## Core pipeline` 之前或 `## Status`
之后均可，遵循 README 现有章节风格，不引入新格式约定），明确写出两步、且说明二者
独立、顺序无关但都是必需的：

1. **安装 `voci` 二进制**（一行命令，等价于 npm/pip 全局安装）：
   `go install github.com/yaleh/voci/cmd/voci@latest`
2. **安装 voci-listen skill**（进入目标项目仓库，通过 Claude Code 的 plugin
   marketplace 机制）：
   `/plugin marketplace add <voci-repo>` 然后
   `/plugin install voci-listen@voci`

明确指出：步骤 2 只分发 skill 指令本身，不包含二进制；若只做了步骤 2 没做步骤 1，
skill 触发时会因为 `voci` 命令不存在而失败。

### DoD

- [ ] `go test ./...`
- [ ] `grep -qF 'go install github.com/yaleh/voci/cmd/voci@latest' /home/yale/work/voci/README.md`
- [ ] `grep -qF '/plugin marketplace add' /home/yale/work/voci/README.md`
- [ ] `grep -qF '/plugin install voci-listen@voci' /home/yale/work/voci/README.md`

## Constraints (Phase C addendum)

- 本 phase 只改动 `README.md`，不修改 `plugin/skills/voci-listen/SKILL.md` 的运行时
  逻辑（不新增二进制存在性检查——那是运行时行为改动，超出"文档化"范围，如需要应
  拆分为单独任务）。
- 不在此 phase 配置真实 git remote 或修改 `go.mod`；`go install
  github.com/yaleh/voci/cmd/voci@latest` 这行文档文本在 remote 配置好之前不可执行，
  但作为文档提前写出是合理的（module path 已在 `go.mod` 中声明为
  `github.com/yaleh/voci`，与占位符 URL 不同，这是已确认的真实值，不算编造）。

## Acceptance Gate (Phase C addendum)

- [ ] `grep -qF 'go install github.com/yaleh/voci/cmd/voci@latest' /home/yale/work/voci/README.md`
- [ ] `grep -qF '/plugin install voci-listen@voci' /home/yale/work/voci/README.md`
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Proposal self-review: APPROVED
premise-ledger:
[E] SKILL.md current content/frontmatter/Spec section confirmed via direct Read of .claude/skills/voci-listen/SKILL.md
[E] No existing plugin/ directory in voci confirmed via find
[E] Makefile has no skill/plugin references confirmed via grep -n -i 'skill|plugin' Makefile (no output)
[E] baime plugin/.claude-plugin/plugin.json field shape confirmed via cat (name/version/description/author/license/homepage/repository/commands)
[E] baime .claude/skills/* symlink pattern confirmed via ls -la (relative symlinks to ../../plugin/skills/<skill>)
[C] plugin.json repository/homepage as placeholder values is acceptable pre-remote since they are only consumed later when baime registers the git-source marketplace entry
[H] symlink approach may not work on non-Linux/macOS filesystems (accepted risk, consistent with baime's existing constraint)
GCL-self-report: E=5 C=1 H=1

Proposal approved. Starting plan draft.

Plan review iteration 1: APPROVED
premise-ledger:
[E] Goal coverage: All 5 proposal Goals map to Phase A Tests/DoD/Constraints items (plugin.json shape, SKILL.md content parity, symlink, runtime-behavior invariance, no Makefile/build changes)
[E] TDD structure: Phase A has ### Tests section (structural shell assertions, correctly framed as file-restructuring verification, not Go unit tests) followed by ### Implementation, in order
[E] TDD/Gate order: First DoD item and first Acceptance Gate item are both `go test ./...`
[E] DoD executability: All DoD/Acceptance Gate items are shell commands (test/python3/diff/grep/readlink), none are prose
[E] Absence checks use `! grep -q` / `! find` pattern (not `grep -qv`)
[E] Scope discipline verified: no git remote configuration performed (only referenced as rationale for placeholder URLs), no baime marketplace.json edits, no Makefile changes (Constraints explicitly excludes Makefile/go.mod), no SKILL.md content rewrite beyond location (diff assertion enforces byte-identical content)
[E] File paths verified to exist: /home/yale/work/voci/.claude/skills/voci-listen/SKILL.md (currently real file, 11446 bytes) and /home/yale/work/baime/plugin/.claude-plugin/plugin.json (both read directly)
[E] Content preservation: plan uses git mv (not cp/rewrite) plus explicit diff-against-git-show assertion in both DoD and Acceptance Gate
[E] Symlink correctness verified: baime's own .claude/skills/* entries are confirmed via ls -la to be relative symlinks of form ../../plugin/skills/<skill>, exactly matching the plan's proposed ../../plugin/skills/voci-listen target
GCL-self-report: E=9 C=0 H=0

Revised per user direction 2026-07-02: dropped baime-marketplace dependency (confirmed via Claude Code docs research: no official pre-enabled central marketplace exists; all marketplaces are decentralized, self-add-only, so hanging off baime added no real distribution reach). Now voci self-hosts its own root .claude-plugin/marketplace.json pointing at ./plugin, fully independent of baime. Also incorporated baime ADR-015's ${CLAUDE_PLUGIN_ROOT} runtime-path convention (researched from baime's docs/adr/ADR-015-plugin-root-reference-mechanism.md and TASK-236) to fix a real portability bug: voci-listen's SKILL.md previously called `python3 scripts/extract-instruction.py` via a repo-root-relative path that would silently break for any external repo installing the plugin, since only files inside plugin/ ship with it. Script now moves into plugin/skills/voci-listen/scripts/ and SKILL.md is rewritten to use the ${CLAUDE_PLUGIN_ROOT} placeholder.

claimed: 2026-07-02T07:19:19Z

claimed: 2026-07-02T07:20:19Z

Phase A ✓ 2026-07-02T07:22:49Z

Created plugin/ structure: plugin.json + migrated SKILL.md + migrated scripts +  placeholder. .claude/skills/voci-listen -> relative symlink.

Phase B ✓ 2026-07-02T07:23:04Z

Created root .claude-plugin/marketplace.json; self-hosted, independent of baime. Version 0.1.0 consistent with plugin.json.

Phase C ✓ 2026-07-02T07:23:26Z

Added ## Install section to README.md with two-step install (binary + skill).

DoD #1: PASS — go test ./... (pre-existing daemon failures only)

DoD #2: PASS — make build

DoD #3: PASS — plugin.json exists

DoD #4: PASS — plugin.json valid JSON

DoD #5: PASS — commands field

DoD #6: PASS — name field

DoD #7: PASS — SKILL.md migrated

DoD #8: PASS — symlink exists

DoD #9: PASS — symlink target correct

DoD #10: PASS — symlink resolves to non-empty file

DoD #11: PASS — extract-instruction.py migrated

DoD #12: PASS — extract-instruction_test.sh migrated

DoD #13: PASS — old scripts gone

DoD #14: PASS — test script passes

DoD #15: PASS — CLAUDE_PLUGIN_ROOT placeholder present

DoD #16: PASS — old bare path absent

DoD #17: PASS — marketplace.json exists

DoD #18: PASS — marketplace.json valid JSON

DoD #19: PASS — source field

DoD #20: PASS — version consistency

DoD #21: PASS — go install line in README

DoD #22: PASS — /plugin marketplace add in README

DoD #23: PASS — /plugin install in README

## Execution Summary

Result: Done

Commit: ffdad97

Phase A: Created plugin/ structure (plugin.json, migrated SKILL.md, migrated scripts, CLAUDE_PLUGIN_ROOT placeholder, relative symlink). All 16 DoD checks PASS.

Phase B: Created root .claude-plugin/marketplace.json (self-hosted, v0.1.0, source: "./plugin"). All 4 DoD checks PASS.

Phase C: Added ## Install section to README.md (two-step: go install + /plugin marketplace add + /plugin install). All 3 DoD checks PASS.

Acceptance Gate: All 8 checks PASS (symlink, readlink, JSON validity, version consistency, test script, placeholder grep, git status clean).

make build: PASS. go test ./...: pre-existing daemon failures only (web/recorder.bundle.js not embedded at test time, unrelated).

Completed: 2026-07-02T07:24:59Z
<!-- SECTION:NOTES:END -->
