// Copyright 2025, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { WaveStreamdown } from "@/app/element/streamdown";
import { useLocale, useT } from "@/app/i18n/react";
import { memo, useEffect, useRef, useState } from "react";

interface ChatConfig {
    userPrompt: string;
    toolName: string;
    toolDescription: string;
    markdownResponse: string;
}

const chatConfigsEn: ChatConfig[] = [
    {
        userPrompt: "Check out ~/waveterm and summarize the project — what it does and how it's organized.",
        toolName: "read_dir",
        toolDescription: 'reading directory "~/waveterm"',
        markdownResponse: `Here's a quick, file-structure–driven overview of this repo (Wave Terminal):

## What it is
- Electron + React front end with a Go backend ("wavesrv"). Provides a terminal with GUI widgets, previews, web, and AI. (README.md)
- Licensed Apache-2.0. (LICENSE)

## Architecture at a glance
- **Electron main process:** \`emain/*.ts\` configures windows, menus, preload scripts, updater, and ties into the Go backend via local RPC. (\`emain/\`)
- **Renderer UI:** React/TS built with Vite, Tailwind. (\`frontend/\`, \`index.html\`, \`electron.vite.config.ts\`)
- **Go backend ("wavesrv"):** starts services, web and websocket listeners, telemetry loops, config watcher, local RPC, filestore and SQLite-backed object store. (\`cmd/server/main-server.go\`, \`pkg/*\`)
- **CLI/helper ("wsh"):** built for multiple OS/arch; used for shell integration and remote operations. (\`cmd/wsh/\`, \`Taskfile.yml build:wsh\`)

## Key directories
- **cmd/:** entrypoints and generators
  - \`server/\`: wavesrv main
  - \`generategs/\`, \`generatego/\`: TS/Go bindings generation
  - \`wsh/\`: shell helper

- **pkg/:** backend packages
  - \`wcore/\`: startup coordination, initial data, window/workspace creation
  - \`web/\`: HTTP+WS servers, Unix listener
  - \`waveai/\`: AI backends (OpenAI, Anthropic, Google, Perplexity)
  - \`wshrpc\`, \`wshutil\`, \`wslconn\`, \`remote/*\`: local/remote RPC, WSL, SSH, fileshare
  - \`wstore/\`: persistent store (SQLite via sqlx); \`waveobj/\`: object model
  - \`filestore/\`: local file cache/storage
  - \`telemetry/\`: metrics/events, periodic reporting`,
    },
    {
        userPrompt: "can you analyze the log output in my terminal?",
        toolName: "term_get_scrollback",
        toolDescription: "reading terminal output from widget",
        markdownResponse: `Here's what your log shows:

- Normal startup/init at 18:06:51 (router/register, DBGetAllObjsByType, Stat/Read).
- Many FocusWindow events — normal UI activity.
- 18:08:28.558 WebSocket ReadPump error: close 1001 (going away) — usually a tab/app reload, sleep, or brief network hiccup.
- 18:08:29 re-init + new WebSocket connection — successful auto-reconnect.
- SetMetaCommand and periodic filestore flushes with err: — normal; no failures.
- No panics/fatal errors; just the transient disconnect.

If that disconnect was unexpected, check for a tab reload/sleep at 18:08:28, and watch for repeats.

Want deeper triage? Run these and paste results:

\`\`\`bash
# 1) Scan recent logs for problems
grep -Ei 'error|panic|fatal|websocket|1006|1011' \\
  "$HOME/Library/Application Support/waveterm-dev/waveapp.log" | tail -n 200

# 2) Inspect around the disconnect window
awk '($0 ~ /2025-10-10 18:08:2[0-9]/){print}' \\
  "$HOME/Library/Application Support/waveterm-dev/waveapp.log"

# 3) Live follow for recurring drops
tail -f "$HOME/Library/Application Support/waveterm-dev/waveapp.log" \\
  | grep -Ei 'error|panic|fatal|websocket|close'
\`\`\`

Need me to look at a longer slice (e.g., last 1000 lines) or a different time range?`,
    },
];

const chatConfigsZh: ChatConfig[] = [
    {
        userPrompt: "查看 ~/waveterm 并总结这个项目——它做什么、如何组织。",
        toolName: "read_dir",
        toolDescription: '正在读取目录 "~/waveterm"',
        markdownResponse: `根据目录结构，这是 Wave Terminal 的快速概览：

## 项目简介
- Electron + React 前端，Go 后端（wavesrv）。提供带 GUI 小组件、预览、网页和 AI 的终端。（README.md）
- Apache-2.0 许可证。（LICENSE）

## 架构概览
- **Electron 主进程：** \`emain/*.ts\` 配置窗口、菜单、预加载脚本、更新器，并通过本地 RPC 连接 Go 后端。（\`emain/\`）
- **渲染器 UI：** 使用 Vite、Tailwind 构建的 React/TS。（\`frontend/\`、\`index.html\`、\`electron.vite.config.ts\`）
- **Go 后端（wavesrv）：** 启动服务、Web/WebSocket 监听、遥测循环、配置监视、本地 RPC、filestore 和 SQLite 对象存储。（\`cmd/server/main-server.go\`、\`pkg/*\`）
- **CLI 助手（wsh）：** 多平台构建，用于 shell 集成和远程操作。（\`cmd/wsh/\`、\`Taskfile.yml build:wsh\`）

## 主要目录
- **cmd/：** 入口和代码生成
- **pkg/：** 后端包（wcore、web、waveai、wshrpc、wstore、filestore、telemetry 等）`,
    },
    {
        userPrompt: "能分析一下我终端里的日志输出吗？",
        toolName: "term_get_scrollback",
        toolDescription: "正在从小组件读取终端输出",
        markdownResponse: `日志分析如下：

- 18:06:51 正常启动/初始化（router/register、DBGetAllObjsByType、Stat/Read）。
- 多次 FocusWindow 事件——正常的 UI 活动。
- 18:08:28.558 WebSocket ReadPump 错误：close 1001 (going away)——通常是标签页重载、休眠或短暂网络波动。
- 18:08:29 重新初始化并建立新 WebSocket 连接——自动重连成功。
- SetMetaCommand 和周期性 filestore 刷新，err: 为空——正常，无失败。
- 无 panic/fatal 错误；只是短暂断开。

若该断开不符合预期，请检查 18:08:28 附近是否有标签页重载或休眠，并留意是否重复发生。

需要更深入排查？可运行以下命令并粘贴结果：

\`\`\`bash
grep -Ei 'error|panic|fatal|websocket|1006|1011' \\
  "$HOME/Library/Application Support/waveterm-dev/waveapp.log" | tail -n 200
\`\`\`

需要我查看更长的日志片段（例如最近 1000 行）或其他时间段吗？`,
    },
];

const AIThinking = memo(() => {
    const tt = useT();
    return (
        <div className="flex items-center gap-2">
            <div className="animate-pulse flex items-center">
                <i className="fa fa-circle text-[10px]"></i>
                <i className="fa fa-circle text-[10px] mx-1"></i>
                <i className="fa fa-circle text-[10px]"></i>
            </div>
            <span className="text-sm text-gray-400">{tt("AI is thinking...")}</span>
        </div>
    );
});

AIThinking.displayName = "AIThinking";

const FakeToolCall = memo(({ toolName, toolDescription }: { toolName: string; toolDescription: string }) => {
    return (
        <div className="flex items-start gap-1 p-2 rounded bg-zinc-800 border border-gray-700 text-success">
            <span className="font-bold">✓</span>
            <div className="flex-1">
                <div className="font-semibold">{toolName}</div>
                <div className="text-sm text-gray-400">{toolDescription}</div>
            </div>
        </div>
    );
});

FakeToolCall.displayName = "FakeToolCall";

const FakeUserMessage = memo(({ userPrompt }: { userPrompt: string }) => {
    return (
        <div className="flex justify-end">
            <div className="px-2 py-2 rounded-lg bg-zinc-700 text-white max-w-[calc(100%-20px)]">
                <div className="whitespace-pre-wrap break-words">{userPrompt}</div>
            </div>
        </div>
    );
});

FakeUserMessage.displayName = "FakeUserMessage";

const FakeAssistantMessage = memo(({ config, onComplete }: { config: ChatConfig; onComplete?: () => void }) => {
    const [phase, setPhase] = useState<"thinking" | "tool" | "streaming">("thinking");
    const [streamedText, setStreamedText] = useState("");

    useEffect(() => {
        const timeouts: NodeJS.Timeout[] = [];
        let streamInterval: NodeJS.Timeout | null = null;

        const runAnimation = () => {
            setPhase("thinking");
            setStreamedText("");

            timeouts.push(
                setTimeout(() => {
                    setPhase("tool");
                }, 2000)
            );

            timeouts.push(
                setTimeout(() => {
                    setPhase("streaming");
                }, 4000)
            );

            timeouts.push(
                setTimeout(() => {
                    let currentIndex = 0;
                    streamInterval = setInterval(() => {
                        if (currentIndex >= config.markdownResponse.length) {
                            if (streamInterval) {
                                clearInterval(streamInterval);
                                streamInterval = null;
                            }
                            if (onComplete) {
                                onComplete();
                            }
                            return;
                        }
                        currentIndex += 10;
                        setStreamedText(config.markdownResponse.slice(0, currentIndex));
                    }, 100);
                }, 4000)
            );
        };

        runAnimation();

        return () => {
            timeouts.forEach(clearTimeout);
            if (streamInterval) {
                clearInterval(streamInterval);
            }
        };
    }, [config.markdownResponse, onComplete]);

    return (
        <div className="flex justify-start">
            <div className="px-2 py-2 rounded-lg">
                {phase === "thinking" && <AIThinking />}
                {phase === "tool" && (
                    <>
                        <div className="mb-2">
                            <FakeToolCall toolName={config.toolName} toolDescription={config.toolDescription} />
                        </div>
                        <AIThinking />
                    </>
                )}
                {phase === "streaming" && (
                    <>
                        <div className="mb-2">
                            <FakeToolCall toolName={config.toolName} toolDescription={config.toolDescription} />
                        </div>
                        <WaveStreamdown text={streamedText} parseIncompleteMarkdown={true} className="text-gray-100" />
                    </>
                )}
            </div>
        </div>
    );
});

FakeAssistantMessage.displayName = "FakeAssistantMessage";

const FakeAIPanelHeader = memo(() => {
    const tt = useT();
    return (
        <div className="py-2 pl-3 pr-1 border-b border-gray-600 flex items-center justify-between min-w-0 bg-zinc-900">
            <h2 className="text-white text-sm font-semibold flex items-center gap-2 flex-shrink-0 whitespace-nowrap">
                <i className="fa fa-sparkles text-accent"></i>
                Wave AI
            </h2>

            <div className="flex items-center flex-shrink-0 whitespace-nowrap">
                <div className="flex items-center text-sm whitespace-nowrap">
                    <span className="text-gray-300 mr-1 text-[12px]">{tt("Context")}</span>
                    <button
                        className="relative inline-flex h-6 w-14 items-center rounded-full transition-colors bg-accent-600"
                        title={tt("Widget Access {state}", { state: tt("ON") })}
                    >
                        <span className="absolute inline-block h-4 w-4 transform rounded-full bg-white transition-transform translate-x-8" />
                        <span className="relative z-10 text-xs text-white transition-all ml-2.5 mr-6 text-left font-bold">
                            {tt("ON")}
                        </span>
                    </button>
                </div>

                <button
                    className="text-gray-400 transition-colors p-1 rounded flex-shrink-0 ml-2 focus:outline-none"
                    title={tt("More options")}
                >
                    <i className="fa fa-ellipsis-vertical"></i>
                </button>
            </div>
        </div>
    );
});

FakeAIPanelHeader.displayName = "FakeAIPanelHeader";

export const FakeChat = memo(() => {
    const locale = useLocale();
    const scrollRef = useRef<HTMLDivElement>(null);
    const [chatIndex, setChatIndex] = useState(1);
    const chatConfigs = locale === "zh-CN" ? chatConfigsZh : chatConfigsEn;
    const config = chatConfigs[chatIndex] || chatConfigs[0];

    useEffect(() => {
        const interval = setInterval(() => {
            if (scrollRef.current) {
                scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
            }
        }, 1000);

        return () => clearInterval(interval);
    }, []);

    const handleComplete = () => {
        setTimeout(() => {
            setChatIndex((prev) => (prev + 1) % chatConfigs.length);
        }, 2000);
    };

    return (
        <div className="flex flex-col w-full h-full">
            <FakeAIPanelHeader />
            <div className="flex-1 overflow-hidden">
                <div ref={scrollRef} className="flex flex-col gap-1 p-2 h-full overflow-y-auto bg-zinc-900">
                    <FakeUserMessage userPrompt={config.userPrompt} />
                    <FakeAssistantMessage config={config} onComplete={handleComplete} />
                </div>
            </div>
        </div>
    );
});

FakeChat.displayName = "FakeChat";