// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { t, type Locale } from "@/app/i18n";

export type ProcessSortCol = "pid" | "command" | "user" | "cpu" | "mem" | "status" | "threads";

export type ProcessColDef = {
    key: ProcessSortCol;
    label: string;
    tooltip?: string;
    width: string;
    align?: "right";
    hideOnPlatform?: string[];
};

const ColumnDefs: ProcessColDef[] = [
    { key: "pid", label: "PID", width: "70px", align: "right" },
    { key: "command", label: "Command", width: "minmax(120px, 4fr)" },
    { key: "status", label: "Status", width: "75px", hideOnPlatform: ["windows", "darwin"] },
    { key: "user", label: "User", width: "80px", hideOnPlatform: ["windows"] },
    { key: "threads", label: "NT", tooltip: "Num Threads", width: "40px", align: "right", hideOnPlatform: ["windows"] },
    { key: "cpu", label: "CPU%", width: "70px", align: "right" },
    { key: "mem", label: "Memory", width: "90px", align: "right" },
];

export function getProcessColumns(platform: string, locale: Locale): ProcessColDef[] {
    return ColumnDefs.filter((c) => !c.hideOnPlatform?.includes(platform)).map((col) => ({
        ...col,
        label: col.key === "pid" || col.key === "cpu" ? col.label : t(col.label, undefined, locale),
        tooltip: col.tooltip ? t(col.tooltip, undefined, locale) : undefined,
    }));
}

export function getProcessGridTemplate(platform: string): string {
    return ColumnDefs.filter((c) => !c.hideOnPlatform?.includes(platform))
        .map((c) => c.width)
        .join(" ");
}

export function makeProcessSignalStatusMessage(
    pid: number,
    signal: string,
    killLabel: boolean | undefined,
    locale: Locale
): string {
    const label = killLabel ? t("Killed", undefined, locale) : t("sent {signal}", { signal }, locale);
    return t("Process #{pid} {label}", { pid, label }, locale);
}

export function makeProcessActionErrorMessage(message: string, locale: Locale): string {
    return t("Error: {message}", { message }, locale);
}

export function makeRefreshIntervalMenuItems(
    currentInterval: number,
    setFetchInterval: (ms: number) => void,
    locale: Locale
): ContextMenuItem[] {
    const tl = (key: string) => t(key, undefined, locale);
    return [
        {
            label: tl("Refresh Interval"),
            type: "submenu",
            submenu: [
                {
                    label: tl("1 second"),
                    type: "checkbox",
                    checked: currentInterval === 1000,
                    click: () => setFetchInterval(1000),
                },
                {
                    label: tl("2 seconds"),
                    type: "checkbox",
                    checked: currentInterval === 2000,
                    click: () => setFetchInterval(2000),
                },
                {
                    label: tl("5 seconds"),
                    type: "checkbox",
                    checked: currentInterval === 5000,
                    click: () => setFetchInterval(5000),
                },
            ],
        },
    ];
}

export function makeProcessContextMenuItems(
    pid: number,
    isWindows: boolean,
    settingsMenuItems: ContextMenuItem[],
    handlers: {
        sendSignal: (pid: number, signal: string, killLabel?: boolean) => void;
    },
    locale: Locale
): ContextMenuItem[] {
    const tl = (key: string) => t(key, undefined, locale);
    const menu: ContextMenuItem[] = [
        {
            label: tl("Copy PID"),
            click: () => navigator.clipboard.writeText(String(pid)),
        },
        { type: "separator" },
    ];

    if (!isWindows) {
        menu.push({
            label: tl("Signal"),
            type: "submenu",
            submenu: [
                { label: "SIGTERM", click: () => handlers.sendSignal(pid, "SIGTERM") },
                { label: "SIGINT", click: () => handlers.sendSignal(pid, "SIGINT") },
                { label: "SIGHUP", click: () => handlers.sendSignal(pid, "SIGHUP") },
                { label: "SIGKILL", click: () => handlers.sendSignal(pid, "SIGKILL") },
                { label: "SIGUSR1", click: () => handlers.sendSignal(pid, "SIGUSR1") },
                { label: "SIGUSR2", click: () => handlers.sendSignal(pid, "SIGUSR2") },
            ],
        });
        menu.push({ type: "separator" });
        menu.push({
            label: tl("Kill Process"),
            click: () => handlers.sendSignal(pid, "SIGTERM", true),
        });
    }

    menu.push({ type: "separator" });
    menu.push(...settingsMenuItems);
    return menu;
}

export function makeCpuCoreTooltip(numcpu: number, locale: Locale): string {
    const cores = numcpu === 1 ? t("core", undefined, locale) : t("cores", undefined, locale);
    return t("100% per core · {numcpu} {cores} = {max}% max", { numcpu, cores, max: numcpu * 100 }, locale);
}