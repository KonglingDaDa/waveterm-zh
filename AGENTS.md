# AGENTS.md - waveterm-zh 本地化维护说明

## A. 项目在做什么

这是 [wavetermdev/waveterm](https://github.com/wavetermdev/waveterm) 的**简体中文本地化维护仓库**（发布于 `Bianshumeng/waveterm-zh`）。

目标：

- 提供简体中文 UI、语言切换（`en-US` / `zh-CN`）、简体中文语言包及相关本地化适配。
- **不做额外功能二开**；除非 PM 明确要求，否则不要引入与汉化无关的新功能。

## B. Remote 约定

```text
upstream: https://github.com/wavetermdev/waveterm.git   # 官方上游，只 fetch，不直接改
origin:   https://github.com/Bianshumeng/waveterm-zh.git # 本仓库公开发布
```

- 官方代码只从 `upstream` 获取。
- 汉化提交推送到 `origin` 的维护分支（`main` / `zh-cn`）。

## C. 分支与 Commit 约定

| 分支 | 用途 |
|------|------|
| `upstream/main` | 官方主线，仅作 rebase 基线 |
| `main` | 当前汉化版本（官方基线 + 汉化 commit） |
| `zh-cn` | 可选，显式标记汉化维护分支 |

规则：

- 汉化内容以 **commit** 形式保存在维护分支上，不要长期留在未提交工作区。
- 每轮改动用清晰 commit 保留，便于跟进官方版本。
- 推荐 commit message 前缀：
  - `feat(i18n): ...`
  - `fix(i18n): ...`
  - `docs: ...`
  - `chore(repo): ...`

## D. 跟进官方更新的标准流程

```bash
# 1. 确保当前汉化分支干净
git status

# 2. 拉取官方更新
git fetch upstream

# 3. 切到汉化维护分支
git switch main   # 或 zh-cn

# 4. 把汉化提交 rebase 到新的官方 main
git rebase upstream/main

# 5. 如果冲突：
#    - 手动解决冲突
#    - 运行相关测试（见下方验证要求）
#    - git add <resolved files>
#    - git rebase --continue
#    - rerere 会记录本次解决方案，下次同类冲突可自动套用

# 6. 验证通过后推送到 origin
git push origin main
# 如维护了 zh-cn 分支：
git push origin zh-cn
```

**注意：** 不要把 `upstream/main` 直接改成汉化内容；汉化 commit 应 rebase 到新官方基线上。

## E. Git rerere

本仓库已开启：

```bash
git config rerere.enabled true
git config rerere.autoupdate true
```

说明：

- rerere 会记录冲突解决方案；后续 rebase 遇到同样冲突时 Git 可自动套用。
- **不要删除** `.git/rr-cache`。
- 自动套用后仍需人工 review；rerere 是辅助，不是免审查。

## F. 验证要求

每次 rebase 或汉化改动后至少运行：

```bash
npm run test -- --run frontend/app/i18n
```

环境允许时再运行：

```bash
npm run build:dev
```

Go/task 可用时补跑：

```bash
task generate
```

## G. 许可证与对外声明

- 上游许可证为 **Apache-2.0**；本仓库不得更换或移除 `LICENSE` / `NOTICE` 中的上游版权声明。
- 本地化属于衍生作品：更新 `NOTICE` 时**追加**维护方信息，不要删除 Command Line Inc. 条目。
- `README.md` 顶部须保留非官方 fork 声明，避免与官方 [waveterm.dev](https://www.waveterm.dev) 混淆。
- 发布二进制时建议在名称中区分（如 `waveterm-zh`），并随附 `LICENSE` 与 `NOTICE`。

## H. 排除提交内容

不要提交：

- `.omx/`
- 本地日志、`*.log`
- 构建产物（`dist/`、`dist-dev/`、`out/`、`bin/` 等）
- `node_modules/`
- 临时文件、`test-results.xml`
- 私钥、token、本地路径等敏感信息

---

<!-- gitnexus:start -->
# GitNexus — Code Intelligence

This project is indexed by GitNexus as **waveterm** (26289 symbols, 59903 relationships, 300 execution flows). Use the GitNexus MCP tools to understand code, assess impact, and navigate safely.

> Index stale? Run `node .gitnexus/run.cjs analyze` from the project root — it auto-selects an available runner. No `.gitnexus/run.cjs` yet? `npx gitnexus analyze` (npm 11 crash → `npm i -g gitnexus`; #1939).

## Always Do

- **MUST run impact analysis before editing any symbol.** Before modifying a function, class, or method, run `gitnexus_impact({target: "symbolName", direction: "upstream"})` and report the blast radius (direct callers, affected processes, risk level) to the user.
- **MUST run `gitnexus_detect_changes()` before committing** to verify your changes only affect expected symbols and execution flows. For regression review, compare against the default branch: `gitnexus_detect_changes({scope: "compare", base_ref: "main"})`.
- **MUST warn the user** if impact analysis returns HIGH or CRITICAL risk before proceeding with edits.
- When exploring unfamiliar code, use `gitnexus_query({query: "concept"})` to find execution flows instead of grepping. It returns process-grouped results ranked by relevance.
- When you need full context on a specific symbol — callers, callees, which execution flows it participates in — use `gitnexus_context({name: "symbolName"})`.

## Never Do

- NEVER edit a function, class, or method without first running `gitnexus_impact` on it.
- NEVER ignore HIGH or CRITICAL risk warnings from impact analysis.
- NEVER rename symbols with find-and-replace — use `gitnexus_rename` which understands the call graph.
- NEVER commit changes without running `gitnexus_detect_changes()` to check affected scope.

## Resources

| Resource | Use for |
|----------|---------|
| `gitnexus://repo/waveterm/context` | Codebase overview, check index freshness |
| `gitnexus://repo/waveterm/clusters` | All functional areas |
| `gitnexus://repo/waveterm/processes` | All execution flows |
| `gitnexus://repo/waveterm/process/{name}` | Step-by-step execution trace |

## CLI

| Task | Read this skill file |
|------|---------------------|
| Understand architecture / "How does X work?" | `.claude/skills/gitnexus/gitnexus-exploring/SKILL.md` |
| Blast radius / "What breaks if I change X?" | `.claude/skills/gitnexus/gitnexus-impact-analysis/SKILL.md` |
| Trace bugs / "Why is X failing?" | `.claude/skills/gitnexus/gitnexus-debugging/SKILL.md` |
| Rename / extract / split / refactor | `.claude/skills/gitnexus/gitnexus-refactoring/SKILL.md` |
| Tools, resources, schema reference | `.claude/skills/gitnexus/gitnexus-guide/SKILL.md` |
| Index, status, clean, wiki CLI commands | `.claude/skills/gitnexus/gitnexus-cli/SKILL.md` |

<!-- gitnexus:end -->