// Copyright 2025, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { menuLabel } from "@/app/i18n/electron-ui";
import { getMainProcessLocale, updateMainProcessLocaleFromFullConfig } from "@/app/i18n/main";
import type { Locale } from "@/app/i18n/types";
import { waveEventSubscribeSingle } from "@/app/store/wps";
import { RpcApi } from "@/app/store/wshclientapi";
import * as electron from "electron";

const ZhForkReleasesUrl = "https://github.com/KonglingDaDa/waveterm-zh/releases";
import { fireAndForget } from "../frontend/util/util";
import { focusedBuilderWindow, getBuilderWindowById } from "./emain-builder";
import { openBuilderWindow } from "./emain-ipc";
import { isDev, unamePlatform } from "./emain-platform";
import { clearTabCache } from "./emain-tabview";
import { decreaseZoomLevel, increaseZoomLevel, resetZoomLevel } from "./emain-util";
import {
    createNewWaveWindow,
    createWorkspace,
    focusedWaveWindow,
    getAllWaveWindows,
    getWaveWindowByWorkspaceId,
    relaunchBrowserWindows,
    WaveBrowserWindow,
} from "./emain-window";
import { ElectronWshClient } from "./emain-wsh";


type AppMenuCallbacks = {
    createNewWaveWindow: () => Promise<void>;
    relaunchBrowserWindows: () => Promise<void>;
};

function getWindowWebContents(window: electron.BaseWindow): electron.WebContents {
    if (window == null) {
        return null;
    }
    // Check BrowserWindow first (for Tsunami Builder windows)
    if (window instanceof electron.BrowserWindow) {
        return window.webContents;
    }
    // Check WaveBrowserWindow (for main Wave windows with tab views)
    if (window instanceof WaveBrowserWindow) {
        if (window.activeTabView) {
            return window.activeTabView.webContents;
        }
        return null;
    }
    return null;
}

async function getWorkspaceMenu(
    locale: Locale,
    ww?: WaveBrowserWindow
): Promise<Electron.MenuItemConstructorOptions[]> {
    const workspaceList = await RpcApi.WorkspaceListCommand(ElectronWshClient);
    const workspaceMenu: Electron.MenuItemConstructorOptions[] = [
        {
            label: menuLabel("Create Workspace", locale),
            click: (_, window) => fireAndForget(() => createWorkspace((window as WaveBrowserWindow) ?? ww)),
        },
    ];
    function getWorkspaceSwitchAccelerator(i: number): string {
        if (i < 9) {
            return unamePlatform == "darwin" ? `Command+Control+${i + 1}` : `Alt+Control+${i + 1}`;
        }
    }
    if (workspaceList?.length) {
        workspaceMenu.push(
            { type: "separator" },
            ...workspaceList.map<Electron.MenuItemConstructorOptions>((workspace, i) => {
                return {
                    label: `${workspace.workspacedata.name}`,
                    click: (_, window) => {
                        ((window as WaveBrowserWindow) ?? ww)?.switchWorkspace(workspace.workspacedata.oid);
                    },
                    accelerator: getWorkspaceSwitchAccelerator(i),
                };
            })
        );
    }
    return workspaceMenu;
}

function makeEditMenu(locale: Locale, fullConfig?: FullConfigType): Electron.MenuItemConstructorOptions[] {
    let pasteAccelerator: string;
    if (unamePlatform === "darwin") {
        pasteAccelerator = "Command+V";
    } else {
        const ctrlVPaste = fullConfig?.settings?.["app:ctrlvpaste"];
        if (ctrlVPaste == null) {
            pasteAccelerator = unamePlatform === "win32" ? "Control+V" : "";
        } else if (ctrlVPaste) {
            pasteAccelerator = "Control+V";
        } else {
            pasteAccelerator = "";
        }
    }
    return [
        {
            role: "undo",
            label: menuLabel("Undo", locale),
            accelerator: unamePlatform === "darwin" ? "Command+Z" : "",
        },
        {
            role: "redo",
            label: menuLabel("Redo", locale),
            accelerator: unamePlatform === "darwin" ? "Command+Shift+Z" : "",
        },
        { type: "separator" },
        {
            role: "cut",
            label: menuLabel("Cut", locale),
            accelerator: unamePlatform === "darwin" ? "Command+X" : "",
        },
        {
            role: "copy",
            label: menuLabel("Copy", locale),
            accelerator: unamePlatform === "darwin" ? "Command+C" : "",
        },
        {
            role: "paste",
            label: menuLabel("Paste", locale),
            accelerator: pasteAccelerator,
        },
        {
            role: "pasteAndMatchStyle",
            label: menuLabel("Paste and Match Style", locale),
            accelerator: unamePlatform === "darwin" ? "Command+Shift+V" : "",
        },
        {
            role: "delete",
            label: menuLabel("Delete", locale),
        },
        {
            role: "selectAll",
            label: menuLabel("Select All", locale),
            accelerator: unamePlatform === "darwin" ? "Command+A" : "",
        },
    ];
}

function makeFileMenu(
    locale: Locale,
    numWaveWindows: number,
    callbacks: AppMenuCallbacks,
    fullConfig: FullConfigType
): Electron.MenuItemConstructorOptions[] {
    const fileMenu: Electron.MenuItemConstructorOptions[] = [
        {
            label: menuLabel("New Window", locale),
            accelerator: "CommandOrControl+Shift+N",
            click: () => fireAndForget(callbacks.createNewWaveWindow),
        },
        {
            role: "close",
            label: menuLabel("Close", locale),
            accelerator: "",
            click: () => {
                focusedWaveWindow?.close();
            },
        },
    ];
    const featureWaveAppBuilder = fullConfig?.settings?.["feature:waveappbuilder"];
    if (isDev || featureWaveAppBuilder) {
        fileMenu.splice(1, 0, {
            label: menuLabel("New WaveApp Builder Window", locale),
            accelerator: unamePlatform === "darwin" ? "Command+Shift+B" : "Alt+Shift+B",
            click: () => openBuilderWindow(""),
        });
    }
    if (numWaveWindows == 0) {
        fileMenu.push({
            label: menuLabel("New Window (hidden-1)", locale),
            accelerator: unamePlatform === "darwin" ? "Command+N" : "Alt+N",
            acceleratorWorksWhenHidden: true,
            visible: false,
            click: () => fireAndForget(callbacks.createNewWaveWindow),
        });
        fileMenu.push({
            label: menuLabel("New Window (hidden-2)", locale),
            accelerator: unamePlatform === "darwin" ? "Command+T" : "Alt+T",
            acceleratorWorksWhenHidden: true,
            visible: false,
            click: () => fireAndForget(callbacks.createNewWaveWindow),
        });
    }
    return fileMenu;
}

function makeAppMenuItems(locale: Locale, webContents: electron.WebContents): Electron.MenuItemConstructorOptions[] {
    const appMenuItems: Electron.MenuItemConstructorOptions[] = [
        {
            label: menuLabel("About Wave Terminal", locale),
            click: (_, window) => {
                (getWindowWebContents(window) ?? webContents)?.send("menu-item-about");
            },
        },
        {
            label: menuLabel("Check for Updates", locale),
            click: () => {
                fireAndForget(() => electron.shell.openExternal(ZhForkReleasesUrl));
            },
        },
        { type: "separator" },
    ];
    if (unamePlatform === "darwin") {
        appMenuItems.push(
            { role: "services", label: menuLabel("Services", locale) },
            { type: "separator" },
            { role: "hide", label: menuLabel("Hide Wave", locale) },
            { role: "hideOthers", label: menuLabel("Hide Others", locale) },
            { type: "separator" }
        );
    }
    appMenuItems.push({ role: "quit", label: menuLabel("Quit Wave", locale) });
    return appMenuItems;
}

function makeViewMenu(
    locale: Locale,
    webContents: electron.WebContents,
    callbacks: AppMenuCallbacks,
    isBuilderWindowFocused: boolean,
    fullscreenOnLaunch: boolean
): Electron.MenuItemConstructorOptions[] {
    const devToolsAccel = unamePlatform === "darwin" ? "Option+Command+I" : "Alt+Shift+I";
    return [
        {
            label: menuLabel(isBuilderWindowFocused ? "Reload Window" : "Reload Tab", locale),
            accelerator: "Shift+CommandOrControl+R",
            click: (_, window) => {
                (getWindowWebContents(window) ?? webContents)?.reloadIgnoringCache();
            },
        },
        {
            label: menuLabel("Relaunch All Windows", locale),
            click: () => callbacks.relaunchBrowserWindows(),
        },
        {
            label: menuLabel("Clear Tab Cache", locale),
            click: () => clearTabCache(),
        },
        {
            label: menuLabel("Toggle DevTools", locale),
            accelerator: devToolsAccel,
            click: (_, window) => {
                const wc = getWindowWebContents(window) ?? webContents;
                wc?.toggleDevTools();
            },
        },
        { type: "separator" },
        {
            label: menuLabel("Reset Zoom", locale),
            accelerator: "CommandOrControl+0",
            click: (_, window) => {
                const wc = getWindowWebContents(window) ?? webContents;
                if (wc) {
                    resetZoomLevel(wc);
                }
            },
        },
        {
            label: menuLabel("Zoom In", locale),
            accelerator: "CommandOrControl+=",
            click: (_, window) => {
                const wc = getWindowWebContents(window) ?? webContents;
                if (wc) {
                    increaseZoomLevel(wc);
                }
            },
        },
        {
            label: menuLabel("Zoom In (hidden)", locale),
            accelerator: "CommandOrControl+Shift+=",
            click: (_, window) => {
                const wc = getWindowWebContents(window) ?? webContents;
                if (wc) {
                    increaseZoomLevel(wc);
                }
            },
            visible: false,
            acceleratorWorksWhenHidden: true,
        },
        {
            label: menuLabel("Zoom Out", locale),
            accelerator: "CommandOrControl+-",
            click: (_, window) => {
                const wc = getWindowWebContents(window) ?? webContents;
                if (wc) {
                    decreaseZoomLevel(wc);
                }
            },
        },
        {
            label: menuLabel("Zoom Out (hidden)", locale),
            accelerator: "CommandOrControl+Shift+-",
            click: (_, window) => {
                const wc = getWindowWebContents(window) ?? webContents;
                if (wc) {
                    decreaseZoomLevel(wc);
                }
            },
            visible: false,
            acceleratorWorksWhenHidden: true,
        },
        {
            label: menuLabel("Launch On Full Screen", locale),
            submenu: [
                {
                    label: menuLabel("On", locale),
                    type: "radio",
                    checked: fullscreenOnLaunch,
                    click: () => {
                        RpcApi.SetConfigCommand(ElectronWshClient, { "window:fullscreenonlaunch": true });
                    },
                },
                {
                    label: menuLabel("Off", locale),
                    type: "radio",
                    checked: !fullscreenOnLaunch,
                    click: () => {
                        RpcApi.SetConfigCommand(ElectronWshClient, { "window:fullscreenonlaunch": false });
                    },
                },
            ],
        },
        { type: "separator" },
        {
            role: "togglefullscreen",
            label: menuLabel("Toggle Full Screen", locale),
        },
        { type: "separator" },
        {
            label: menuLabel("Toggle Widgets Bar", locale),
            click: () => {
                fireAndForget(async () => {
                    const workspaceId = focusedWaveWindow?.workspaceId;
                    if (!workspaceId) return;
                    const oref = `workspace:${workspaceId}`;
                    const meta = await RpcApi.GetMetaCommand(ElectronWshClient, { oref });
                    const current = meta?.["layout:widgetsvisible"] ?? true;
                    await RpcApi.SetMetaCommand(ElectronWshClient, { oref, meta: { "layout:widgetsvisible": !current } });
                });
            },
        },
    ];
}

async function makeFullAppMenu(callbacks: AppMenuCallbacks, workspaceOrBuilderId?: string): Promise<Electron.Menu> {
    const numWaveWindows = getAllWaveWindows().length;
    const webContents = workspaceOrBuilderId && getWebContentsByWorkspaceOrBuilderId(workspaceOrBuilderId);
    const isBuilderWindowFocused = focusedBuilderWindow != null;
    let fullscreenOnLaunch = false;
    let fullConfig: FullConfigType = null;
    try {
        fullConfig = await RpcApi.GetFullConfigCommand(ElectronWshClient);
        fullscreenOnLaunch = fullConfig?.settings["window:fullscreenonlaunch"];
    } catch (e) {
        console.error("Error fetching config:", e);
    }
    const locale = updateMainProcessLocaleFromFullConfig(fullConfig);
    const appMenuItems = makeAppMenuItems(locale, webContents);
    const editMenu = makeEditMenu(locale, fullConfig);
    const fileMenu = makeFileMenu(locale, numWaveWindows, callbacks, fullConfig);
    const viewMenu = makeViewMenu(locale, webContents, callbacks, isBuilderWindowFocused, fullscreenOnLaunch);
    let workspaceMenu: Electron.MenuItemConstructorOptions[] = null;
    try {
        workspaceMenu = await getWorkspaceMenu(locale);
    } catch (e) {
        console.error("getWorkspaceMenu error:", e);
    }
    const windowMenu: Electron.MenuItemConstructorOptions[] = [
        { role: "minimize", label: menuLabel("Minimize", locale), accelerator: "" },
        { role: "zoom", label: menuLabel("Zoom", locale) },
        { type: "separator" },
        { role: "front", label: menuLabel("Bring All to Front", locale) },
    ];
    const menuTemplate: Electron.MenuItemConstructorOptions[] = [
        { role: "appMenu", submenu: appMenuItems },
        { label: menuLabel("File", locale), submenu: fileMenu },
        { label: menuLabel("Edit", locale), submenu: editMenu },
        { label: menuLabel("View", locale), submenu: viewMenu },
    ];
    if (workspaceMenu != null && !isBuilderWindowFocused) {
        menuTemplate.push({
            label: menuLabel("Workspace", locale),
            id: "workspace-menu",
            submenu: workspaceMenu,
        });
    }
    menuTemplate.push({
        label: menuLabel("Window", locale),
        submenu: windowMenu,
    });
    return electron.Menu.buildFromTemplate(menuTemplate);
}

export function instantiateAppMenu(workspaceOrBuilderId?: string): Promise<electron.Menu> {
    return makeFullAppMenu(
        {
            createNewWaveWindow,
            relaunchBrowserWindows,
        },
        workspaceOrBuilderId
    );
}

// does not a set a menu on windows
export function makeAndSetAppMenu() {
    if (unamePlatform === "win32") {
        return;
    }
    fireAndForget(async () => {
        const menu = await instantiateAppMenu();
        electron.Menu.setApplicationMenu(menu);
    });
}

function handleConfigUpdateForMenu(event: { data?: { fullconfig?: FullConfigType } }) {
    updateMainProcessLocaleFromFullConfig(event?.data?.fullconfig);
    makeAndSetAppMenu();
    makeDockTaskbar();
}

function initMenuEventSubscriptions() {
    waveEventSubscribeSingle({
        eventType: "workspace:update",
        handler: makeAndSetAppMenu,
    });
    waveEventSubscribeSingle({
        eventType: "config",
        handler: handleConfigUpdateForMenu,
    });
}

function getWebContentsByWorkspaceOrBuilderId(workspaceOrBuilderId: string): electron.WebContents {
    const ww = getWaveWindowByWorkspaceId(workspaceOrBuilderId);
    if (ww) {
        return ww.activeTabView?.webContents;
    }

    const bw = getBuilderWindowById(workspaceOrBuilderId);
    if (bw) {
        return bw.webContents;
    }

    return null;
}

function convertMenuDefArrToMenu(
    webContents: electron.WebContents,
    menuDefArr: ElectronContextMenuItem[],
    menuState: { hasClick: boolean }
): electron.Menu {
    const menuItems: electron.MenuItem[] = [];
    for (const menuDef of menuDefArr) {
        const menuItemTemplate: electron.MenuItemConstructorOptions = {
            role: menuDef.role as any,
            label: menuDef.label,
            type: menuDef.type,
            click: () => {
                menuState.hasClick = true;
                webContents.send("contextmenu-click", menuDef.id);
            },
            checked: menuDef.checked,
            enabled: menuDef.enabled,
        };
        if (menuDef.submenu != null) {
            menuItemTemplate.submenu = convertMenuDefArrToMenu(webContents, menuDef.submenu, menuState);
        }
        const menuItem = new electron.MenuItem(menuItemTemplate);
        menuItems.push(menuItem);
    }
    return electron.Menu.buildFromTemplate(menuItems);
}

electron.ipcMain.on(
    "contextmenu-show",
    (event, workspaceOrBuilderId: string, menuDefArr: ElectronContextMenuItem[]) => {
        const webContents = getWebContentsByWorkspaceOrBuilderId(workspaceOrBuilderId);
        if (!webContents) {
            console.error("invalid window for context menu:", workspaceOrBuilderId);
            event.returnValue = true;
            return;
        }
        if (menuDefArr.length === 0) {
            webContents.send("contextmenu-click", null);
            event.returnValue = true;
            return;
        }
        fireAndForget(async () => {
            const menuState = { hasClick: false };
            const menu = convertMenuDefArrToMenu(webContents, menuDefArr, menuState);
            menu.popup({
                callback: () => {
                    if (!menuState.hasClick) {
                        webContents.send("contextmenu-click", null);
                    }
                },
            });
        });
        event.returnValue = true;
    }
);

electron.ipcMain.on("workspace-appmenu-show", (event, workspaceId: string) => {
    fireAndForget(async () => {
        const webContents = getWebContentsByWorkspaceOrBuilderId(workspaceId);
        if (!webContents) {
            console.error("invalid window for workspace app menu:", workspaceId);
            return;
        }
        const menu = await instantiateAppMenu(workspaceId);
        menu.popup();
    });
    event.returnValue = true;
});

electron.ipcMain.on("builder-appmenu-show", (event, builderId: string) => {
    fireAndForget(async () => {
        const webContents = getWebContentsByWorkspaceOrBuilderId(builderId);
        if (!webContents) {
            console.error("invalid window for builder app menu:", builderId);
            return;
        }
        const menu = await instantiateAppMenu(builderId);
        menu.popup();
    });
    event.returnValue = true;
});

function makeDockTaskbar() {
    if (unamePlatform == "darwin") {
        const locale = getMainProcessLocale();
        const dockMenu = electron.Menu.buildFromTemplate([
            {
                label: menuLabel("New Window", locale),
                click() {
                    fireAndForget(createNewWaveWindow);
                },
            },
        ]);
        electron.app.dock.setMenu(dockMenu);
    }
}

export { initMenuEventSubscriptions, makeDockTaskbar };
