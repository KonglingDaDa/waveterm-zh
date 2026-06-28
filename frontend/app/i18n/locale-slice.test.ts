// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { getThinkingMessage } from "@/app/aipanel/aimessage";
import { formatFileSizeError } from "@/app/aipanel/ai-utils";
import { getModeDisplayDescription, getModeDisplayName } from "@/app/aipanel/ai-utils";
import {
    formatPreviewConnectionError,
    makeBookmarkMenuLabel,
} from "@/app/view/preview/preview-ui";
import {
    getProcessColumns,
    makeProcessSignalStatusMessage,
    makeRefreshIntervalMenuItems,
} from "@/app/view/processviewer/processviewer-ui";
import { formatConfigErrorLine, makeConfigFiles } from "@/app/view/waveconfig/waveconfig-files";
import { blockViewToName } from "@/app/block/blockutil";
import { globalStore } from "@/app/store/jotaiStore";
import { buildTabBarContextMenu } from "@/app/tab/tabcontextmenu";
import { atom } from "jotai";
import { describe, expect, it } from "vitest";
import {
    makeCloseTabDialog,
    makeDeleteWorkspaceDialog,
    makeQuitConfirmDialog,
    menuLabel,
} from "./electron-ui";
import { getLocaleFromFullConfig, getMainProcessLocale, setMainProcessLocale } from "./main";
import { t } from "./index";

describe("locale slice", () => {
    it("blockViewToName returns locale-specific labels", () => {
        expect(blockViewToName("term", "en-US")).toBe("Terminal");
        expect(blockViewToName("term", "zh-CN")).toBe("终端");
        expect(blockViewToName("preview", "zh-CN")).toBe("预览");
    });

    it("view name atom pattern refreshes when localeAtom changes", () => {
        const localeAtom = atom<"en-US" | "zh-CN">("en-US");
        const viewNameAtom = atom((get) => t("Terminal", undefined, get(localeAtom)));
        expect(globalStore.get(viewNameAtom)).toBe("Terminal");
        globalStore.set(localeAtom, "zh-CN");
        expect(globalStore.get(viewNameAtom)).toBe("终端");
    });

    it("buildTabBarContextMenu uses the passed locale for labels", () => {
        const settingsAtom = atom<SettingsType>({ "app:tabbar": "top" });
        const fullConfigAtom = atom<FullConfigType>(null);
        const env = {
            getSettingsKeyAtom: (key: keyof SettingsType) =>
                atom((get) => get(settingsAtom)?.[key] ?? null) as ReturnType<typeof atom>,
            atoms: { fullConfigAtom },
            rpc: {} as TabEnv["rpc"],
        } as TabEnv;

        const enMenu = buildTabBarContextMenu(env, "en-US");
        const zhMenu = buildTabBarContextMenu(env, "zh-CN");

        expect(enMenu[0].label).toBe("Tab Bar Position");
        expect(zhMenu[0].label).toBe("标签栏位置");
        expect(enMenu[0].submenu?.[0].label).toBe("Top");
        expect(zhMenu[0].submenu?.[0].label).toBe("顶部");
    });

    it("term menu label keys resolve in zh-CN", () => {
        expect(t("Send to Wave AI", undefined, "zh-CN")).toBe("发送到 Wave AI");
        expect(t("Allow Bracketed Paste Mode", undefined, "zh-CN")).toBe("允许括号粘贴模式");
        expect(t("WebGL not supported", undefined, "zh-CN")).toBe("不支持 WebGL");
    });

    it("formatFileSizeError uses locale-aware templates with correct placeholder order", () => {
        const error = {
            fileName: "big.png",
            fileSize: 11 * 1024 * 1024,
            maxSize: 10 * 1024 * 1024,
            fileType: "image" as const,
        };
        expect(formatFileSizeError(error, "en-US")).toContain('Image "big.png" is too large');
        expect(formatFileSizeError(error, "zh-CN")).toBe('图片 "big.png" 过大（11 MB）。最大允许大小为 10 MB。');
    });

    it("AI panel placeholder keys differ by locale", () => {
        expect(t("Ask Wave AI anything...", undefined, "en-US")).toBe("Ask Wave AI anything...");
        expect(t("Ask Wave AI anything...", undefined, "zh-CN")).toBe("向 Wave AI 提问...");
        expect(t("Continue...", undefined, "zh-CN")).toBe("继续输入...");
    });

    it("AI context menu label keys resolve in zh-CN", () => {
        expect(t("New Chat", undefined, "zh-CN")).toBe("新建对话");
        expect(t("Max Output Tokens", undefined, "zh-CN")).toBe("最大输出 Token 数");
        expect(t("Configure Modes", undefined, "zh-CN")).toBe("配置模式");
        expect(t("Hide Wave AI", undefined, "zh-CN")).toBe("隐藏 Wave AI");
    });

    it("makeConfigFiles keeps paths but translates labels by locale", () => {
        const enFiles = makeConfigFiles(false, "en-US", undefined);
        const zhFiles = makeConfigFiles(false, "zh-CN", undefined);
        expect(enFiles.map((f) => f.path)).toEqual(zhFiles.map((f) => f.path));
        expect(enFiles[0].path).toBe("settings.json");
        expect(enFiles[0].name).toBe("General");
        expect(zhFiles[0].name).toBe("通用");
        expect(zhFiles[2].name).toBe("侧边栏小组件");
        expect(zhFiles[5].name).toBe("密钥");
        expect(zhFiles[1].description).toBe("SSH 主机");
    });

    it("wave config secret validation messages resolve in zh-CN", () => {
        expect(t("Secret name cannot be empty", undefined, "zh-CN")).toBe("密钥名称不能为空");
        expect(
            t(
                "Invalid secret name: must start with a letter and contain only letters, numbers, and underscores",
                undefined,
                "zh-CN"
            )
        ).toBe("无效的密钥名称：必须以字母开头，且只能包含字母、数字和下划线");
        expect(t('Secret "{name}" already exists', { name: "MY_KEY" }, "zh-CN")).toBe('密钥“MY_KEY”已存在');
    });

    it("wave config viewName atom pattern refreshes when localeAtom changes", () => {
        const localeAtom = atom<"en-US" | "zh-CN">("en-US");
        const viewNameAtom = atom((get) => t("Wave Config", undefined, get(localeAtom)));
        expect(globalStore.get(viewNameAtom)).toBe("Wave Config");
        globalStore.set(localeAtom, "zh-CN");
        expect(globalStore.get(viewNameAtom)).toBe("Wave 配置");
    });

    it("electron menu labels differ by locale", () => {
        expect(menuLabel("Create Workspace", "en-US")).toBe("Create Workspace");
        expect(menuLabel("Create Workspace", "zh-CN")).toBe("创建工作区");
        expect(menuLabel("Check for Updates", "zh-CN")).toBe("检查更新");
        expect(menuLabel("Toggle Widgets Bar", "zh-CN")).toBe("显示/隐藏小组件栏");
    });

    it("electron dialog builders differ by locale", () => {
        const enQuit = makeQuitConfirmDialog("en-US");
        const zhQuit = makeQuitConfirmDialog("zh-CN");
        expect(enQuit.title).toBe("Confirm Quit");
        expect(zhQuit.title).toBe("确认退出");
        expect(zhQuit.buttons).toEqual(["取消", "退出"]);

        const zhCloseTab = makeCloseTabDialog("zh-CN");
        expect(zhCloseTab.message).toBe("确定要关闭此标签页吗？");
        expect(zhCloseTab.buttons).toEqual(["取消", "关闭标签"]);

        const zhDeleteWs = makeDeleteWorkspaceDialog("zh-CN");
        expect(zhDeleteWs.buttons).toEqual(["取消", "删除工作区"]);
    });

    it("formatConfigErrorLine wraps errors in zh-CN but preserves file and err", () => {
        const cerr = { file: "settings.json", err: "invalid character '}' looking for beginning of object key string" };
        expect(formatConfigErrorLine(cerr, "en-US")).toBe(
            "Configuration error in settings.json: invalid character '}' looking for beginning of object key string"
        );
        expect(formatConfigErrorLine(cerr, "zh-CN")).toBe(
            "配置文件 settings.json 中存在错误：invalid character '}' looking for beginning of object key string"
        );
    });

    it("save session modal message keys differ by locale", () => {
        expect(t("No scrollback content to save.", undefined, "zh-CN")).toBe("没有可保存的会话回滚内容。");
        expect(t("Failed to save session scrollback: {error}", { error: "disk full" }, "zh-CN")).toBe(
            "保存会话回滚内容失败：disk full"
        );
        expect(t("Ok", undefined, "zh-CN")).toBe("确定");
    });

    it("builder built-in mode display names translate with user custom fallback", () => {
        const builderDefault = {
            "display:name": "Builder Default",
            "display:description": "Good mix of speed and accuracy\n(gpt-5.4 with minimal thinking)",
            "ai:provider": "wave",
        } as AIModeConfigType;
        const userCustom = {
            "display:name": "My Custom Mode",
            "ai:provider": "openai",
            "ai:model": "gpt-4",
        } as AIModeConfigType;

        expect(getModeDisplayName(builderDefault, { modeId: "waveaibuilder@default", locale: "zh-CN" })).toBe(
            "Builder 默认"
        );
        expect(getModeDisplayDescription(builderDefault, { modeId: "waveaibuilder@default", locale: "zh-CN" })).toContain(
            "gpt-5.4"
        );
        expect(getModeDisplayName(userCustom, { modeId: "ai@custom", locale: "zh-CN" })).toBe("My Custom Mode");
    });

    it("preview helpers translate labels but keep paths and error detail", () => {
        expect(makeBookmarkMenuLabel("Home", "~/Desktop", "en-US")).toBe("Go to Home (~/Desktop)");
        expect(makeBookmarkMenuLabel("Home", "~/Desktop", "zh-CN")).toBe("转到 主目录（~/Desktop）");
        expect(formatPreviewConnectionError("connection refused", "zh-CN")).toBe("连接错误：connection refused");
    });

    it("process viewer column and menu helpers differ by locale", () => {
        const enCols = getProcessColumns("linux", "en-US");
        const zhCols = getProcessColumns("linux", "zh-CN");
        expect(enCols.find((c) => c.key === "command")?.label).toBe("Command");
        expect(zhCols.find((c) => c.key === "command")?.label).toBe("命令");
        expect(enCols.find((c) => c.key === "pid")?.label).toBe("PID");

        const zhMenu = makeRefreshIntervalMenuItems(1000, () => {}, "zh-CN");
        expect(zhMenu[0].label).toBe("刷新间隔");
        expect(zhMenu[0].submenu?.[0].label).toBe("1 秒");

        expect(makeProcessSignalStatusMessage(42, "SIGTERM", true, "zh-CN")).toBe("进程 #42 已结束");
        expect(makeProcessSignalStatusMessage(42, "SIGTERM", false, "en-US")).toBe("Process #42 sent SIGTERM");
    });

    it("launcher instruction keys differ by locale", () => {
        expect(t("Type to Filter", undefined, "zh-CN")).toBe("输入以筛选");
        expect(t('Searching "{search}"', { search: "term" }, "zh-CN")).toBe('正在搜索“term”');
        expect(t("No widgets found. Press Escape to clear search.", undefined, "zh-CN")).toBe(
            "未找到小组件。按 Escape 清除搜索。"
        );
    });

    it("main-safe locale helpers fall back to en-US without config", () => {
        setMainProcessLocale("en-US");
        expect(getLocaleFromFullConfig(null)).toBe("en-US");
        expect(getLocaleFromFullConfig(undefined)).toBe("en-US");
        expect(getLocaleFromFullConfig({ settings: {} } as FullConfigType)).toBe("en-US");
        expect(getLocaleFromFullConfig({ settings: { "app:locale": "zh-CN" } } as FullConfigType)).toBe("zh-CN");
        expect(getMainProcessLocale()).toBe("en-US");
    });

    it("getThinkingMessage returns locale-specific status text", () => {
        const parts = [
            {
                type: "data-tooluse" as const,
                data: { approval: "needs-approval", toolcallid: "1" },
            },
        ];
        expect(getThinkingMessage(parts, true, "assistant", "en-US")?.message).toBe("Waiting for Tool Approvals...");
        expect(getThinkingMessage(parts, true, "assistant", "zh-CN")?.message).toBe("等待工具审批...");
    });
});