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

### C.1 二开层 Commit 主题结构（6 层）

`main` 相对 `upstream/main` 的二开历史应维持 **6 个主题 commit**（详见 [docs/commit-structure.md](docs/commit-structure.md)）：

| 顺序 | Message 前缀 | 范围 |
|------|----------------|------|
| 1 | `feat(i18n): add locale infrastructure and zh-CN catalog` | i18n 核心、settings、`zh-CN.ts`、测试骨架 |
| 2 | `feat(i18n): localize electron main process` | `emain/`、`electron-ui.ts` |
| 3 | `feat(i18n): localize frontend app shell and core views` | app shell、tabs、blocks、主要 view（不含 AI/onboarding） |
| 4 | `feat(i18n): localize AI, onboarding, and system presets` | `aipanel/`、`onboarding/`、`display-names.ts`、waveai |
| 5 | `docs: fork attribution, NOTICE, and maintenance guide` | `README`、`NOTICE`、`AGENTS.md` |
| 6 | `chore(ci): add zh-CN release workflow` | `.github/workflows/release-zh.yml` |

整理或跟进官方版本后，用 `git rev-list --count upstream/main..HEAD` 确认为 **6**；若历史被拆散，按 `docs/commit-structure.md` 的方案 B 重新整理。

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
# rebase 改写了历史，需 force-with-lease（见 J. 分支保护）
git push --force-with-lease origin main
# 同步 zh-cn（无 rebase 时可用普通 push；与 main 同步时同上）
git push --force-with-lease origin main:zh-cn
```

**注意：** 不要把 `upstream/main` 直接改成汉化内容；汉化 commit 应 rebase 到新官方基线上。

**日常汉化提交（无 rebase）** 可直接：

```bash
git push origin main
git push origin main:zh-cn
```

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
- 构建产物（`dist/`、`dist-dev/`、`out/`、`bin/`、`make/` 等）
- `node_modules/`
- 临时文件、`test-results.xml`
- 私钥、token、本地路径等敏感信息

## I. 发布与打包

### 策略

- **仅手动发布**：普通 commit 推送到 `main` / `zh-cn` **不会**触发构建。
- 发布由 GitHub Actions workflow **手动触发**（`workflow_dispatch`），在汉化改动积累并验证后再打包。
- 跨平台产物（macOS / Windows / Linux）依赖 CI 矩阵；本地环境通常无法一次打全平台包。

### 发布流程

1. 确保 `main`（及 `zh-cn`）已包含待发布的汉化 commit，且验证通过（见 **F. 验证要求**）。
2. 打开 GitHub → **Actions** → **Release zh-CN Build** → **Run workflow**。
3. 填写参数：
   - **tag**：版本标签，格式 `v{上游版本}-zh.{序号}`，例如 `v0.14.5-zh.2`（序号在同类版本上递增）。
   - **publish**：`true` 直接发布；`false` 先创建 **Draft** Release，检查产物后再手动发布。
4. Workflow 从当前默认分支（`main`）检出代码，在 4 个 runner 上并行构建并上传至 GitHub Releases。
5. 用户在 [Releases](https://github.com/Bianshumeng/waveterm-zh/releases) 页面下载对应平台安装包。

Workflow 文件：`.github/workflows/release-zh.yml`。

### 产物格式

与上游 `electron-builder` 配置一致（输出目录 `make/`）：

| 平台 | 格式 |
|------|------|
| macOS | DMG、ZIP（arm64、x64） |
| Windows | MSI、NSIS 安装包、ZIP |
| Linux | DEB、RPM、AppImage、Pacman、ZIP（amd64 / arm64 按 runner 区分） |

CI **不构建 Snap**：Snapcraft 9 与 electron-builder 不兼容（`snap` 命令已改名为 `pack`）。Deb/AppImage 等已覆盖主流 Linux 安装方式。

### 签名与分发说明

- 构建为**未签名**版本（`CSC_IDENTITY_AUTO_DISCOVERY=false`），无需上游代码签名密钥。
- macOS 首次打开可能需右键「打开」或系统安全设置放行；Windows 可能触发 SmartScreen 提示。
- 公开发布仓库在标准 GitHub-hosted runner 上运行 Actions **免费**（公开 repo 额度内）。

### 本地构建（可选）

完整打包需 `task`、`go`、`zig` 等工具链，见上游 `BUILD.md`。CI 为推荐的发布路径；本地可仅做 `npm run build:dev` 等开发验证。

### 发布记录（截至文档更新时）

| 项目 | 状态 |
|------|------|
| 远程 tag `v0.14.5-zh.1` | 存在；由旧 workflow（tag 自动触发）启动过一次构建，**不代表当前推荐发布方式** |
| 正式 Release 产物 | 以 [Releases](https://github.com/Bianshumeng/waveterm-zh/releases) 页面为准；后续请用手动 workflow 发布 `v0.14.5-zh.2` 及以后版本 |
| 当前 `package.json` 版本 | `0.14.5`（与上游基线一致；汉化版序号体现在 tag 的 `-zh.{n}` 后缀） |

## J. 仓库现状与 GitHub 设置

### 维护模式

- **单人维护**，规则从宽：不强制 PR、不强制 CI 绿灯、owner 可绕过限制。
- 目标：防误删主分支，同时保留 rebase 后 force push 能力。

### 分支保护（Branch protection）

已保护分支：`main`、`zh-cn`（规则相同）。

设置入口：[Settings → Branches](https://github.com/Bianshumeng/waveterm-zh/settings/branches)

| 选项 | 当前值 | 对你意味着什么 |
|------|--------|----------------|
| **Allow deletions**（允许删除分支） | 关闭 | 不能在 GitHub 上误删 `main` / `zh-cn` |
| **Allow force pushes**（允许 force push） | 开启 | rebase 后可用 `--force-with-lease` 推送，**不会被这条规则挡住** |
| **Require a pull request before merging** | 关闭 | 可直接 push 到 `main`，**不必开 PR** |
| **Require status checks to pass** | 关闭 | push 不依赖 CI 绿灯，**不会被未跑的 Actions 挡住** |
| **Include administrators** / **Do not allow bypassing** | 关闭 | owner 操作留有余地 |
| **Require signed commits** | 关闭 | 普通 commit 即可 |
| **Require linear history** | 关闭 | 不强制线性历史策略 |
| **Lock branch** | 关闭 | 分支可正常读写，未冻结 |

### 提交代码时怎么做

**普通汉化 commit（最常见）：**

```bash
git add <files>
git commit -m "feat(i18n): ..."
git push origin main
git push origin main:zh-cn    # 保持两分支同步
```

**跟进官方 rebase 之后：**

```bash
git rebase upstream/main
# ... 解决冲突、验证 ...
git push --force-with-lease origin main
git push --force-with-lease origin main:zh-cn
```

优先用 `--force-with-lease` 而不是 `--force`：若远程有你不知道的新提交，会拒绝推送，避免覆盖他人改动（将来若多人协作更安全）。

### 什么操作会被挡住

| 操作 | 结果 |
|------|------|
| 在 GitHub 删除 `main` / `zh-cn` | 被拒绝（已禁止删除） |
| 普通 `git push` 写汉化 commit | 允许 |
| rebase 后 `git push --force-with-lease` | 允许 |
| 未跑 CI 就 push | 允许（未要求 status checks） |
| 不开 PR 直接 push | 允许 |

### 什么不会自动发生

| 事件 | 是否触发 |
|------|----------|
| push 到 `main` / `zh-cn` | 不触发 Release 打包 |
| push tag `v*-zh*` | **已不再**自动触发（已改为仅手动 workflow） |
| 手动 Run **Release zh-CN Build** | 触发跨平台打包 |

### 发布失败后重跑

- 修复 workflow 并 push 后，需在 Actions **重新手动 Run workflow**（不能用旧 run 续跑）。
- `create-release` 会在**至少有一个平台构建成功**时执行，并上传已成功的产物（某平台失败不阻断其余平台发布）。
- 产物从各 runner 子目录汇总到 `release-files/` 再上传（避免 `make/*.zip` 匹配不到嵌套路径）。
- 建议使用新 tag（如 `v0.14.5-zh.2`）；若复用已存在 tag，需先在 GitHub 删除旧 Release/tag。

### 修改分支保护时请注意

若将来收紧规则，优先评估对 **rebase + force push** 的影响：

- 关闭 **Allow force pushes** → rebase 后无法正常推送到 `origin`
- 开启 **Require status checks** → 需先配置「每次 push 必跑」的轻量 CI，否则会 push 失败
- 开启 **Require pull request** → 日常汉化不能直接 push，需改走 PR 流程

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