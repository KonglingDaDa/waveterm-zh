# 二开 Commit 主题结构

本文档定义 `waveterm-zh` 相对 `upstream/main` 的二开层应采用的 **6 个主题 commit** 结构，便于 `git rebase upstream/main` 跟进官方新版本。

## 目标结构

```text
upstream/main（官方基线，只读）
  ├─ 1. feat(i18n): add locale infrastructure and zh-CN catalog
  ├─ 2. feat(i18n): localize electron main process
  ├─ 3. feat(i18n): localize frontend app shell and core views
  ├─ 4. feat(i18n): localize AI, onboarding, and system presets
  ├─ 5. docs: fork attribution, NOTICE, and maintenance guide
  └─ 6. chore(ci): add zh-CN release workflow
```

## 各 Commit 文件范围

### 1 — 基础设施

- `frontend/app/i18n/index.ts`, `types.ts`, `react.tsx`, `localeutil.ts`, `main.ts`, `en-US.ts`, `zh-CN.ts`, `i18n.test.ts`, `locale-slice.test.ts`
- `schema/settings.json`, `pkg/wconfig/metaconsts.go`, `pkg/wconfig/settingsconfig.go`
- `frontend/types/gotypes.d.ts`, `frontend/types/custom.d.ts`
- `frontend/app/store/global-atoms.ts`, `frontend/preview/mock/mockwaveenv.ts`

### 2 — Electron 主进程

- `emain/*.ts`
- `frontend/app/i18n/electron-ui.ts`

### 3 — 前端壳层与核心视图

- `frontend/app/app.tsx`, `block/`, `element/`, `modals/`, `suggestion/`, `tab/`, `treeview/`, `workspace/`
- `frontend/app/i18n/connection-ui.ts`
- `frontend/app/view/` 下除 `aipanel`、`onboarding`、`waveai`、`aifilediff` 外的视图
- `frontend/preview/previews/onboarding.preview.tsx`

### 4 — AI、引导与系统预设

- `frontend/app/aipanel/`
- `frontend/app/onboarding/`
- `frontend/app/i18n/display-names.ts`
- `frontend/app/view/aifilediff/`, `frontend/app/view/waveai/`

### 5 — 文档与合规

- `README.md`, `NOTICE`, `AGENTS.md`, `CLAUDE.md`, `docs/commit-structure.md`

### 6 — CI 发布

- `.github/workflows/release-zh.yml`

## 整理执行步骤（方案 B：软重置 + 分主题提交）

```bash
git status                                    # 工作区必须干净
git fetch upstream origin
git branch backup/main-before-squash main     # 安全备份

git reset --soft upstream/main                # 全部二开改动进入暂存区
git reset HEAD                                # 取消暂存，保留工作区

# 按上文 1→6 顺序 git add <paths> && git commit -m "..."
# 每步后可用 git status 确认无遗漏

git rev-list --count upstream/main..HEAD      # 应为 6
git diff backup/main-before-squash main       # 代码树应一致（文档可有增量）

npm run test -- --run frontend/app/i18n
npm run build:dev

git push --force-with-lease origin main
git push --force-with-lease origin main:zh-cn
```

## 执行清单（2026-06-28 整理记录）

- [x] 1. 创建备份分支 `backup/main-before-squash`
- [x] 2. 软重置到 `upstream/main` 并取消暂存
- [x] 3. Commit 1：locale infrastructure (`650219b8`)
- [x] 4. Commit 2：electron main process (`878efd21`)
- [x] 5. Commit 3：frontend shell and core views (`46d0d285`)
- [x] 6. Commit 4：AI, onboarding, presets (`7c54963a`)
- [x] 7. Commit 5：docs（`git log upstream/main..HEAD` 第 2 条）
- [x] 8. Commit 6：CI workflow（`git log upstream/main..HEAD` 第 1 条）
- [x] 9. 验证 commit 数量 = 6；非文档文件与备份树一致
- [x] 10. i18n 测试 38/38 通过；build:dev 成功
- [x] 11. force-with-lease 推送到 `origin/main` 与 `origin/zh-cn`

## 当前六层 Commit（整理后）

| # | Hash | Message |
|---|------|---------|
| 1 | `650219b8` | feat(i18n): add locale infrastructure and zh-CN catalog |
| 2 | `878efd21` | feat(i18n): localize electron main process |
| 3 | `46d0d285` | feat(i18n): localize frontend app shell and core views |
| 4 | `7c54963a` | feat(i18n): localize AI, onboarding, and system presets |
| 5 | （见 `git log`） | docs: fork attribution, NOTICE, and maintenance guide |
| 6 | （见 `git log`） | chore(ci): add zh-CN release workflow |

## 日常维护约定

新增汉化改动时，按主题追加**新 commit**（保持 message 前缀与层一致），不要混入无关主题。下次跟官方版本仍使用 `git rebase upstream/main`。